package prediction

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"driftline/go/internal/metrics"
	"driftline/go/internal/models"
)

// LogSink receives prediction logs. Enqueue must never block the request path.
type LogSink interface {
	Enqueue(models.PredictionLog)
}

// LogWriter buffers prediction logs in a channel and writes them to Postgres
// in batches with COPY, which is far cheaper than one INSERT per prediction.
//
//	request goroutines --Enqueue--> [buffered chan] --> one writer goroutine --COPY--> prediction_logs
//
// If the buffer is full (DB slow or down) logs are dropped and counted rather
// than slowing predictions: serving traffic matters more than logging it.
type LogWriter struct {
	pool      *pgxpool.Pool
	log       *slog.Logger
	ch        chan models.PredictionLog
	batchSize int
	interval  time.Duration
	wg        sync.WaitGroup
}

func NewLogWriter(pool *pgxpool.Pool, log *slog.Logger, buffer int) *LogWriter {
	return &LogWriter{
		pool:      pool,
		log:       log,
		ch:        make(chan models.PredictionLog, buffer),
		batchSize: 500,
		interval:  time.Second,
	}
}

func (w *LogWriter) Enqueue(l models.PredictionLog) {
	select {
	case w.ch <- l:
	default:
		metrics.PredictionLogsDropped.Inc()
	}
}

// Start runs the writer until Close. Flushes when a batch fills or every interval.
func (w *LogWriter) Start() {
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()

		batch := make([]models.PredictionLog, 0, w.batchSize)
		flush := func() {
			if len(batch) == 0 {
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := w.write(ctx, batch); err != nil {
				w.log.Error("prediction logs: write failed", "rows", len(batch), "err", err)
			} else {
				metrics.PredictionLogsWritten.Add(float64(len(batch)))
			}
			batch = batch[:0]
		}

		for {
			select {
			case l, ok := <-w.ch:
				if !ok { // Close() was called: drain done, final flush
					flush()
					return
				}
				batch = append(batch, l)
				if len(batch) >= w.batchSize {
					flush()
				}
			case <-ticker.C:
				flush()
			}
		}
	}()
}

// Close stops accepting logs and blocks until everything buffered is written.
func (w *LogWriter) Close() {
	close(w.ch)
	w.wg.Wait()
}

func (w *LogWriter) write(ctx context.Context, batch []models.PredictionLog) error {
	rows := make([][]any, 0, len(batch))
	for _, l := range batch {
		features, err := json.Marshal(l.Features)
		if err != nil {
			return fmt.Errorf("marshal features: %w", err)
		}
		rows = append(rows, []any{
			l.RequestID, l.ModelVersionID, l.DeploymentID, features, l.Prediction,
			l.Probabilities, l.LatencyMS, l.StatusCode, l.ErrorMessage, l.CreatedAt,
		})
	}
	_, err := w.pool.CopyFrom(ctx,
		pgx.Identifier{"prediction_logs"},
		[]string{"request_id", "model_version_id", "deployment_id", "features", "prediction",
			"probabilities", "latency_ms", "status_code", "error_message", "created_at"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// Stats is a per-version summary of logged traffic, for the dashboard.
type Stats struct {
	ModelVersionID string  `json:"model_version_id"`
	ModelName      string  `json:"model_name"`
	Version        string  `json:"version"`
	Requests       int64   `json:"requests"`
	Errors         int64   `json:"errors"`
	DefaultRate    float64 `json:"default_rate"` // share of OK predictions == 1
	AvgLatencyMS   float64 `json:"avg_latency_ms"`
}

// StatsSince aggregates prediction_logs per version since t.
func StatsSince(ctx context.Context, pool *pgxpool.Pool, model string, since time.Time) ([]Stats, error) {
	rows, err := pool.Query(ctx, `
		SELECT v.id::text, m.name, v.version,
		       count(*),
		       count(*) FILTER (WHERE l.status_code <> 0),
		       coalesce(avg((l.prediction = 1)::int) FILTER (WHERE l.status_code = 0), 0),
		       coalesce(avg(l.latency_ms), 0)
		FROM prediction_logs l
		JOIN model_versions v ON v.id = l.model_version_id
		JOIN models m ON m.id = v.model_id
		WHERE m.name = $1 AND l.created_at >= $2
		GROUP BY v.id, m.name, v.version
		ORDER BY v.version`, model, since)
	if err != nil {
		return nil, fmt.Errorf("prediction stats: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Stats, error) {
		var s Stats
		err := row.Scan(&s.ModelVersionID, &s.ModelName, &s.Version, &s.Requests, &s.Errors, &s.DefaultRate, &s.AvgLatencyMS)
		return s, err
	})
}
