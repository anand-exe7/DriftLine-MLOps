package drift

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"driftline/go/internal/models"
)

// Target is a model version worth checking: it serves traffic and has a baseline.
type Target struct {
	VersionID uuid.UUID
	Model     string
	Version   string
	Role      string // "primary", "canary" or "production"
	Baseline  []byte
}

type Repository interface {
	Targets(ctx context.Context) ([]Target, error)
	Samples(ctx context.Context, versionID uuid.UUID, since time.Time, limit int) ([]map[string]float64, error)
	Save(ctx context.Context, r *models.DriftReport) error
	List(ctx context.Context, model string, limit int) ([]models.DriftReport, error)
}

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Targets: versions with a running deployment, plus production versions of
// models that have no running deployment (they still serve via the proxy).
func (r *PostgresRepository) Targets(ctx context.Context) ([]Target, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT v.id, m.name, v.version, d.role, v.baseline_stats
		FROM deployments d
		JOIN model_versions v ON v.id = d.model_version_id
		JOIN models m ON m.id = v.model_id
		WHERE d.status = 'running' AND v.baseline_stats IS NOT NULL
		UNION ALL
		SELECT v.id, m.name, v.version, 'production', v.baseline_stats
		FROM model_versions v
		JOIN models m ON m.id = v.model_id
		WHERE v.stage = 'production' AND v.baseline_stats IS NOT NULL
		  AND NOT EXISTS (
		      SELECT 1 FROM deployments d2
		      JOIN model_versions v2 ON v2.id = d2.model_version_id
		      WHERE v2.model_id = m.id AND d2.status = 'running')
		ORDER BY 2, 3`)
	if err != nil {
		return nil, fmt.Errorf("drift targets: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Target, error) {
		var t Target
		err := row.Scan(&t.VersionID, &t.Model, &t.Version, &t.Role, &t.Baseline)
		return t, err
	})
}

// Samples returns the feature maps of successful predictions in the window,
// newest first, capped at limit.
func (r *PostgresRepository) Samples(ctx context.Context, versionID uuid.UUID, since time.Time, limit int) ([]map[string]float64, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT features FROM prediction_logs
		WHERE model_version_id = $1 AND created_at >= $2 AND status_code = 0
		ORDER BY created_at DESC
		LIMIT $3`, versionID, since, limit)
	if err != nil {
		return nil, fmt.Errorf("drift samples: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (map[string]float64, error) {
		var raw []byte
		if err := row.Scan(&raw); err != nil {
			return nil, err
		}
		var m map[string]float64
		return m, json.Unmarshal(raw, &m)
	})
}

func (r *PostgresRepository) Save(ctx context.Context, rep *models.DriftReport) error {
	scores, err := json.Marshal(rep.FeatureScores)
	if err != nil {
		return err
	}
	err = r.pool.QueryRow(ctx, `
		INSERT INTO drift_reports (model_version_id, window_start, window_end, sample_size, overall_psi, drift_detected, feature_scores)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`,
		rep.ModelVersionID, rep.WindowStart, rep.WindowEnd, rep.SampleSize, rep.OverallPSI, rep.DriftDetected, scores,
	).Scan(&rep.ID, &rep.CreatedAt)
	if err != nil {
		return fmt.Errorf("save drift report: %w", err)
	}
	return nil
}

func (r *PostgresRepository) List(ctx context.Context, model string, limit int) ([]models.DriftReport, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT d.id, d.model_version_id, m.name, v.version, d.window_start, d.window_end,
		       d.sample_size, d.overall_psi, d.drift_detected, d.feature_scores, d.created_at
		FROM drift_reports d
		JOIN model_versions v ON v.id = d.model_version_id
		JOIN models m ON m.id = v.model_id
		WHERE ($1 = '' OR m.name = $1)
		ORDER BY d.created_at DESC
		LIMIT $2`, model, limit)
	if err != nil {
		return nil, fmt.Errorf("list drift reports: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.DriftReport, error) {
		var d models.DriftReport
		var scores []byte
		err := row.Scan(&d.ID, &d.ModelVersionID, &d.ModelName, &d.Version, &d.WindowStart, &d.WindowEnd,
			&d.SampleSize, &d.OverallPSI, &d.DriftDetected, &scores, &d.CreatedAt)
		if err != nil {
			return d, err
		}
		return d, json.Unmarshal(scores, &d.FeatureScores)
	})
}
