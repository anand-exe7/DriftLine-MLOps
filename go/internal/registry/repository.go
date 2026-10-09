package registry

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"driftline/go/internal/models"
)

// Repository is the persistence boundary for the registry. The service only
// sees this interface, so tests can swap in an in-memory fake.
type Repository interface {
	CreateModel(ctx context.Context, m *models.Model) error
	GetModelByName(ctx context.Context, name string) (models.Model, error)
	ListModels(ctx context.Context) ([]models.Model, error)

	CreateVersion(ctx context.Context, v *models.ModelVersion) error
	GetVersion(ctx context.Context, modelName, version string) (models.ModelVersion, error)
	ListVersions(ctx context.Context, modelName string) ([]models.ModelVersion, error)
	// SetStage moves a version to stage. Promoting to production archives the
	// model's current production version in the same transaction.
	SetStage(ctx context.Context, modelName, version string, stage models.Stage) (models.ModelVersion, error)
}

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

const pgUniqueViolation = "23505"

// mapErr converts driver errors into the registry's sentinel errors so the
// service and handler never import pgx.
func mapErr(err error, what string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", what, ErrNotFound)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == pgUniqueViolation {
		return fmt.Errorf("%s: %w", what, ErrConflict)
	}
	return fmt.Errorf("%s: %w", what, err)
}

func (r *PostgresRepository) CreateModel(ctx context.Context, m *models.Model) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO models (name, description, task_type)
		VALUES ($1, $2, $3)
		RETURNING id, created_at, updated_at`,
		m.Name, m.Description, m.TaskType,
	).Scan(&m.ID, &m.CreatedAt, &m.UpdatedAt)
	return mapErr(err, "create model "+m.Name)
}

const modelCols = `id, name, description, task_type, created_at, updated_at`

func scanModel(row pgx.Row) (models.Model, error) {
	var m models.Model
	err := row.Scan(&m.ID, &m.Name, &m.Description, &m.TaskType, &m.CreatedAt, &m.UpdatedAt)
	return m, err
}

func (r *PostgresRepository) GetModelByName(ctx context.Context, name string) (models.Model, error) {
	m, err := scanModel(r.pool.QueryRow(ctx, `SELECT `+modelCols+` FROM models WHERE name = $1`, name))
	return m, mapErr(err, "get model "+name)
}

func (r *PostgresRepository) ListModels(ctx context.Context) ([]models.Model, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+modelCols+` FROM models ORDER BY name`)
	if err != nil {
		return nil, mapErr(err, "list models")
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.Model, error) { return scanModel(row) })
	return out, mapErr(err, "list models")
}

func (r *PostgresRepository) CreateVersion(ctx context.Context, v *models.ModelVersion) error {
	err := r.pool.QueryRow(ctx, `
		INSERT INTO model_versions
			(model_id, version, framework, artifact_key, artifact_size, checksum_sha256,
			 feature_schema, metrics, baseline_stats, stage)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, created_at`,
		v.ModelID, v.Version, v.Framework, v.ArtifactKey, v.ArtifactSize, v.ChecksumSHA256,
		v.FeatureSchema, v.Metrics, nullableJSON(v.BaselineStats), v.Stage,
	).Scan(&v.ID, &v.CreatedAt)
	return mapErr(err, "create version "+v.Version)
}

// versionSelect joins models so every returned version carries its model name.
const versionSelect = `
	SELECT v.id, v.model_id, m.name, v.version, v.framework, v.artifact_key, v.artifact_size,
	       v.checksum_sha256, v.feature_schema, v.metrics, v.baseline_stats, v.stage,
	       v.created_at, v.promoted_at
	FROM model_versions v JOIN models m ON m.id = v.model_id`

func scanVersion(row pgx.Row) (models.ModelVersion, error) {
	var v models.ModelVersion
	var baseline []byte
	err := row.Scan(&v.ID, &v.ModelID, &v.ModelName, &v.Version, &v.Framework, &v.ArtifactKey,
		&v.ArtifactSize, &v.ChecksumSHA256, &v.FeatureSchema, &v.Metrics, &baseline, &v.Stage,
		&v.CreatedAt, &v.PromotedAt)
	if baseline != nil {
		v.BaselineStats = baseline
	}
	return v, err
}

func (r *PostgresRepository) GetVersion(ctx context.Context, modelName, version string) (models.ModelVersion, error) {
	v, err := scanVersion(r.pool.QueryRow(ctx,
		versionSelect+` WHERE m.name = $1 AND v.version = $2`, modelName, version))
	return v, mapErr(err, fmt.Sprintf("get version %s/%s", modelName, version))
}

func (r *PostgresRepository) ListVersions(ctx context.Context, modelName string) ([]models.ModelVersion, error) {
	rows, err := r.pool.Query(ctx, versionSelect+` WHERE m.name = $1 ORDER BY v.created_at DESC`, modelName)
	if err != nil {
		return nil, mapErr(err, "list versions "+modelName)
	}
	out, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (models.ModelVersion, error) { return scanVersion(row) })
	return out, mapErr(err, "list versions "+modelName)
}

func (r *PostgresRepository) SetStage(ctx context.Context, modelName, version string, stage models.Stage) (models.ModelVersion, error) {
	what := fmt.Sprintf("set stage %s/%s -> %s", modelName, version, stage)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return models.ModelVersion{}, mapErr(err, what)
	}
	defer tx.Rollback(ctx) // no-op after Commit

	// Lock the target row so two concurrent promotions serialise here instead
	// of racing into the one-production-per-model unique index.
	var id, modelID string
	if err := tx.QueryRow(ctx, `
		SELECT v.id, v.model_id FROM model_versions v JOIN models m ON m.id = v.model_id
		WHERE m.name = $1 AND v.version = $2
		FOR UPDATE OF v`, modelName, version).Scan(&id, &modelID); err != nil {
		return models.ModelVersion{}, mapErr(err, what)
	}

	if stage == models.StageProduction {
		if _, err := tx.Exec(ctx, `
			UPDATE model_versions SET stage = 'archived'
			WHERE model_id = $1 AND stage = 'production' AND id <> $2`, modelID, id); err != nil {
			return models.ModelVersion{}, mapErr(err, what)
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE model_versions SET stage = $2, promoted_at = now() WHERE id = $1`, id, stage); err != nil {
		return models.ModelVersion{}, mapErr(err, what)
	}

	v, err := scanVersion(tx.QueryRow(ctx, versionSelect+` WHERE v.id = $1`, id))
	if err != nil {
		return models.ModelVersion{}, mapErr(err, what)
	}
	return v, mapErr(tx.Commit(ctx), what)
}

func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
