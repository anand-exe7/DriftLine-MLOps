package alert

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"driftline/go/internal/models"
)

type Repository interface {
	Insert(ctx context.Context, a *models.Alert) error
	ListRecent(ctx context.Context, limit int) ([]models.Alert, error)
}

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

func (r *PostgresRepository) Insert(ctx context.Context, a *models.Alert) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO alerts (model_version_id, drift_report_id, severity, channel, message, status, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at`,
		a.ModelVersionID, a.DriftReportID, a.Severity, a.Channel, a.Message, a.Status, a.Error,
	).Scan(&a.ID, &a.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert alert: %w", err)
	}
	return nil
}

func (r *PostgresRepository) ListRecent(ctx context.Context, limit int) ([]models.Alert, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, model_version_id, drift_report_id, severity, channel, message, status, error, created_at
		FROM alerts ORDER BY created_at DESC LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("list alerts: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Alert, error) {
		var a models.Alert
		err := row.Scan(&a.ID, &a.ModelVersionID, &a.DriftReportID, &a.Severity, &a.Channel,
			&a.Message, &a.Status, &a.Error, &a.CreatedAt)
		return a, err
	})
}
