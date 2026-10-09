// Package rollback decides when to undo a release automatically.
//
// Two signals trigger an automatic canary rollback:
//
//  1. Health: on live traffic, the canary's error rate or its predicted
//     positive rate diverges from the primary's beyond configured limits.
//  2. Version-specific drift: the canary's inputs drift from *its* training
//     baseline while the primary, receiving the same traffic, does not. Then
//     the canary was trained on the wrong distribution.
//
// Drift on the primary itself does not roll back: an older model faces the
// same shifted inputs, so the fix is retraining. That case is alerted instead.
package rollback

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"driftline/go/internal/alert"
	"driftline/go/internal/drift"
	"driftline/go/internal/metrics"
	"driftline/go/internal/models"
	"driftline/go/internal/prediction"
)

type Deployer interface {
	Rollback(ctx context.Context, model, reason string) (models.Deployment, error)
	RunningCanaries(ctx context.Context) ([]models.Deployment, error)
}

// StatsSource returns per-version traffic stats for a model since t.
type StatsSource func(ctx context.Context, model string, since time.Time) ([]prediction.Stats, error)

type Notifier interface {
	NotifyEvent(ctx context.Context, ev alert.Event, modelVersionID uuid.UUID) error
}

type Config struct {
	Auto            bool          // perform automatic rollbacks (otherwise only alert)
	CheckInterval   time.Duration // health check cadence
	Window          time.Duration // traffic window compared
	MinRequests     int64         // canary requests needed before judging it
	MaxErrorRate    float64       // canary error rate that is always too high
	MaxErrorDelta   float64       // canary error rate allowed above primary's
	MaxPositiveDiff float64       // |canary - primary| predicted default rate allowed
}

type Manager struct {
	dep    Deployer
	stats  StatsSource
	notify Notifier
	cfg    Config
	log    *slog.Logger
}

func NewManager(dep Deployer, stats StatsSource, notify Notifier, cfg Config, log *slog.Logger) *Manager {
	return &Manager{dep: dep, stats: stats, notify: notify, cfg: cfg, log: log}
}

// Start runs the canary health check on a ticker until ctx is cancelled.
func (m *Manager) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(m.cfg.CheckInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				m.CheckCanaries(ctx)
			}
		}
	}()
	return done
}

// Verdict explains a health decision (returned for tests and logs).
type Verdict struct {
	Rollback bool
	Reason   string
}

// Judge compares a canary's live stats with the primary's.
func (m *Manager) Judge(canary, primary prediction.Stats) Verdict {
	if canary.Requests < m.cfg.MinRequests {
		return Verdict{}
	}
	cErr := float64(canary.Errors) / float64(canary.Requests)
	pErr := 0.0
	if primary.Requests > 0 {
		pErr = float64(primary.Errors) / float64(primary.Requests)
	}
	switch {
	case cErr >= m.cfg.MaxErrorRate && cErr-pErr >= m.cfg.MaxErrorDelta:
		return Verdict{true, fmt.Sprintf("canary error rate %.1f%% vs primary %.1f%%", cErr*100, pErr*100)}
	case primary.Requests >= m.cfg.MinRequests && abs(canary.DefaultRate-primary.DefaultRate) >= m.cfg.MaxPositiveDiff:
		return Verdict{true, fmt.Sprintf("canary predicts default for %.1f%% of applicants vs primary %.1f%%",
			canary.DefaultRate*100, primary.DefaultRate*100)}
	}
	return Verdict{}
}

// CheckCanaries evaluates every running canary once.
func (m *Manager) CheckCanaries(ctx context.Context) {
	canaries, err := m.dep.RunningCanaries(ctx)
	if err != nil {
		m.log.Error("rollback: list canaries", "err", err)
		return
	}
	for _, c := range canaries {
		stats, err := m.stats(ctx, c.ModelName, time.Now().Add(-m.cfg.Window))
		if err != nil {
			m.log.Error("rollback: canary stats", "model", c.ModelName, "err", err)
			continue
		}
		// Everything else logged for this model in the window is the primary
		// (possibly several versions if one was just replaced): pool them,
		// weighting each version's default rate by its successful requests.
		var cs, ps prediction.Stats
		var positives, okRequests float64
		for _, s := range stats {
			if s.ModelVersionID == c.ModelVersionID.String() {
				cs = s
				continue
			}
			ps.Requests += s.Requests
			ps.Errors += s.Errors
			ok := float64(s.Requests - s.Errors)
			positives += s.DefaultRate * ok
			okRequests += ok
		}
		if okRequests > 0 {
			ps.DefaultRate = positives / okRequests
		}
		if v := m.Judge(cs, ps); v.Rollback {
			m.act(ctx, c, "errors", v.Reason)
		}
	}
}

// OnDriftRun implements drift.ReportObserver.
func (m *Manager) OnDriftRun(ctx context.Context, reports []drift.CheckedReport) {
	byModel := map[string][]drift.CheckedReport{}
	for _, r := range reports {
		byModel[r.Report.ModelName] = append(byModel[r.Report.ModelName], r)
	}
	for model, rs := range byModel {
		var canary, primary *drift.CheckedReport
		for i := range rs {
			switch rs[i].Role {
			case string(models.RoleCanary):
				canary = &rs[i]
			case string(models.RolePrimary), "production":
				primary = &rs[i]
			}
		}

		if canary != nil && canary.Report.DriftDetected && primary != nil && !primary.Report.DriftDetected {
			canaries, err := m.dep.RunningCanaries(ctx)
			if err != nil {
				continue
			}
			for _, c := range canaries {
				if c.ModelName == model {
					m.act(ctx, c, "drift", fmt.Sprintf(
						"canary inputs drifted from its training baseline (PSI %.3f) while the primary's did not (PSI %.3f)",
						canary.Report.OverallPSI, primary.Report.OverallPSI))
				}
			}
			continue
		}

		if primary != nil && primary.Report.DriftDetected && m.notify != nil {
			_ = m.notify.NotifyEvent(ctx, alert.Event{
				Title:    fmt.Sprintf("Retraining recommended: %s/%s", model, primary.Report.Version),
				Text:     fmt.Sprintf("Production inputs have drifted (PSI %.3f). Rolling back would not help: an older model sees the same data. Retrain on recent data and deploy it as a canary.", primary.Report.OverallPSI),
				Severity: models.SeverityWarning,
			}, primary.Report.ModelVersionID)
		}
	}
}

func (m *Manager) act(ctx context.Context, canary models.Deployment, trigger, reason string) {
	title := fmt.Sprintf("Canary unhealthy: %s/%s", canary.ModelName, canary.Version)
	if !m.cfg.Auto {
		m.log.Warn("rollback recommended (auto-rollback disabled)", "model", canary.ModelName, "version", canary.Version, "reason", reason)
		m.event(ctx, canary, title+" (auto-rollback off)", reason, models.SeverityWarning)
		return
	}
	if _, err := m.dep.Rollback(ctx, canary.ModelName, "auto: "+reason); err != nil {
		m.log.Error("automatic rollback failed", "model", canary.ModelName, "err", err)
		return
	}
	metrics.Rollbacks.WithLabelValues(trigger).Inc()
	m.event(ctx, canary, fmt.Sprintf("Canary rolled back: %s/%s", canary.ModelName, canary.Version), reason, models.SeverityCritical)
}

func (m *Manager) event(ctx context.Context, d models.Deployment, title, text string, sev models.Severity) {
	if m.notify == nil {
		return
	}
	if err := m.notify.NotifyEvent(ctx, alert.Event{Title: title, Text: text, Severity: sev}, d.ModelVersionID); err != nil {
		m.log.Warn("rollback event notification failed", "err", err)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
