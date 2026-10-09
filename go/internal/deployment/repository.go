package deployment

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"driftline/go/internal/models"
)

// Change is one row update inside an atomic batch (see Repository.Apply).
type Change struct {
	ID      uuid.UUID
	Status  models.DeploymentStatus
	Traffic int
	Reason  string
	Role    models.DeploymentRole // "" leaves the role unchanged
}

type Repository interface {
	Create(ctx context.Context, d *models.Deployment) error
	Get(ctx context.Context, id uuid.UUID) (models.Deployment, error)
	// List returns a model's deployments, newest first ("" = all models).
	List(ctx context.Context, model string, limit int) ([]models.Deployment, error)
	Running(ctx context.Context, model string) ([]models.Deployment, error)
	// PreviousPrimary is the most recent non-running primary whose version
	// differs from excludeVersionID: the rollback target.
	PreviousPrimary(ctx context.Context, model string, excludeVersionID uuid.UUID) (models.Deployment, error)
	// Apply updates several deployments in one transaction, so traffic
	// shares never transiently stop summing to 100.
	Apply(ctx context.Context, changes []Change) error
}

type PostgresRepository struct{ pool *pgxpool.Pool }

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const selectCols = `
	SELECT d.id, d.model_version_id, m.name, v.version, d.environment, d.role, d.status,
	       d.status_reason, d.traffic_percent, d.endpoint, d.container_id,
	       d.started_at, d.stopped_at, d.created_at, d.updated_at
	FROM deployments d
	JOIN model_versions v ON v.id = d.model_version_id
	JOIN models m ON m.id = v.model_id`

func scan(row pgx.Row) (models.Deployment, error) {
	var d models.Deployment
	err := row.Scan(&d.ID, &d.ModelVersionID, &d.ModelName, &d.Version, &d.Environment, &d.Role, &d.Status,
		&d.StatusReason, &d.TrafficPercent, &d.Endpoint, &d.ContainerID,
		&d.StartedAt, &d.StoppedAt, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

func mapErr(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}
	return nil
}

func (r *PostgresRepository) Create(ctx context.Context, d *models.Deployment) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO deployments (model_version_id, environment, role, status, status_reason, traffic_percent, endpoint)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at`,
		d.ModelVersionID, d.Environment, d.Role, d.Status, d.StatusReason, d.TrafficPercent, d.Endpoint,
	).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
	return mapErr(err, "create deployment")
}

func (r *PostgresRepository) Get(ctx context.Context, id uuid.UUID) (models.Deployment, error) {
	d, err := scan(r.pool.QueryRow(ctx, selectCols+` WHERE d.id = $1`, id))
	return d, mapErr(err, "get deployment")
}

func (r *PostgresRepository) List(ctx context.Context, model string, limit int) ([]models.Deployment, error) {
	rows, err := r.pool.Query(ctx, selectCols+`
		WHERE ($1 = '' OR m.name = $1)
		ORDER BY (d.status = 'running') DESC, d.created_at DESC
		LIMIT $2`, model, limit)
	if err != nil {
		return nil, mapErr(err, "list deployments")
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Deployment, error) { return scan(row) })
	return out, mapErr(err, "list deployments")
}

func (r *PostgresRepository) Running(ctx context.Context, model string) ([]models.Deployment, error) {
	rows, err := r.pool.Query(ctx, selectCols+`
		WHERE m.name = $1 AND d.status = 'running'
		ORDER BY d.role DESC`, model) // 'primary' sorts before 'canary' descending
	if err != nil {
		return nil, mapErr(err, "running deployments")
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Deployment, error) { return scan(row) })
	return out, mapErr(err, "running deployments")
}

func (r *PostgresRepository) PreviousPrimary(ctx context.Context, model string, exclude uuid.UUID) (models.Deployment, error) {
	d, err := scan(r.pool.QueryRow(ctx, selectCols+`
		WHERE m.name = $1 AND d.role = 'primary' AND d.status IN ('stopped', 'rolled_back')
		  AND d.model_version_id <> $2 AND d.started_at IS NOT NULL
		ORDER BY d.stopped_at DESC NULLS LAST
		LIMIT 1`, model, exclude))
	return d, mapErr(err, "previous primary")
}

func (r *PostgresRepository) Apply(ctx context.Context, changes []Change) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return mapErr(err, "apply deployment changes")
	}
	defer tx.Rollback(ctx)

	for _, c := range changes {
		_, err := tx.Exec(ctx, `
			UPDATE deployments SET
				status = $2,
				traffic_percent = $3,
				status_reason = CASE WHEN $4 = '' THEN status_reason ELSE $4 END,
				role = CASE WHEN $5 = '' THEN role ELSE $5 END,
				started_at = CASE WHEN $2 = 'running' AND started_at IS NULL THEN now() ELSE started_at END,
				stopped_at = CASE WHEN $2 IN ('stopped', 'rolled_back', 'failed') THEN now() ELSE stopped_at END,
				updated_at = now()
			WHERE id = $1`, c.ID, c.Status, c.Traffic, c.Reason, string(c.Role))
		if err != nil {
			return mapErr(err, "apply deployment changes")
		}
	}
	return mapErr(tx.Commit(ctx), "apply deployment changes")
}
