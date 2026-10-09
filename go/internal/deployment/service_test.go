package deployment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"driftline/go/internal/models"
	"driftline/go/internal/prediction"
	"driftline/go/internal/registry"
	pb "driftline/proto/predict"
)

// ---- fakes ----------------------------------------------------------------

type memRepo struct {
	mu   sync.Mutex
	rows map[uuid.UUID]*models.Deployment
	seq  int
}

func newMemRepo() *memRepo { return &memRepo{rows: map[uuid.UUID]*models.Deployment{}} }

func (m *memRepo) Create(_ context.Context, d *models.Deployment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	d.ID = uuid.New()
	d.CreatedAt = time.Unix(int64(m.seq), 0)
	cp := *d
	m.rows[d.ID] = &cp
	return nil
}

func (m *memRepo) Get(_ context.Context, id uuid.UUID) (models.Deployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.rows[id]
	if !ok {
		return models.Deployment{}, fmt.Errorf("get: %w", ErrNotFound)
	}
	return *d, nil
}

func (m *memRepo) sorted(filter func(*models.Deployment) bool) []models.Deployment {
	var out []models.Deployment
	for _, d := range m.rows {
		if filter(d) {
			out = append(out, *d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

func (m *memRepo) List(_ context.Context, model string, _ int) ([]models.Deployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sorted(func(d *models.Deployment) bool { return model == "" || d.ModelName == model }), nil
}

func (m *memRepo) Running(_ context.Context, model string) ([]models.Deployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sorted(func(d *models.Deployment) bool {
		return d.ModelName == model && d.Status == models.DeploymentRunning
	}), nil
}

func (m *memRepo) PreviousPrimary(_ context.Context, model string, exclude uuid.UUID) (models.Deployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.sorted(func(d *models.Deployment) bool {
		return d.ModelName == model && d.Role == models.RolePrimary && d.ModelVersionID != exclude &&
			(d.Status == models.DeploymentStopped || d.Status == models.DeploymentRolledBack) && d.StartedAt != nil
	}) {
		return d, nil
	}
	return models.Deployment{}, fmt.Errorf("previous: %w", ErrNotFound)
}

func (m *memRepo) Apply(_ context.Context, changes []Change) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range changes {
		d := m.rows[c.ID]
		d.Status, d.TrafficPercent = c.Status, c.Traffic
		if c.Role != "" {
			d.Role = c.Role
		}
		if c.Status == models.DeploymentRunning && d.StartedAt == nil {
			now := time.Now()
			d.StartedAt = &now
		}
	}
	return nil
}

type memVersions struct {
	mu       sync.Mutex
	versions map[string]*models.ModelVersion // "model/version"
}

func newVersions(model string, versions ...string) *memVersions {
	mv := &memVersions{versions: map[string]*models.ModelVersion{}}
	for _, v := range versions {
		mv.versions[model+"/"+v] = &models.ModelVersion{ID: uuid.New(), ModelName: model, Version: v, Stage: models.StageRegistered}
	}
	return mv
}

func (m *memVersions) GetVersion(_ context.Context, model, version string) (models.ModelVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.versions[model+"/"+version]
	if !ok {
		return models.ModelVersion{}, fmt.Errorf("version: %w", registry.ErrNotFound)
	}
	return *v, nil
}

func (m *memVersions) ListVersions(_ context.Context, model string) ([]models.ModelVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []models.ModelVersion
	for _, v := range m.versions {
		if v.ModelName == model {
			out = append(out, *v)
		}
	}
	return out, nil
}

func (m *memVersions) SetStage(_ context.Context, model, version string, stage models.Stage) (models.ModelVersion, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if stage == models.StageProduction {
		for _, v := range m.versions {
			if v.ModelName == model && v.Stage == models.StageProduction {
				v.Stage = models.StageArchived
			}
		}
	}
	v := m.versions[model+"/"+version]
	v.Stage = stage
	return *v, nil
}

func (m *memVersions) stage(model, version string) models.Stage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.versions[model+"/"+version].Stage
}

type fakeML struct {
	failLoad string // version that fails to load
	unloaded []string
}

func (f *fakeML) Predict(context.Context, string, string, map[string]float64, string) (*pb.PredictionResponse, error) {
	return &pb.PredictionResponse{}, nil
}
func (f *fakeML) LoadModel(_ context.Context, _, version string) (*pb.LoadModelResponse, error) {
	if version == f.failLoad {
		return &pb.LoadModelResponse{StatusCode: 5, ErrorMessage: "not found"}, nil
	}
	return &pb.LoadModelResponse{}, nil
}
func (f *fakeML) UnloadModel(_ context.Context, _, version string) (*pb.LoadModelResponse, error) {
	f.unloaded = append(f.unloaded, version)
	return &pb.LoadModelResponse{}, nil
}
func (f *fakeML) Health(context.Context) (*pb.HealthResponse, error) {
	return &pb.HealthResponse{}, nil
}

func newSvc(t *testing.T, versions ...string) (*Service, *memRepo, *memVersions, *fakeML, *[]string) {
	t.Helper()
	repo, vs, ml := newMemRepo(), newVersions("m", versions...), &fakeML{}
	var invalidated []string
	svc := NewService(repo, vs, ml, "ml:50051", slog.New(slog.NewTextHandler(io.Discard, nil)),
		func(model string) { invalidated = append(invalidated, model) })
	return svc, repo, vs, ml, &invalidated
}

// assertTraffic checks the running set: exact versions and weights, summing to 100.
func assertTraffic(t *testing.T, repo *memRepo, want map[string]int) {
	t.Helper()
	running, _ := repo.Running(context.Background(), "m")
	got := map[string]int{}
	sum := 0
	for _, d := range running {
		got[d.Version] = d.TrafficPercent
		sum += d.TrafficPercent
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("running traffic = %v, want %v", got, want)
	}
	if len(running) > 0 && sum != 100 {
		t.Fatalf("traffic sums to %d", sum)
	}
}

// ---- tests ----------------------------------------------------------------

func TestCanaryLifecyclePromote(t *testing.T) {
	ctx := context.Background()
	svc, repo, vs, ml, inval := newSvc(t, "v1", "v2")

	if _, err := svc.Deploy(ctx, DeployInput{Model: "m", Version: "v1"}); err != nil {
		t.Fatal(err)
	}
	assertTraffic(t, repo, map[string]int{"v1": 100})
	if vs.stage("m", "v1") != models.StageProduction {
		t.Fatal("primary deploy should make the version production")
	}

	canary, err := svc.Deploy(ctx, DeployInput{Model: "m", Version: "v2", TrafficPercent: 10})
	if err != nil {
		t.Fatal(err)
	}
	if canary.Role != models.RoleCanary {
		t.Fatalf("role = %s", canary.Role)
	}
	assertTraffic(t, repo, map[string]int{"v1": 90, "v2": 10})
	if vs.stage("m", "v2") != models.StageStaging {
		t.Fatal("canary should be staging")
	}

	if _, err := svc.SetTraffic(ctx, canary.ID, 40); err != nil {
		t.Fatal(err)
	}
	assertTraffic(t, repo, map[string]int{"v1": 60, "v2": 40})

	promoted, err := svc.Promote(ctx, canary.ID)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.Role != models.RolePrimary || promoted.TrafficPercent != 100 {
		t.Fatalf("promoted = %+v", promoted)
	}
	assertTraffic(t, repo, map[string]int{"v2": 100})
	if vs.stage("m", "v2") != models.StageProduction || vs.stage("m", "v1") != models.StageArchived {
		t.Fatal("registry stages not updated on promote")
	}
	if len(ml.unloaded) != 1 || ml.unloaded[0] != "v1" {
		t.Fatalf("old primary should be unloaded, got %v", ml.unloaded)
	}
	if len(*inval) < 4 {
		t.Fatalf("router should be invalidated on every change, got %v", *inval)
	}
}

func TestCanaryRollback(t *testing.T) {
	ctx := context.Background()
	svc, repo, vs, _, _ := newSvc(t, "v1", "v2")
	_, _ = svc.Deploy(ctx, DeployInput{Model: "m", Version: "v1"})
	canary, _ := svc.Deploy(ctx, DeployInput{Model: "m", Version: "v2", TrafficPercent: 20})

	if _, err := svc.Rollback(ctx, "m", "test"); err != nil {
		t.Fatal(err)
	}
	assertTraffic(t, repo, map[string]int{"v1": 100})
	got, _ := repo.Get(ctx, canary.ID)
	if got.Status != models.DeploymentRolledBack {
		t.Fatalf("canary status = %s", got.Status)
	}
	if vs.stage("m", "v2") != models.StageArchived {
		t.Fatal("rolled-back canary should be archived")
	}
}

func TestPrimaryRollbackRedeploysPrevious(t *testing.T) {
	ctx := context.Background()
	svc, repo, vs, _, _ := newSvc(t, "v1", "v2")
	_, _ = svc.Deploy(ctx, DeployInput{Model: "m", Version: "v1"})
	_, _ = svc.Deploy(ctx, DeployInput{Model: "m", Version: "v2"}) // blue/green replace
	assertTraffic(t, repo, map[string]int{"v2": 100})

	if _, err := svc.Rollback(ctx, "m", "bad release"); err != nil {
		t.Fatal(err)
	}
	assertTraffic(t, repo, map[string]int{"v1": 100})
	if vs.stage("m", "v1") != models.StageProduction {
		t.Fatal("v1 should be production again")
	}
}

func TestRollbackWithNothingToRollBackTo(t *testing.T) {
	ctx := context.Background()
	svc, _, _, _, _ := newSvc(t, "v1")
	if _, err := svc.Rollback(ctx, "m", "x"); !errors.Is(err, ErrNothingToRollback) {
		t.Fatalf("no deployments: %v", err)
	}
	_, _ = svc.Deploy(ctx, DeployInput{Model: "m", Version: "v1"})
	if _, err := svc.Rollback(ctx, "m", "x"); !errors.Is(err, ErrNothingToRollback) {
		t.Fatalf("single primary: %v", err)
	}
}

func TestDeployValidation(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		setup   func(*Service, *memVersions)
		in      DeployInput
		wantErr error
	}{
		{"unknown version", nil, DeployInput{Model: "m", Version: "v9"}, ErrNotFound},
		{"traffic out of range", nil, DeployInput{Model: "m", Version: "v1", TrafficPercent: 150}, ErrInvalid},
		{"canary without primary", nil, DeployInput{Model: "m", Version: "v1", TrafficPercent: 10}, ErrInvalid},
		{"archived version", func(_ *Service, vs *memVersions) {
			_, _ = vs.SetStage(ctx, "m", "v1", models.StageArchived)
		}, DeployInput{Model: "m", Version: "v1"}, ErrInvalid},
		{"already deployed", func(s *Service, _ *memVersions) {
			_, _ = s.Deploy(ctx, DeployInput{Model: "m", Version: "v1"})
		}, DeployInput{Model: "m", Version: "v1"}, ErrConflict},
		{"second canary", func(s *Service, _ *memVersions) {
			_, _ = s.Deploy(ctx, DeployInput{Model: "m", Version: "v1"})
			_, _ = s.Deploy(ctx, DeployInput{Model: "m", Version: "v2", TrafficPercent: 10})
		}, DeployInput{Model: "m", Version: "v3", TrafficPercent: 10}, ErrConflict},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, vs, _, _ := newSvc(t, "v1", "v2", "v3")
			if tc.setup != nil {
				tc.setup(svc, vs)
			}
			if _, err := svc.Deploy(ctx, tc.in); !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestWarmupFailureMarksDeploymentFailed(t *testing.T) {
	ctx := context.Background()
	svc, repo, _, ml, _ := newSvc(t, "v1")
	ml.failLoad = "v1"
	if _, err := svc.Deploy(ctx, DeployInput{Model: "m", Version: "v1"}); !errors.Is(err, ErrWarmup) {
		t.Fatalf("got %v", err)
	}
	all, _ := repo.List(ctx, "m", 10)
	if len(all) != 1 || all[0].Status != models.DeploymentFailed {
		t.Fatalf("deployment should be recorded as failed: %+v", all)
	}
	assertTraffic(t, repo, map[string]int{})
}

func TestRoutesFallBackToProductionVersion(t *testing.T) {
	ctx := context.Background()
	svc, _, vs, _, _ := newSvc(t, "v1")
	if r, _ := svc.Routes(ctx, "m"); len(r) != 0 {
		t.Fatalf("no production version: want no routes, got %v", r)
	}
	_, _ = vs.SetStage(ctx, "m", "v1", models.StageProduction)
	r, err := svc.Routes(ctx, "m")
	if err != nil || len(r) != 1 || r[0].Role != prediction.RoleProduction || r[0].Weight != 100 {
		t.Fatalf("routes = %+v, err %v", r, err)
	}
}
