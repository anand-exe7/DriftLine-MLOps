package alert

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"driftline/go/internal/models"
)

func sampleReport() models.DriftReport {
	end := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	return models.DriftReport{
		ID:             uuid.New(),
		ModelVersionID: uuid.New(),
		ModelName:      "loan_default_xgb",
		Version:        "v1",
		WindowStart:    end.Add(-time.Hour),
		WindowEnd:      end,
		SampleSize:     500,
		OverallPSI:     0.31,
		DriftDetected:  true,
		FeatureScores: map[string]models.FeatureDrift{
			"Income": {PSI: 0.42}, "Age": {PSI: 0.05}, "InterestRate": {PSI: 0.2},
		},
	}
}

// capture records every request body the fake webhook receives and answers
// with the status codes in `statuses`, one per call (last one repeats).
type capture struct {
	mu       sync.Mutex
	bodies   []map[string]any
	statuses []int
}

func (c *capture) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type = %q", r.Header.Get("Content-Type"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		c.mu.Lock()
		c.bodies = append(c.bodies, body)
		status := c.statuses[min(len(c.bodies), len(c.statuses))-1]
		c.mu.Unlock()

		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (c *capture) calls() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.bodies)
}

func fastRetries(h *webhook) { h.backoff = time.Millisecond }

func TestSlackPayload(t *testing.T) {
	c := &capture{statuses: []int{200}}
	n := NewSlackWebhookNotifier(c.server(t).URL, nil)

	if err := n.NotifyDrift(context.Background(), sampleReport()); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(c.bodies[0])
	s := string(raw)
	for _, want := range []string{"loan_default_xgb/v1", "0.310", "blocks", ":rotating_light:", "Income: PSI 0.420"} {
		if !strings.Contains(s, want) {
			t.Errorf("payload missing %q: %s", want, s)
		}
	}
	// Highest-PSI feature must be listed first.
	if strings.Index(s, "Income") > strings.Index(s, "InterestRate") {
		t.Errorf("features not sorted by PSI: %s", s)
	}
}

func TestDiscordPayload(t *testing.T) {
	c := &capture{statuses: []int{204}} // Discord answers 204 No Content
	n := NewDiscordWebhookNotifier(c.server(t).URL, nil)

	if err := n.NotifyDrift(context.Background(), sampleReport()); err != nil {
		t.Fatal(err)
	}
	embeds, ok := c.bodies[0]["embeds"].([]any)
	if !ok || len(embeds) != 1 {
		t.Fatalf("expected one embed: %v", c.bodies[0])
	}
	embed := embeds[0].(map[string]any)
	if embed["title"] != "Drift detected: loan_default_xgb/v1" || embed["color"].(float64) != 0xE01E5A {
		t.Errorf("unexpected embed: %v", embed)
	}
}

func TestWebhookRetryPolicy(t *testing.T) {
	tests := []struct {
		name      string
		statuses  []int
		wantErr   bool
		wantCalls int
	}{
		{"success first try", []int{200}, false, 1},
		{"retries 5xx then succeeds", []int{503, 500, 200}, false, 3},
		{"retries 429", []int{429, 200}, false, 2},
		{"gives up after max retries", []int{500}, true, 3},
		{"no retry on 400", []int{400}, true, 1},
		{"no retry on 404", []int{404}, true, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &capture{statuses: tc.statuses}
			n := NewSlackWebhookNotifier(c.server(t).URL, nil)
			fastRetries(&n.hook)

			err := n.NotifyDrift(context.Background(), sampleReport())
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if c.calls() != tc.wantCalls {
				t.Fatalf("calls = %d, want %d", c.calls(), tc.wantCalls)
			}
		})
	}
}

func TestSeverity(t *testing.T) {
	tests := []struct {
		psi      float64
		detected bool
		want     models.Severity
	}{
		{0.30, true, models.SeverityCritical},
		{0.25, false, models.SeverityCritical},
		{0.15, false, models.SeverityWarning},
		{0.05, true, models.SeverityWarning},
		{0.05, false, models.SeverityInfo},
	}
	for _, tc := range tests {
		got := severityOf(models.DriftReport{OverallPSI: tc.psi, DriftDetected: tc.detected})
		if got != tc.want {
			t.Errorf("psi=%.2f detected=%v: got %s, want %s", tc.psi, tc.detected, got, tc.want)
		}
	}
}

// ---- Service ----------------------------------------------------------------

type stubNotifier struct {
	name  string
	err   error
	calls atomic.Int32
}

func (s *stubNotifier) Name() string { return s.name }
func (s *stubNotifier) NotifyDrift(context.Context, models.DriftReport) error {
	s.calls.Add(1)
	return s.err
}

type memRepo struct {
	mu     sync.Mutex
	alerts []models.Alert
}

func (m *memRepo) Insert(_ context.Context, a *models.Alert) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = append(m.alerts, *a)
	return nil
}

func (m *memRepo) ListRecent(context.Context, int) ([]models.Alert, error) { return m.alerts, nil }

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestServiceFansOutAndRecordsEachChannel(t *testing.T) {
	ok := &stubNotifier{name: "slack"}
	bad := &stubNotifier{name: "discord", err: errors.New("boom")}
	repo := &memRepo{}
	svc := NewService([]AlertNotifier{ok, bad}, repo, quietLogger(), time.Hour)

	err := svc.HandleDriftReport(context.Background(), sampleReport())
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected joined error containing boom, got %v", err)
	}
	if ok.calls.Load() != 1 || bad.calls.Load() != 1 {
		t.Fatal("every notifier should be called once")
	}

	statuses := map[string]string{}
	for _, a := range repo.alerts {
		statuses[a.Channel] = a.Status
	}
	if statuses["slack"] != "sent" || statuses["discord"] != "failed" {
		t.Fatalf("recorded = %v", statuses)
	}
}

func TestServiceSkipsWhenNoDrift(t *testing.T) {
	n := &stubNotifier{name: "slack"}
	svc := NewService([]AlertNotifier{n}, &memRepo{}, quietLogger(), time.Hour)

	r := sampleReport()
	r.DriftDetected = false
	if err := svc.HandleDriftReport(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if n.calls.Load() != 0 {
		t.Fatal("should not notify without drift")
	}
}

func TestServiceCooldown(t *testing.T) {
	n := &stubNotifier{name: "slack"}
	svc := NewService([]AlertNotifier{n}, &memRepo{}, quietLogger(), 30*time.Minute)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }

	r := sampleReport()
	other := sampleReport() // different ModelVersionID

	_ = svc.HandleDriftReport(context.Background(), r)
	_ = svc.HandleDriftReport(context.Background(), r) // suppressed
	_ = svc.HandleDriftReport(context.Background(), other)
	if got := n.calls.Load(); got != 2 {
		t.Fatalf("calls = %d, want 2 (second report for same version suppressed)", got)
	}

	now = now.Add(31 * time.Minute)
	_ = svc.HandleDriftReport(context.Background(), r)
	if got := n.calls.Load(); got != 3 {
		t.Fatalf("calls = %d, want 3 after cooldown expired", got)
	}
}

func TestServiceCooldownIsRaceFree(t *testing.T) {
	n := &stubNotifier{name: "slack"}
	svc := NewService([]AlertNotifier{n}, &memRepo{}, quietLogger(), time.Hour)
	r := sampleReport()

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = svc.HandleDriftReport(context.Background(), r)
		}()
	}
	wg.Wait()
	if got := n.calls.Load(); got != 1 {
		t.Fatalf("calls = %d, want exactly 1 under concurrency", got)
	}
}
