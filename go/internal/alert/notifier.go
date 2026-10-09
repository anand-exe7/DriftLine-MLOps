// Package alert turns drift reports into notifications on external channels.
package alert

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"driftline/go/internal/models"
)

// AlertNotifier is implemented by every outbound channel. The drift engine
// (through Service) depends only on this interface, so adding email or
// PagerDuty later means adding a type, not touching callers.
type AlertNotifier interface {
	NotifyDrift(ctx context.Context, report models.DriftReport) error
	Name() string
}

// ---------------------------------------------------------------------------
// Shared webhook plumbing

type webhook struct {
	url        string
	client     *http.Client
	maxRetries int
	backoff    time.Duration
}

func newWebhook(url string, client *http.Client) webhook {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return webhook{url: url, client: client, maxRetries: 3, backoff: 500 * time.Millisecond}
}

// post sends payload as JSON, retrying on network errors, 429 and 5xx with
// linear backoff. 4xx (other than 429) means the payload or URL is wrong and
// retrying won't help, so it fails immediately.
func (w webhook) post(ctx context.Context, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= w.maxRetries; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := w.client.Do(req)
		if err == nil {
			snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
			resp.Body.Close()
			if resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("webhook returned %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
			if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode < 500 {
				return lastErr
			}
		} else {
			lastErr = err
		}

		if attempt == w.maxRetries {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * w.backoff):
		}
	}
	return fmt.Errorf("after %d attempts: %w", w.maxRetries, lastErr)
}

// ---------------------------------------------------------------------------
// Message formatting shared by both channels

func severityOf(r models.DriftReport) models.Severity {
	switch {
	case r.OverallPSI >= 0.25:
		return models.SeverityCritical
	case r.DriftDetected || r.OverallPSI >= 0.1:
		return models.SeverityWarning
	default:
		return models.SeverityInfo
	}
}

func title(r models.DriftReport) string {
	return fmt.Sprintf("Drift detected: %s/%s", r.ModelName, r.Version)
}

func summary(r models.DriftReport) string {
	return fmt.Sprintf("Overall PSI %.3f over %d predictions (%s to %s)",
		r.OverallPSI, r.SampleSize,
		r.WindowStart.UTC().Format(time.RFC3339), r.WindowEnd.UTC().Format(time.RFC3339))
}

type featurePSI struct {
	name string
	psi  float64
}

// topFeatures returns the n most-drifted features, highest PSI first.
func topFeatures(r models.DriftReport, n int) []featurePSI {
	out := make([]featurePSI, 0, len(r.FeatureScores))
	for name, s := range r.FeatureScores {
		out = append(out, featurePSI{name, s.PSI})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].psi != out[j].psi {
			return out[i].psi > out[j].psi
		}
		return out[i].name < out[j].name
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func featureLines(r models.DriftReport) string {
	var b strings.Builder
	for _, f := range topFeatures(r, 5) {
		fmt.Fprintf(&b, "• %s: PSI %.3f\n", f.name, f.psi)
	}
	return strings.TrimRight(b.String(), "\n")
}

// ---------------------------------------------------------------------------
// Slack (incoming webhooks + Block Kit)

type SlackWebhookNotifier struct{ hook webhook }

func NewSlackWebhookNotifier(url string, client *http.Client) *SlackWebhookNotifier {
	return &SlackWebhookNotifier{hook: newWebhook(url, client)}
}

func (s *SlackWebhookNotifier) Name() string { return "slack" }

func (s *SlackWebhookNotifier) NotifyDrift(ctx context.Context, r models.DriftReport) error {
	emoji := map[models.Severity]string{
		models.SeverityCritical: ":rotating_light:",
		models.SeverityWarning:  ":warning:",
		models.SeverityInfo:     ":information_source:",
	}[severityOf(r)]

	blocks := []map[string]any{
		{"type": "header", "text": map[string]any{"type": "plain_text", "text": title(r)}},
		{"type": "section", "text": map[string]any{"type": "mrkdwn", "text": emoji + " " + summary(r)}},
	}
	if lines := featureLines(r); lines != "" {
		blocks = append(blocks, map[string]any{
			"type": "section",
			"text": map[string]any{"type": "mrkdwn", "text": "*Top drifted features*\n" + lines},
		})
	}

	// "text" is the fallback shown in notifications and by clients without Block Kit.
	if err := s.hook.post(ctx, map[string]any{"text": title(r) + " — " + summary(r), "blocks": blocks}); err != nil {
		return fmt.Errorf("slack: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Discord (webhooks + embeds)

type DiscordWebhookNotifier struct{ hook webhook }

func NewDiscordWebhookNotifier(url string, client *http.Client) *DiscordWebhookNotifier {
	return &DiscordWebhookNotifier{hook: newWebhook(url, client)}
}

func (d *DiscordWebhookNotifier) Name() string { return "discord" }

func (d *DiscordWebhookNotifier) NotifyDrift(ctx context.Context, r models.DriftReport) error {
	color := map[models.Severity]int{
		models.SeverityCritical: 0xE01E5A,
		models.SeverityWarning:  0xECB22E,
		models.SeverityInfo:     0x36C5F0,
	}[severityOf(r)]

	embed := map[string]any{
		"title":       title(r),
		"description": summary(r),
		"color":       color,
		"timestamp":   r.WindowEnd.UTC().Format(time.RFC3339),
	}
	if lines := featureLines(r); lines != "" {
		embed["fields"] = []map[string]any{{"name": "Top drifted features", "value": lines}}
	}

	if err := d.hook.post(ctx, map[string]any{"username": "Driftline", "embeds": []any{embed}}); err != nil {
		return fmt.Errorf("discord: %w", err)
	}
	return nil
}
