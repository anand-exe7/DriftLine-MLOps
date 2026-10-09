package rollback

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"driftline/go/internal/alert"
	"driftline/go/internal/drift"
	"driftline/go/internal/models"
	"driftline/go/internal/prediction"
)

type fakeDeployer struct {
	mu        sync.Mutex
	canaries  []models.Deployment
	rollbacks []string
}

func (f *fakeDeployer) Rollback(_ context.Context, model, reason string) (models.Deployment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rollbacks = append(f.rollbacks, model+": "+reason)
	return models.Deployment{}, nil
}

func (f *fakeDeployer) RunningCanaries(context.Context) ([]models.Deployment, error) {
	return f.canaries, nil
}

type fakeNotifier struct{ titles []string }

func (f *fakeNotifier) NotifyEvent(_ context.Context, ev alert.Event, _ uuid.UUID) error {
	f.titles = append(f.titles, ev.Title)
	return nil
}

var cfg = Config{
	Auto: true, CheckInterval: time.Minute, Window: 5 * time.Minute,
	MinRequests: 50, MaxErrorRate: 0.05, MaxErrorDelta: 0.03, MaxPositiveDiff: 0.25,
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestJudge(t *testing.T) {
	m := NewManager(nil, nil, nil, cfg, quiet())
	tests := []struct {
		name    string
		canary  prediction.Stats
		primary prediction.Stats
		want    bool
	}{
		{"too little traffic to judge", prediction.Stats{Requests: 10, Errors: 10}, prediction.Stats{Requests: 1000}, false},
		{"healthy", prediction.Stats{Requests: 100, Errors: 1, DefaultRate: 0.3}, prediction.Stats{Requests: 900, Errors: 9, DefaultRate: 0.32}, false},
		{"error spike", prediction.Stats{Requests: 100, Errors: 20, DefaultRate: 0.3}, prediction.Stats{Requests: 900, DefaultRate: 0.3}, true},
		{"errors high but primary equally bad", prediction.Stats{Requests: 100, Errors: 10}, prediction.Stats{Requests: 900, Errors: 90}, false},
		{"flags everyone as a defaulter", prediction.Stats{Requests: 100, DefaultRate: 0.95}, prediction.Stats{Requests: 900, DefaultRate: 0.33}, true},
		{"rate diff ignored when primary has no traffic", prediction.Stats{Requests: 100, DefaultRate: 0.95}, prediction.Stats{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.Judge(tc.canary, tc.primary); got.Rollback != tc.want {
				t.Fatalf("rollback=%v (%s), want %v", got.Rollback, got.Reason, tc.want)
			}
		})
	}
}

func TestCheckCanariesRollsBackUnhealthy(t *testing.T) {
	canaryVersion := uuid.New()
	dep := &fakeDeployer{canaries: []models.Deployment{{ModelName: "m", Version: "v2", ModelVersionID: canaryVersion, Role: models.RoleCanary}}}
	stats := func(context.Context, string, time.Time) ([]prediction.Stats, error) {
		return []prediction.Stats{
			{ModelVersionID: canaryVersion.String(), Requests: 100, Errors: 30},
			{ModelVersionID: uuid.NewString(), Requests: 900, Errors: 0},
		}, nil
	}
	n := &fakeNotifier{}
	NewManager(dep, stats, n, cfg, quiet()).CheckCanaries(context.Background())

	if len(dep.rollbacks) != 1 || len(n.titles) != 1 {
		t.Fatalf("rollbacks=%v notifications=%v", dep.rollbacks, n.titles)
	}
}

func TestAutoRollbackDisabledOnlyNotifies(t *testing.T) {
	canaryVersion := uuid.New()
	dep := &fakeDeployer{canaries: []models.Deployment{{ModelName: "m", ModelVersionID: canaryVersion, Role: models.RoleCanary}}}
	stats := func(context.Context, string, time.Time) ([]prediction.Stats, error) {
		return []prediction.Stats{{ModelVersionID: canaryVersion.String(), Requests: 100, Errors: 50}}, nil
	}
	off := cfg
	off.Auto = false
	n := &fakeNotifier{}
	NewManager(dep, stats, n, off, quiet()).CheckCanaries(context.Background())
	if len(dep.rollbacks) != 0 || len(n.titles) != 1 {
		t.Fatalf("rollbacks=%v notifications=%v", dep.rollbacks, n.titles)
	}
}

func report(model string, drifted bool, psi float64) models.DriftReport {
	return models.DriftReport{ModelName: model, Version: "v", DriftDetected: drifted, OverallPSI: psi, ModelVersionID: uuid.New()}
}

func TestOnDriftRun(t *testing.T) {
	tests := []struct {
		name          string
		reports       []drift.CheckedReport
		wantRollbacks int
		wantNotices   int
	}{
		{"canary drifted, primary fine -> roll back canary", []drift.CheckedReport{
			{Report: report("m", true, 0.4), Role: "canary"},
			{Report: report("m", false, 0.02), Role: "primary"},
		}, 1, 1},
		{"both drifted -> data drift, recommend retraining", []drift.CheckedReport{
			{Report: report("m", true, 0.4), Role: "canary"},
			{Report: report("m", true, 0.4), Role: "primary"},
		}, 0, 1},
		{"production drifted -> recommend retraining", []drift.CheckedReport{
			{Report: report("m", true, 0.3), Role: "production"},
		}, 0, 1},
		{"nothing drifted", []drift.CheckedReport{
			{Report: report("m", false, 0.01), Role: "primary"},
		}, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dep := &fakeDeployer{canaries: []models.Deployment{{ModelName: "m", Role: models.RoleCanary}}}
			n := &fakeNotifier{}
			NewManager(dep, nil, n, cfg, quiet()).OnDriftRun(context.Background(), tc.reports)
			if len(dep.rollbacks) != tc.wantRollbacks || len(n.titles) != tc.wantNotices {
				t.Fatalf("rollbacks=%v notices=%v", dep.rollbacks, n.titles)
			}
		})
	}
}
