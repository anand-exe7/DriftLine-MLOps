package alert

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"driftline/go/internal/models"
)

// Service is what the drift engine calls. It decides *whether* to alert
// (drift detected, not in cooldown), fans out to every configured notifier
// concurrently, and records one alerts row per channel attempt.
type Service struct {
	notifiers []AlertNotifier
	repo      Repository
	log       *slog.Logger
	cooldown  time.Duration
	timeout   time.Duration
	now       func() time.Time // injectable for tests

	mu       sync.Mutex
	lastSent map[uuid.UUID]time.Time // model_version_id -> last alert time
}

func NewService(notifiers []AlertNotifier, repo Repository, log *slog.Logger, cooldown time.Duration) *Service {
	return &Service{
		notifiers: notifiers,
		repo:      repo,
		log:       log,
		cooldown:  cooldown,
		timeout:   15 * time.Second,
		now:       time.Now,
		lastSent:  make(map[uuid.UUID]time.Time),
	}
}

// Channels lists the configured notifier names.
func (s *Service) Channels() []string {
	names := make([]string, len(s.notifiers))
	for i, n := range s.notifiers {
		names[i] = n.Name()
	}
	return names
}

// HandleDriftReport is the drift engine's entry point. It returns nil when no
// alert was needed, and a joined error if any channel failed.
func (s *Service) HandleDriftReport(ctx context.Context, report models.DriftReport) error {
	if !report.DriftDetected {
		return nil
	}
	if !s.claimCooldown(report.ModelVersionID) {
		s.log.InfoContext(ctx, "alert suppressed by cooldown",
			"model", report.ModelName, "version", report.Version, "cooldown", s.cooldown)
		return nil
	}
	return s.dispatch(ctx, report)
}

// SendTest bypasses the drift/cooldown checks; used by POST /alerts/test to
// verify webhook URLs.
func (s *Service) SendTest(ctx context.Context, report models.DriftReport) error {
	return s.dispatch(ctx, report)
}

// claimCooldown reports whether an alert may be sent for this version now,
// and if so records the send time. Check-and-set happens under one lock so
// two concurrent drift reports for the same version can't both pass.
func (s *Service) claimCooldown(versionID uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if last, ok := s.lastSent[versionID]; ok && now.Sub(last) < s.cooldown {
		return false
	}
	s.lastSent[versionID] = now
	return true
}

func (s *Service) dispatch(ctx context.Context, report models.DriftReport) error {
	if len(s.notifiers) == 0 {
		s.log.WarnContext(ctx, "drift detected but no alert channels configured",
			"model", report.ModelName, "version", report.Version, "psi", report.OverallPSI)
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	// One goroutine per channel: a slow Slack shouldn't delay Discord.
	errs := make([]error, len(s.notifiers))
	var wg sync.WaitGroup
	for i, n := range s.notifiers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := n.NotifyDrift(ctx, report)
			errs[i] = err // each goroutine writes only its own index: no lock needed
			s.record(ctx, report, n.Name(), err)
		}()
	}
	wg.Wait()

	return errors.Join(errs...)
}

func (s *Service) record(ctx context.Context, report models.DriftReport, channel string, sendErr error) {
	a := models.Alert{
		Severity: severityOf(report),
		Channel:  channel,
		Message:  title(report) + " — " + summary(report),
		Status:   "sent",
	}
	if report.ModelVersionID != uuid.Nil {
		a.ModelVersionID = &report.ModelVersionID
	}
	if report.ID != uuid.Nil {
		a.DriftReportID = &report.ID
	}
	if sendErr != nil {
		a.Status = "failed"
		a.Error = sendErr.Error()
		s.log.ErrorContext(ctx, "alert failed", "channel", channel, "model", report.ModelName, "err", sendErr)
	} else {
		s.log.InfoContext(ctx, "alert sent", "channel", channel, "model", report.ModelName, "version", report.Version)
	}

	if s.repo == nil {
		return
	}
	// Persist even if the send timed out, hence WithoutCancel.
	if err := s.repo.Insert(context.WithoutCancel(ctx), &a); err != nil {
		s.log.ErrorContext(ctx, "failed to record alert", "err", fmt.Errorf("alert repo: %w", err))
	}
}
