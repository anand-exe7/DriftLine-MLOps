// Command server is the Driftline control plane: it wires config, Postgres,
// MinIO and every module's HTTP routes, then serves until SIGINT/SIGTERM.
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

	"driftline/go/internal/alert"
	"driftline/go/internal/config"
	"driftline/go/internal/db"
	"driftline/go/internal/middleware"
	"driftline/go/internal/registry"
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

	// ctx is cancelled on Ctrl+C / docker stop; everything downstream watches it.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

	// --- modules -----------------------------------------------------------
	registrySvc := registry.NewService(registry.NewPostgresRepository(pool), store, log)
	registryHandler := registry.NewHandler(registrySvc, cfg.MaxUploadBytes)

	var notifiers []alert.AlertNotifier
	if cfg.SlackWebhookURL != "" {
		notifiers = append(notifiers, alert.NewSlackWebhookNotifier(cfg.SlackWebhookURL, nil))
	}
	if cfg.DiscordWebhookURL != "" {
		notifiers = append(notifiers, alert.NewDiscordWebhookNotifier(cfg.DiscordWebhookURL, nil))
	}
	alertRepo := alert.NewPostgresRepository(pool)
	alertSvc := alert.NewService(notifiers, alertRepo, log, cfg.AlertCooldown)
	alertHandler := alert.NewHandler(alertSvc, alertRepo)
	// The drift scheduler receives alertSvc and calls alertSvc.HandleDriftReport.

	// --- router ------------------------------------------------------------
	r := chi.NewRouter()
	r.Use(chimw.RequestID, chimw.RealIP, middleware.Logger(log), chimw.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) {
		hctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		checks := map[string]string{"postgres": "ok", "minio": "ok"}
		status := http.StatusOK
		if err := pool.Ping(hctx); err != nil {
			checks["postgres"], status = err.Error(), http.StatusServiceUnavailable
		}
		if err := store.Ping(hctx); err != nil {
			checks["minio"], status = err.Error(), http.StatusServiceUnavailable
		}
		response.JSON(w, status, checks)
	})

	r.Route("/api/v1", func(r chi.Router) {
		registryHandler.Routes(r)
		alertHandler.Routes(r)
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http listening", "addr", cfg.HTTPAddr, "alert_channels", alertSvc.Channels())
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		log.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
