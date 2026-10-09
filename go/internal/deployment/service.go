// Package deployment manages which model versions receive live traffic.
//
// ml-service is a single multi-model server, so a "deployment" here is a
// routing decision plus a warm model session, not a container per version:
// deploying = warm the version in ml-service (LoadModel) + give it a traffic
// share. A model has at most one primary and one canary running; their
// traffic_percent always sums to 100.
package deployment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"driftline/go/internal/metrics"
	"driftline/go/internal/mlclient"
	"driftline/go/internal/models"
	"driftline/go/internal/prediction"
	"driftline/go/internal/registry"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrConflict          = errors.New("conflict")
	ErrInvalid           = errors.New("invalid input")
	ErrWarmup            = errors.New("ml-service could not load the model")
	ErrNothingToRollback = errors.New("nothing to roll back to")
)

// VersionStore is the slice of the registry the deployment manager needs.
type VersionStore interface {
	GetVersion(ctx context.Context, model, version string) (models.ModelVersion, error)
	ListVersions(ctx context.Context, model string) ([]models.ModelVersion, error)
	SetStage(ctx context.Context, model, version string, stage models.Stage) (models.ModelVersion, error)
}

type Service struct {
	repo     Repository
	versions VersionStore
	ml       mlclient.Client
	endpoint string
	log      *slog.Logger
	onChange func(model string) // invalidates the prediction router's cache

	locks sync.Map // model name -> *sync.Mutex
}

func NewService(repo Repository, versions VersionStore, ml mlclient.Client, endpoint string, log *slog.Logger, onChange func(string)) *Service {
	if onChange == nil {
		onChange = func(string) {}
	}
	return &Service{repo: repo, versions: versions, ml: ml, endpoint: endpoint, log: log, onChange: onChange}
}

// lock serialises every state change for one model, so two concurrent
// "deploy canary" calls can't both see "no canary yet". Single-replica scope;
// several control-plane replicas would need a Postgres advisory lock instead.
func (s *Service) lock(model string) func() {
	m, _ := s.locks.LoadOrStore(model, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

type DeployInput struct {
	Model          string `json:"model"`
	Version        string `json:"version"`
	TrafficPercent int    `json:"traffic_percent"`
}

// Deploy starts serving a version.
//   - traffic 100 (or no primary yet): it becomes the primary; the old primary
//     and any canary are stopped once the new one is warm (blue/green switch).
//   - traffic 1-99 with a primary running: it becomes the canary and the
//     primary keeps the remainder.
func (s *Service) Deploy(ctx context.Context, in DeployInput) (models.Deployment, error) {
	if in.TrafficPercent == 0 {
		in.TrafficPercent = 100
	}
	if in.TrafficPercent < 1 || in.TrafficPercent > 100 {
		return models.Deployment{}, fmt.Errorf("%w: traffic_percent must be 1-100", ErrInvalid)
	}
	defer s.lock(in.Model)()

	v, err := s.versions.GetVersion(ctx, in.Model, in.Version)
	if err != nil {
		return models.Deployment{}, wrapRegistry(err)
	}
	if v.Stage == models.StageArchived {
		return models.Deployment{}, fmt.Errorf("%w: version %s is archived", ErrInvalid, in.Version)
	}

	running, err := s.repo.Running(ctx, in.Model)
	if err != nil {
		return models.Deployment{}, err
	}
	primary, canary := split(running)
	for _, d := range running {
		if d.ModelVersionID == v.ID {
			return models.Deployment{}, fmt.Errorf("%w: %s/%s is already deployed as %s", ErrConflict, in.Model, in.Version, d.Role)
		}
	}

	role := models.RolePrimary
	if in.TrafficPercent < 100 {
		if primary == nil {
			return models.Deployment{}, fmt.Errorf("%w: a canary needs a running primary; deploy at 100%% first", ErrInvalid)
		}
		if canary != nil {
			return models.Deployment{}, fmt.Errorf("%w: %s already has a canary (%s); promote or roll it back first", ErrConflict, in.Model, canary.Version)
		}
		role = models.RoleCanary
	}

	d := models.Deployment{
		ModelVersionID: v.ID, ModelName: in.Model, Version: in.Version,
		Environment: "production", Role: role, Status: models.DeploymentPending,
		TrafficPercent: 0, Endpoint: s.endpoint,
	}
	if err := s.repo.Create(ctx, &d); err != nil {
		return models.Deployment{}, err
	}

	// Warm the model before it gets any traffic.
	if err := s.warm(ctx, in.Model, in.Version); err != nil {
		_ = s.repo.Apply(context.WithoutCancel(ctx), []Change{{ID: d.ID, Status: models.DeploymentFailed, Reason: err.Error()}})
		return models.Deployment{}, err
	}

	changes := []Change{{ID: d.ID, Status: models.DeploymentRunning, Traffic: in.TrafficPercent, Reason: "deployed"}}
	if role == models.RoleCanary {
		changes = append(changes, Change{ID: primary.ID, Status: models.DeploymentRunning, Traffic: 100 - in.TrafficPercent})
	} else {
		for _, old := range running {
			changes = append(changes, Change{ID: old.ID, Status: models.DeploymentStopped, Traffic: 0, Reason: "replaced by " + in.Version})
		}
	}
	if err := s.repo.Apply(ctx, changes); err != nil {
		return models.Deployment{}, err
	}

	// Keep the registry's stage in step with what is actually serving.
	stage := models.StageStaging
	if role == models.RolePrimary {
		stage = models.StageProduction
	}
	if v.Stage != stage {
		if _, err := s.versions.SetStage(ctx, in.Model, in.Version, stage); err != nil {
			s.log.WarnContext(ctx, "deployment: registry stage update failed", "err", err)
		}
	}

	s.changed(ctx, in.Model)
	s.log.InfoContext(ctx, "deployed", "model", in.Model, "version", in.Version, "role", role, "traffic", in.TrafficPercent)
	return s.repo.Get(ctx, d.ID)
}

// SetTraffic changes a running canary's share; the primary gets the rest.
func (s *Service) SetTraffic(ctx context.Context, id uuid.UUID, pct int) (models.Deployment, error) {
	if pct < 1 || pct > 99 {
		return models.Deployment{}, fmt.Errorf("%w: canary traffic must be 1-99 (use promote for 100)", ErrInvalid)
	}
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return models.Deployment{}, err
	}
	defer s.lock(d.ModelName)()

	primary, canary, err := s.runningPair(ctx, d.ModelName)
	if err != nil {
		return models.Deployment{}, err
	}
	if canary == nil || canary.ID != id {
		return models.Deployment{}, fmt.Errorf("%w: only a running canary's traffic can be changed", ErrInvalid)
	}
	if err := s.repo.Apply(ctx, []Change{
		{ID: canary.ID, Status: models.DeploymentRunning, Traffic: pct},
		{ID: primary.ID, Status: models.DeploymentRunning, Traffic: 100 - pct},
	}); err != nil {
		return models.Deployment{}, err
	}
	s.changed(ctx, d.ModelName)
	return s.repo.Get(ctx, id)
}

// Promote makes a running canary the primary at 100%.
func (s *Service) Promote(ctx context.Context, id uuid.UUID) (models.Deployment, error) {
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return models.Deployment{}, err
	}
	defer s.lock(d.ModelName)()

	primary, canary, err := s.runningPair(ctx, d.ModelName)
	if err != nil {
		return models.Deployment{}, err
	}
	if canary == nil || canary.ID != id {
		return models.Deployment{}, fmt.Errorf("%w: only a running canary can be promoted", ErrInvalid)
	}
	if err := s.repo.Apply(ctx, []Change{
		{ID: primary.ID, Status: models.DeploymentStopped, Traffic: 0, Reason: "replaced by " + canary.Version},
		{ID: canary.ID, Status: models.DeploymentRunning, Traffic: 100, Reason: "promoted from canary", Role: models.RolePrimary},
	}); err != nil {
		return models.Deployment{}, err
	}
	if _, err := s.versions.SetStage(ctx, d.ModelName, canary.Version, models.StageProduction); err != nil {
		s.log.WarnContext(ctx, "deployment: registry stage update failed", "err", err)
	}
	s.unload(ctx, d.ModelName, primary.Version)
	s.changed(ctx, d.ModelName)
	s.log.InfoContext(ctx, "canary promoted", "model", d.ModelName, "version", canary.Version)
	return s.repo.Get(ctx, id)
}

// Rollback undoes the most recent change for a model:
//   - a running canary is rolled back and the primary returns to 100%;
//   - otherwise the previous primary version is redeployed at 100%.
func (s *Service) Rollback(ctx context.Context, model, reason string) (models.Deployment, error) {
	unlock := s.lock(model)
	primary, canary, err := s.runningPair(ctx, model)
	if err != nil {
		unlock()
		return models.Deployment{}, err
	}

	if canary != nil {
		err := s.repo.Apply(ctx, []Change{
			{ID: canary.ID, Status: models.DeploymentRolledBack, Traffic: 0, Reason: reason},
			{ID: primary.ID, Status: models.DeploymentRunning, Traffic: 100},
		})
		unlock()
		if err != nil {
			return models.Deployment{}, err
		}
		if _, err := s.versions.SetStage(ctx, model, canary.Version, models.StageArchived); err != nil {
			s.log.WarnContext(ctx, "rollback: registry stage update failed", "err", err)
		}
		s.unload(ctx, model, canary.Version)
		s.changed(ctx, model)
		s.log.WarnContext(ctx, "canary rolled back", "model", model, "version", canary.Version, "reason", reason)
		return s.repo.Get(ctx, primary.ID)
	}

	if primary == nil {
		unlock()
		return models.Deployment{}, fmt.Errorf("%w: %s has no running deployment", ErrNothingToRollback, model)
	}
	prev, err := s.repo.PreviousPrimary(ctx, model, primary.ModelVersionID)
	unlock() // Deploy takes the lock itself
	if errors.Is(err, ErrNotFound) {
		return models.Deployment{}, fmt.Errorf("%w: %s has no earlier primary version", ErrNothingToRollback, model)
	}
	if err != nil {
		return models.Deployment{}, err
	}

	// Re-deploying requires the version not be archived; un-archive it first.
	if _, err := s.versions.SetStage(ctx, model, prev.Version, models.StageStaging); err != nil {
		return models.Deployment{}, wrapRegistry(err)
	}
	d, err := s.Deploy(ctx, DeployInput{Model: model, Version: prev.Version, TrafficPercent: 100})
	if err != nil {
		return models.Deployment{}, err
	}
	// Mark the version we moved away from as rolled back, not merely stopped.
	_ = s.repo.Apply(ctx, []Change{{ID: primary.ID, Status: models.DeploymentRolledBack, Traffic: 0, Reason: reason}})
	s.log.WarnContext(ctx, "primary rolled back", "model", model, "from", primary.Version, "to", prev.Version, "reason", reason)
	return d, nil
}

// Stop takes a deployment out of service. A canary stop is a rollback; a
// primary can only be stopped when no canary depends on it.
func (s *Service) Stop(ctx context.Context, id uuid.UUID) (models.Deployment, error) {
	d, err := s.repo.Get(ctx, id)
	if err != nil {
		return models.Deployment{}, err
	}
	if d.Status != models.DeploymentRunning {
		return models.Deployment{}, fmt.Errorf("%w: deployment is %s", ErrInvalid, d.Status)
	}
	if d.Role == models.RoleCanary {
		if _, err := s.Rollback(ctx, d.ModelName, "stopped manually"); err != nil {
			return models.Deployment{}, err
		}
		return s.repo.Get(ctx, id)
	}

	defer s.lock(d.ModelName)()
	_, canary, err := s.runningPair(ctx, d.ModelName)
	if err != nil {
		return models.Deployment{}, err
	}
	if canary != nil {
		return models.Deployment{}, fmt.Errorf("%w: promote or roll back the canary before stopping the primary", ErrConflict)
	}
	if err := s.repo.Apply(ctx, []Change{{ID: id, Status: models.DeploymentStopped, Traffic: 0, Reason: "stopped manually"}}); err != nil {
		return models.Deployment{}, err
	}
	s.changed(ctx, d.ModelName)
	return s.repo.Get(ctx, id)
}

func (s *Service) List(ctx context.Context, model string) ([]models.Deployment, error) {
	return s.repo.List(ctx, model, 100)
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (models.Deployment, error) {
	return s.repo.Get(ctx, id)
}

// RunningCanaries lists every running canary across all models (for the rollback monitor).
func (s *Service) RunningCanaries(ctx context.Context) ([]models.Deployment, error) {
	all, err := s.repo.List(ctx, "", 500)
	if err != nil {
		return nil, err
	}
	var out []models.Deployment
	for _, d := range all {
		if d.Status == models.DeploymentRunning && d.Role == models.RoleCanary {
			out = append(out, d)
		}
	}
	return out, nil
}

// Routes implements prediction.RouteSource: running deployments if any,
// otherwise the registry's production version at 100%.
func (s *Service) Routes(ctx context.Context, model string) ([]prediction.Route, error) {
	running, err := s.repo.Running(ctx, model)
	if err != nil {
		return nil, err
	}
	if len(running) > 0 {
		routes := make([]prediction.Route, 0, len(running))
		for _, d := range running {
			id := d.ID
			role := prediction.RolePrimary
			if d.Role == models.RoleCanary {
				role = prediction.RoleCanary
			}
			routes = append(routes, prediction.Route{
				DeploymentID: &id, VersionID: d.ModelVersionID, ModelName: d.ModelName,
				Version: d.Version, Weight: d.TrafficPercent, Role: role,
			})
		}
		return routes, nil
	}

	versions, err := s.versions.ListVersions(ctx, model)
	if err != nil {
		if errors.Is(err, registry.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	for _, v := range versions {
		if v.Stage == models.StageProduction {
			return []prediction.Route{{VersionID: v.ID, ModelName: model, Version: v.Version, Weight: 100, Role: prediction.RoleProduction}}, nil
		}
	}
	return nil, nil
}

// --- helpers ---------------------------------------------------------------

func split(running []models.Deployment) (primary, canary *models.Deployment) {
	for i := range running {
		switch running[i].Role {
		case models.RolePrimary:
			primary = &running[i]
		case models.RoleCanary:
			canary = &running[i]
		}
	}
	return primary, canary
}

func (s *Service) runningPair(ctx context.Context, model string) (primary, canary *models.Deployment, err error) {
	running, err := s.repo.Running(ctx, model)
	if err != nil {
		return nil, nil, err
	}
	primary, canary = split(running)
	return primary, canary, nil
}

func (s *Service) warm(ctx context.Context, model, version string) error {
	resp, err := s.ml.LoadModel(ctx, model, version)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrWarmup, err)
	}
	if resp.StatusCode != 0 {
		return fmt.Errorf("%w: %s", ErrWarmup, resp.ErrorMessage)
	}
	return nil
}

func (s *Service) unload(ctx context.Context, model, version string) {
	if _, err := s.ml.UnloadModel(context.WithoutCancel(ctx), model, version); err != nil {
		s.log.WarnContext(ctx, "unload failed (session stays cached)", "model", model, "version", version, "err", err)
	}
}

// changed refreshes everything derived from the running set.
func (s *Service) changed(ctx context.Context, model string) {
	s.onChange(model)
	running, err := s.repo.Running(ctx, model)
	if err != nil {
		return
	}
	metrics.TrafficPercent.DeletePartialMatch(map[string]string{"model": model})
	for _, d := range running {
		metrics.TrafficPercent.WithLabelValues(model, d.Version).Set(float64(d.TrafficPercent))
	}
}

func wrapRegistry(err error) error {
	switch {
	case errors.Is(err, registry.ErrNotFound):
		return fmt.Errorf("%w: %v", ErrNotFound, err)
	case errors.Is(err, registry.ErrInvalid):
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return err
}
