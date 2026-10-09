package drift

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"driftline/go/internal/metrics"
	"driftline/go/internal/models"
	"driftline/go/internal/worker"
)

// AlertSink receives drift reports (the alert service implements it).
type AlertSink interface {
	HandleDriftReport(ctx context.Context, report models.DriftReport) error
}

// ReportObserver sees every completed run (the rollback manager implements it).
type ReportObserver interface {
	OnDriftRun(ctx context.Context, reports []CheckedReport)
}

// CheckedReport pairs a saved report with the routing role of its version.
type CheckedReport struct {
	Report models.DriftReport
	Role   string
}

type Config struct {
	Interval   time.Duration // how often to check
	Window     time.Duration // how far back to look at live traffic
	MinSamples int           // skip versions with less traffic than this
	MaxSamples int           // cap rows loaded per version
	Workers    int           // versions checked concurrently
	Thresholds Thresholds
}

// Scheduler runs a drift check on a time.Ticker. Each run fans out one job
// per serving version to a bounded worker pool.
type Scheduler struct {
	repo     Repository
	alerts   AlertSink
	observer ReportObserver
	cfg      Config
	log      *slog.Logger

	runMu sync.Mutex // one run at a time (ticker vs. manual trigger)
}

func NewScheduler(repo Repository, alerts AlertSink, observer ReportObserver, cfg Config, log *slog.Logger) *Scheduler {
	if cfg.Workers < 1 {
		cfg.Workers = 4
	}
	if cfg.MaxSamples < 1 {
		cfg.MaxSamples = 5000
	}
	return &Scheduler{repo: repo, alerts: alerts, observer: observer, cfg: cfg, log: log}
}

// Start runs checks until ctx is cancelled. The returned channel closes when
// the loop has fully stopped, so shutdown can wait for an in-flight run.
func (s *Scheduler) Start(ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(s.cfg.Interval)
		defer ticker.Stop()
		s.log.Info("drift scheduler started", "interval", s.cfg.Interval, "window", s.cfg.Window)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := s.RunOnce(ctx); err != nil && ctx.Err() == nil {
					s.log.Error("drift run failed", "err", err)
				}
			}
		}
	}()
	return done
}

// RunOnce checks every serving version now and returns the saved reports.
func (s *Scheduler) RunOnce(ctx context.Context) ([]models.DriftReport, error) {
	s.runMu.Lock()
	defer s.runMu.Unlock()

	targets, err := s.repo.Targets(ctx)
	if err != nil {
		return nil, err
	}
	end := time.Now().UTC()
	start := end.Add(-s.cfg.Window)

	results, errs := worker.Run(ctx, s.cfg.Workers, targets, func(ctx context.Context, t Target) (*models.DriftReport, error) {
		return s.check(ctx, t, start, end)
	})

	var reports []models.DriftReport
	var checked []CheckedReport
	for i, r := range results {
		if errs[i] != nil {
			metrics.DriftChecks.WithLabelValues("error").Inc()
			s.log.Error("drift check failed", "model", targets[i].Model, "version", targets[i].Version, "err", errs[i])
			continue
		}
		if r == nil {
			continue // skipped: not enough traffic
		}
		reports = append(reports, *r)
		checked = append(checked, CheckedReport{Report: *r, Role: targets[i].Role})

		if r.DriftDetected && s.alerts != nil {
			if err := s.alerts.HandleDriftReport(ctx, *r); err != nil {
				s.log.Warn("drift alert delivery had errors", "model", r.ModelName, "err", err)
			}
		}
	}
	if s.observer != nil && len(checked) > 0 {
		s.observer.OnDriftRun(ctx, checked)
	}
	return reports, nil
}

func (s *Scheduler) check(ctx context.Context, t Target, start, end time.Time) (*models.DriftReport, error) {
	baseline, err := ParseBaseline(t.Baseline)
	if err != nil {
		return nil, err
	}
	samples, err := s.repo.Samples(ctx, t.VersionID, start, s.cfg.MaxSamples)
	if err != nil {
		return nil, err
	}
	if len(samples) < s.cfg.MinSamples {
		metrics.DriftChecks.WithLabelValues("skipped").Inc()
		return nil, nil
	}

	res := Analyze(baseline, samples, s.cfg.Thresholds)
	report := models.DriftReport{
		ModelVersionID: t.VersionID,
		ModelName:      t.Model,
		Version:        t.Version,
		WindowStart:    start,
		WindowEnd:      end,
		SampleSize:     res.SampleSize,
		OverallPSI:     res.OverallPSI,
		DriftDetected:  res.DriftDetected,
		FeatureScores:  res.Features,
	}
	if err := s.repo.Save(ctx, &report); err != nil {
		return nil, err
	}

	result := "ok"
	if report.DriftDetected {
		result = "drift"
	}
	metrics.DriftChecks.WithLabelValues(result).Inc()
	metrics.DriftPSI.WithLabelValues(t.Model, t.Version, "_overall").Set(report.OverallPSI)
	for name, f := range report.FeatureScores {
		metrics.DriftPSI.WithLabelValues(t.Model, t.Version, name).Set(f.PSI)
	}
	s.log.Info("drift checked", "model", t.Model, "version", t.Version, "samples", report.SampleSize,
		"psi", report.OverallPSI, "drift", report.DriftDetected)
	return &report, nil
}
