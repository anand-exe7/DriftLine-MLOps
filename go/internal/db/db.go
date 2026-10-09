// Package db owns the Postgres connection pool and schema migrations.
package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// Arbitrary constant: every server instance takes this advisory lock before
// migrating, so two replicas starting at once can't apply the same file twice.
const migrationLockID = 727_001

// Connect opens a pool and verifies the database is reachable, retrying for a
// while because in docker-compose Postgres may still be booting.
func Connect(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("db: parse url: %w", err)
	}
	cfg.MaxConns = 20
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("db: create pool: %w", err)
	}

	const attempts = 10
	for i := 1; ; i++ {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return pool, nil
		}
		if i == attempts {
			pool.Close()
			return nil, fmt.Errorf("db: ping after %d attempts: %w", attempts, err)
		}
		slog.Warn("db: not ready, retrying", "attempt", i, "err", err)
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(time.Duration(i) * 500 * time.Millisecond):
		}
	}
}

// Migrate applies every embedded migrations/NNN_*.sql file that hasn't been
// applied yet, in filename order, each inside its own transaction.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("db: acquire: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("db: migration lock: %w", err)
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, migrationLockID)

	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("db: create schema_migrations: %w", err)
	}

	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)

	for _, file := range files {
		version := strings.TrimSuffix(strings.TrimPrefix(file, "migrations/"), ".sql")

		var applied bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version,
		).Scan(&applied); err != nil {
			return fmt.Errorf("db: check %s: %w", version, err)
		}
		if applied {
			continue
		}

		sql, err := migrationFS.ReadFile(file)
		if err != nil {
			return err
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("db: apply %s: %w", version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("db: record %s: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("db: commit %s: %w", version, err)
		}
		slog.Info("db: applied migration", "version", version)
	}
	return nil
}
