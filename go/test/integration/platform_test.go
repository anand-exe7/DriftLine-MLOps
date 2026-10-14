//go:build integration

// Package integration exercises the real repositories, MinIO and ml-service
// together. It needs the compose stack running:
//
//	docker compose up -d
//	go test -tags integration ./go/test/integration/ -v
//
// Every run uses a fresh model name, so it never touches existing data.
package integration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"driftline/go/internal/config"
	"driftline/go/internal/db"
	"driftline/go/internal/deployment"
	"driftline/go/internal/drift"
	"driftline/go/internal/mlclient"
	"driftline/go/internal/models"
	"driftline/go/internal/prediction"
	"driftline/go/internal/registry"
	"driftline/go/internal/storage"
)

func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

type env struct {
	pool      *pgxpool.Pool
	store     *storage.MinIOStore
	registry  *registry.Service
	deploy    *deployment.Service
	predict   *prediction.Service
	logs      *prediction.LogWriter
	driftRepo *drift.PostgresRepository
	ml        *mlclient.GRPC
}

func setup(t *testing.T) env {
	t.Helper()
	ctx := context.Background()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		t.Skipf("postgres not reachable (start the compose stack): %v", err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	store, err := storage.NewMinIOStore(ctx, cfg.MinIO)
	if err != nil {
		t.Fatal(err)
	}
	ml, err := mlclient.Dial(cfg.MLServiceAddr, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ml.Close() })

	regRepo := registry.NewPostgresRepository(pool)
	var router *prediction.Router
	dep := deployment.NewService(deployment.NewPostgresRepository(pool), regRepo, ml, cfg.MLServiceAddr, log,
		func(m string) { router.Invalidate(m) })
	router = prediction.NewRouter(dep, time.Second)
	logs := prediction.NewLogWriter(pool, log, 10000)
	logs.Start()

	return env{
		pool:      pool,
		store:     store,
		registry:  registry.NewService(regRepo, store, log),
		deploy:    dep,
		predict:   prediction.NewService(router, ml, logs, log),
		logs:      logs,
		driftRepo: drift.NewPostgresRepository(pool),
		ml:        ml,
	}
}

func register(t *testing.T, e env, model, version string) {
	t.Helper()
	bundle := filepath.Join(repoRoot(), "ml-service", "models", "loan_default_lr", "v1")
	onnx, err := os.ReadFile(filepath.Join(bundle, "model.onnx"))
	if err != nil {
		t.Fatal(err)
	}
	schema, _ := os.ReadFile(filepath.Join(bundle, "feature_schema.json"))
	baseline, _ := os.ReadFile(filepath.Join(repoRoot(), "ml", "projects", "loan_default", "artifacts", "baseline_stats.json"))
	_, err = e.registry.RegisterVersion(context.Background(), registry.RegisterVersionInput{
		ModelName: model, Version: version, Artifact: bytes.NewReader(onnx), ArtifactSize: int64(len(onnx)),
		FeatureSchema: schema, BaselineStats: baseline, Metrics: []byte(`{"roc_auc":0.75}`),
	})
	if err != nil {
		t.Fatal(err)
	}
}

// cleanup removes everything the test created so the dashboard never shows it.
// deployments reference versions with ON DELETE RESTRICT, so they go first;
// deleting the model cascades to versions, prediction logs and drift reports.
func cleanup(t *testing.T, e env, model string) {
	ctx := context.Background()
	_, _ = e.ml.UnloadModel(ctx, model, "v1")
	_, _ = e.ml.UnloadModel(ctx, model, "v2")
	if _, err := e.pool.Exec(ctx, `
		DELETE FROM deployments WHERE model_version_id IN (
			SELECT v.id FROM model_versions v JOIN models m ON m.id = v.model_id WHERE m.name = $1)`, model); err != nil {
		t.Errorf("cleanup deployments: %v", err)
	}
	if _, err := e.pool.Exec(ctx, `DELETE FROM models WHERE name = $1`, model); err != nil {
		t.Errorf("cleanup model: %v", err)
	}
	for _, v := range []string{"v1", "v2"} {
		_ = e.store.DeleteModelArtifact(ctx, storage.ArtifactKey(model, v))
	}
}

// applicant is a typical row; shift moves it far outside the training distribution.
func applicant(i int, shift bool) map[string]float64 {
	f := map[string]float64{
		"Age": float64(20 + i%50), "Income": float64(20000 + (i*7919)%130000), "LoanAmount": float64(5000 + (i*104729)%245000),
		"CreditScore": float64(300 + (i*31)%550), "MonthsEmployed": float64(i % 120), "NumCreditLines": float64(1 + i%4),
		"InterestRate": 2 + float64(i%23), "LoanTerm": float64(12 * (1 + i%5)), "DTIRatio": 0.1 + float64(i%80)/100,
		"HasMortgage": float64(i % 2), "HasDependents": float64((i / 2) % 2), "HasCoSigner": float64((i / 4) % 2),
		"Education_High School": 0, "Education_Master's": 0, "Education_PhD": 0,
		"EmploymentType_Part-time": 0, "EmploymentType_Self-employed": 0, "EmploymentType_Unemployed": 0,
		"MaritalStatus_Married": 0, "MaritalStatus_Single": 0,
		"LoanPurpose_Business": 0, "LoanPurpose_Education": 0, "LoanPurpose_Home": 0, "LoanPurpose_Other": 0,
	}
	if shift {
		f["Income"] = 15000
		f["InterestRate"] = 25
		f["CreditScore"] = 300
	}
	return f
}

func TestPlatformEndToEnd(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	model := fmt.Sprintf("it_%d", time.Now().UnixNano())

	if _, err := e.registry.CreateModel(ctx, registry.CreateModelInput{Name: model}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cleanup(t, e, model) })
	register(t, e, model, "v1")
	register(t, e, model, "v2")

	// Deploying makes ml-service pull the bundle from MinIO (the name is new to it).
	if _, err := e.deploy.Deploy(ctx, deployment.DeployInput{Model: model, Version: "v1"}); err != nil {
		t.Fatalf("deploy primary: %v", err)
	}
	canary, err := e.deploy.Deploy(ctx, deployment.DeployInput{Model: model, Version: "v2", TrafficPercent: 25})
	if err != nil {
		t.Fatalf("deploy canary: %v", err)
	}

	counts := map[string]int{}
	for i := range 200 {
		res, err := e.predict.Predict(ctx, model, prediction.Request{Features: applicant(i, i >= 100)})
		if err != nil {
			t.Fatalf("predict %d: %v", i, err)
		}
		counts[res.Version]++
		if len(res.Probabilities) != 2 {
			t.Fatalf("probabilities: %v", res.Probabilities)
		}
	}
	if counts["v1"] != 150 || counts["v2"] != 50 {
		t.Fatalf("75/25 split violated: %v", counts)
	}

	// Rejected input is logged too, with its status code.
	if _, err := e.predict.Predict(ctx, model, prediction.Request{Features: map[string]float64{"Age": 1}}); !errors.Is(err, prediction.ErrInvalid) {
		t.Fatalf("invalid features: %v", err)
	}

	e.logs.Close() // flush the async buffer

	stats, err := prediction.StatsSince(ctx, e.pool, model, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	total, errs := int64(0), int64(0)
	for _, s := range stats {
		total += s.Requests
		errs += s.Errors
	}
	if total != 201 || errs != 1 {
		t.Fatalf("prediction_logs: %d rows, %d errors (want 201, 1): %+v", total, errs, stats)
	}

	// Drift: half the traffic was pushed to extreme values.
	sched := drift.NewScheduler(e.driftRepo, nil, nil, drift.Config{
		Interval: time.Hour, Window: time.Hour, MinSamples: 20, Workers: 2, Thresholds: drift.DefaultThresholds,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	reports, err := sched.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var mine []models.DriftReport
	for _, r := range reports {
		if r.ModelName == model {
			mine = append(mine, r)
		}
	}
	if len(mine) != 2 {
		t.Fatalf("expected a report for primary and canary, got %d", len(mine))
	}
	for _, r := range mine {
		if !r.DriftDetected || r.FeatureScores["CreditScore"].PSI < 0.25 {
			t.Fatalf("%s: drift not detected: psi=%.3f credit=%.3f", r.Version, r.OverallPSI, r.FeatureScores["CreditScore"].PSI)
		}
	}
	listed, err := e.driftRepo.List(ctx, model, 10)
	if err != nil || len(listed) != 2 {
		t.Fatalf("listed reports: %d, %v", len(listed), err)
	}

	// Roll the canary back; v1 returns to 100%.
	if _, err := e.deploy.Rollback(ctx, model, "integration test"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.deploy.Get(ctx, canary.ID)
	if got.Status != models.DeploymentRolledBack {
		t.Fatalf("canary status %s", got.Status)
	}
	routes, err := e.deploy.Routes(ctx, model)
	if err != nil || len(routes) != 1 || routes[0].Version != "v1" || routes[0].Weight != 100 {
		t.Fatalf("routes after rollback: %+v %v", routes, err)
	}
}
