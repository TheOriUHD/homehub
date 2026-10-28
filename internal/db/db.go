// Package db owns the PostgreSQL connection pool and schema migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Connect opens a pool and waits for the database to become reachable. The
// database container may still be starting when the app boots, so this retries
// for up to a minute before giving up.
func Connect(ctx context.Context, url string, logger *slog.Logger) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	poolCfg.MaxConns = 16
	poolCfg.MinConns = 2
	poolCfg.MaxConnLifetime = time.Hour

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	deadline := time.Now().Add(60 * time.Second)
	for {
		pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		err = pool.Ping(pingCtx)
		cancel()
		if err == nil {
			return pool, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			pool.Close()
			return nil, fmt.Errorf("database unreachable after 60s: %w", err)
		}
		logger.Info("waiting for database", "error", err)
		select {
		case <-ctx.Done():
			pool.Close()
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// Migrate applies pending schema migrations under a session-level advisory
// lock, so concurrent app starts cannot race each other.
func Migrate(ctx context.Context, url string, logger *slog.Logger) error {
	sqlDB, err := sql.Open("pgx", url)
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer sqlDB.Close()

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("create migration locker: %w", err)
	}
	migrations, err := fs.Sub(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("open embedded migrations: %w", err)
	}
	provider, err := goose.NewProvider(database.DialectPostgres, sqlDB, migrations, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}

	results, err := provider.Up(ctx)
	if err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	for _, r := range results {
		logger.Info("applied migration", "migration", r.Source.Path, "duration", r.Duration)
	}
	return nil
}

// SupportedVersion is the newest schema migration this binary carries. A
// database (or a backup of one) at a higher version was written by a newer
// HomeHub, and this binary must not run against it.
func SupportedVersion() (int64, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return 0, fmt.Errorf("read embedded migrations: %w", err)
	}
	var latest int64
	for _, entry := range entries {
		if path.Ext(entry.Name()) != ".sql" {
			continue
		}
		v, err := goose.NumericComponent(entry.Name())
		if err != nil {
			return 0, fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		latest = max(latest, v)
	}
	return latest, nil
}

// AppliedVersion is the newest migration recorded in the database.
func AppliedVersion(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	var v int64
	err := pool.QueryRow(ctx,
		`select coalesce(max(version_id), 0) from `+goose.DefaultTablename+` where is_applied`).Scan(&v)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return v, nil
}
