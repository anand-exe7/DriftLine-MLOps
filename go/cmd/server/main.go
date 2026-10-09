// Command server is the Driftline control plane: it wires config, Postgres,
// MinIO, the ml-service gRPC client and every module, then serves HTTP and
// runs the background loops (drift scheduler, canary health monitor) until
// SIGINT/SIGTERM.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"driftline/go/internal/alert"
	"driftline/go/internal/config"
	"driftline/go/internal/db"
	"driftline/go/internal/deployment"
	"driftline/go/internal/drift"
	"driftline/go/internal/middleware"
	"driftline/go/internal/mlclient"
	"driftline/go/internal/prediction"
	"driftline/go/internal/registry"
	"driftline/go/internal/rollback"
	"driftline/go/internal/storage"
	"driftline/go/pkg/response"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("server exited", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// ctx is cancelled on Ctrl+C / docker stop; every background loop watches it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// --- infrastructure ------------------------------------------------------
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}

	store, err := storage.NewMinIOStore(ctx, cfg.MinIO)
	if err != nil {
		return err
	}

	ml, err := mlclient.Dial(cfg.MLServiceAddr, log)
	if err != nil {
		return err
	}
	defer ml.Close()

	// --- modules -------------------------------------------------------------
	registryRepo := registry.NewPostgresRepository(pool)
	registrySvc := registry.NewService(registryRepo, store, log)

	var notifiers []alert.AlertNotifier
	if cfg.SlackWebhookURL != "" {
		notifiers = append(notifiers, alert.NewSlackWebhookNotifier(cfg.SlackWebhookURL, nil))
	}
	if cfg.DiscordWebhookURL != "" {
		notifiers = append(notifiers, alert.NewDiscordWebhookNotifier(cfg.DiscordWebhookURL, nil))
	}
	alertRepo := alert.NewPostgresRepository(pool)
	alertSvc := alert.NewService(notifiers, alertRepo, log, cfg.AlertCooldown)

	// The router caches routing tables; the deployment manager invalidates
	// them on every change. Declared first so the closure can capture it.
	var router *prediction.Router
	deploySvc := deployment.NewService(deployment.NewPostgresRepository(pool), registryRepo, ml, cfg.MLServiceAddr, log,
		func(model string) { router.Invalidate(model) })
	router = prediction.NewRouter(deploySvc, 5*time.Second)

	logWriter := prediction.NewLogWriter(pool, log, cfg.PredictionLogBuffer)
	logWriter.Start()
	predictSvc := prediction.NewService(router, ml, logWriter, log)

	rollbackMgr := rollback.NewManager(deploySvc,
		func(ctx context.Context, model string, since time.Time) ([]prediction.Stats, error) {
			return prediction.StatsSince(ctx, pool, model, since)
		},
		alertSvc,
		rollback.Config{
			Auto: cfg.Rollback.Auto, CheckInterval: cfg.Rollback.CheckInterval, Window: cfg.Rollback.Window,
			MinRequests: int64(cfg.Rollback.MinRequests), MaxErrorRate: cfg.Rollback.MaxErrorRate,
			MaxErrorDelta: cfg.Rollback.MaxErrorDelta, MaxPositiveDiff: cfg.Rollback.MaxPositiveDiff,
		}, log)

	driftRepo := drift.NewPostgresRepository(pool)
	scheduler := drift.NewScheduler(driftRepo, alertSvc, rollbackMgr, drift.Config{
		Interval: cfg.Drift.Interval, Window: cfg.Drift.Window, MinSamples: cfg.Drift.MinSamples,
		Workers: cfg.Drift.Workers, Thresholds: drift.Thresholds{FeaturePSI: cfg.Drift.FeaturePSI, OverallPSI: cfg.Drift.OverallPSI},
	}, log)

	// --- background loops ------------------------------------------------------
	var loops []<-chan struct{}
	if cfg.Drift.Enabled {
		loops = append(loops, scheduler.Start(ctx))
	}
	loops = append(loops, rollbackMgr.Start(ctx))

	// --- router ----------------------------------------------------------------
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, middleware.Logger(log), chimw.Recoverer)

	r.Handle("/metrics", promhttp.Handler())
	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		hctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		checks := map[string]string{"postgres": "ok", "minio": "ok", "ml_service": "ok"}
		status := http.StatusOK
		if err := pool.Ping(hctx); err != nil {
			checks["postgres"], status = err.Error(), http.StatusServiceUnavailable
		}
		if err := store.Ping(hctx); err != nil {
			checks["minio"], status = err.Error(), http.StatusServiceUnavailable
		}
		if _, err := ml.Health(hctx); err != nil {
			checks["ml_service"] = err.Error() // degraded, but the registry still works
		}
		response.JSON(w, status, checks)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Use(middleware.APIKey(cfg.APIKey))
		registry.NewHandler(registrySvc, cfg.MaxUploadBytes).Routes(r)
		alert.NewHandler(alertSvc, alertRepo).Routes(r)
		deployment.NewHandler(deploySvc).Routes(r)
		prediction.NewHandler(predictSvc, pool).Routes(r)
		drift.NewHandler(driftRepo, scheduler).Routes(r)
	})

	if cfg.APIKey == "" {
		log.Warn("API_KEY is not set: /api/v1 is unauthenticated (fine for local development only)")
	}

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr, "alert_channels", alertSvc.Channels(),
			"drift_interval", cfg.Drift.Interval, "auto_rollback", cfg.Rollback.Auto)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	}

	// Order matters: stop taking requests, let background loops finish their
	// current pass, then flush buffered prediction logs before the pool closes.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	stop()
	for _, done := range loops {
		select {
		case <-done:
		case <-shutdownCtx.Done():
		}
	}
	logWriter.Close()
	return err
}
