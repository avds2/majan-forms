package database

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	appmigrations "forms/migrations"
)

const migrationLockID int64 = 684_667_230_041

var ErrSchemaBehind = errors.New("database schema is behind the application")

type Migration struct {
	Version int64
	Name    string
	SQL     string
}

type MigrationStatus struct {
	Current int64 `json:"current"`
	Latest  int64 `json:"latest"`
	Pending int   `json:"pending"`
}

type Migrator struct{ pool *pgxpool.Pool }

func NewMigrator(pool *pgxpool.Pool) *Migrator { return &Migrator{pool: pool} }

func availableMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(appmigrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded migrations: %w", err)
	}
	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		prefix, _, found := strings.Cut(entry.Name(), "_")
		if !found {
			return nil, fmt.Errorf("invalid migration filename %q", entry.Name())
		}
		version, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse migration filename %q: %w", entry.Name(), err)
		}
		contents, err := fs.ReadFile(appmigrations.FS, entry.Name())
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", entry.Name(), err)
		}
		migrations = append(migrations, Migration{Version: version, Name: entry.Name(), SQL: string(contents)})
	}
	sort.Slice(migrations, func(i, j int) bool { return migrations[i].Version < migrations[j].Version })
	for i := 1; i < len(migrations); i++ {
		if migrations[i-1].Version == migrations[i].Version {
			return nil, fmt.Errorf("duplicate migration version %d", migrations[i].Version)
		}
	}
	return migrations, nil
}

func (m *Migrator) Status(ctx context.Context) (MigrationStatus, error) {
	migrations, err := availableMigrations()
	if err != nil {
		return MigrationStatus{}, err
	}
	if err := ensureMigrationTable(ctx, m.pool); err != nil {
		return MigrationStatus{}, err
	}
	var current int64
	if err := m.pool.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return MigrationStatus{}, fmt.Errorf("read migration status: %w", err)
	}
	latest := int64(0)
	for _, migration := range migrations {
		if migration.Version > latest {
			latest = migration.Version
		}
	}
	pending := 0
	for _, migration := range migrations {
		if migration.Version > current {
			pending++
		}
	}
	return MigrationStatus{Current: current, Latest: latest, Pending: pending}, nil
}

func (m *Migrator) CheckCurrent(ctx context.Context) error {
	status, err := m.Status(ctx)
	if err != nil {
		return err
	}
	if status.Current != status.Latest {
		return fmt.Errorf("%w: current=%d latest=%d", ErrSchemaBehind, status.Current, status.Latest)
	}
	return nil
}

func (m *Migrator) Up(ctx context.Context) (int, error) {
	migrations, err := availableMigrations()
	if err != nil {
		return 0, err
	}
	connection, err := m.pool.Acquire(ctx)
	if err != nil {
		return 0, fmt.Errorf("acquire migration connection: %w", err)
	}
	defer connection.Release()
	if _, err := connection.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return 0, fmt.Errorf("lock migrations: %w", err)
	}
	defer connection.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, migrationLockID) //nolint:errcheck
	if err := ensureMigrationTable(ctx, connection); err != nil {
		return 0, err
	}

	applied := 0
	for _, migration := range migrations {
		var exists bool
		if err := connection.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, migration.Version).Scan(&exists); err != nil {
			return applied, fmt.Errorf("check migration %d: %w", migration.Version, err)
		}
		if exists {
			continue
		}
		tx, err := connection.Begin(ctx)
		if err != nil {
			return applied, fmt.Errorf("begin migration %d: %w", migration.Version, err)
		}
		if _, err := tx.Exec(ctx, migration.SQL); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			return applied, fmt.Errorf("apply migration %s: %w", migration.Name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version, name) VALUES ($1, $2)`, migration.Version, migration.Name); err != nil {
			tx.Rollback(ctx) //nolint:errcheck
			return applied, fmt.Errorf("record migration %s: %w", migration.Name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return applied, fmt.Errorf("commit migration %s: %w", migration.Name, err)
		}
		applied++
	}
	return applied, nil
}

func ensureMigrationTable(ctx context.Context, db interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}) error {
	_, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
        version bigint PRIMARY KEY,
        name text NOT NULL,
        applied_at timestamptz NOT NULL DEFAULT now()
    )`)
	if err != nil {
		return fmt.Errorf("ensure migration table: %w", err)
	}
	return nil
}
