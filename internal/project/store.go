package project

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"forms/internal/audit"
	"forms/internal/id"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

type rowScanner interface{ Scan(...any) error }

const projectColumns = `id, title, slug, status, created_by, updated_by, created_at, updated_at, deleted_at, active_version_id`

func scanProject(row rowScanner) (Project, error) {
	var project Project
	err := row.Scan(&project.ID, &project.Title, &project.Slug, &project.Status, &project.CreatedBy, &project.UpdatedBy, &project.CreatedAt, &project.UpdatedAt, &project.DeletedAt, &project.ActiveVersionID)
	return project, err
}

func (s *Store) CreateWithDraft(ctx context.Context, actorID, title, slug string, schema []byte, checksum string) (Project, error) {
	projectID, err := id.New()
	if err != nil {
		return Project{}, fmt.Errorf("create project ID: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Project{}, fmt.Errorf("begin create project: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	project, err := scanProject(tx.QueryRow(ctx, `
        INSERT INTO projects(id, title, slug, created_by, updated_by)
        VALUES ($1, $2, $3, $4, $4)
        RETURNING `+projectColumns, projectID, title, slug, actorID))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Project{}, ErrDuplicateSlug
		}
		return Project{}, fmt.Errorf("insert project: %w", err)
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO form_drafts(project_id, schema, revision, checksum, updated_by)
        VALUES ($1, $2::jsonb, 1, $3, $4)`, projectID, string(schema), checksum, actorID); err != nil {
		return Project{}, fmt.Errorf("insert initial draft: %w", err)
	}
	if err := audit.Insert(ctx, tx, &actorID, "project.created", "project", projectID, map[string]any{"slug": slug}); err != nil {
		return Project{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, fmt.Errorf("commit create project: %w", err)
	}
	return project, nil
}

func (s *Store) List(ctx context.Context, trash bool) ([]Project, error) {
	predicate := `deleted_at IS NULL`
	if trash {
		predicate = `deleted_at IS NOT NULL`
	}
	rows, err := s.pool.Query(ctx, `SELECT `+projectColumns+` FROM projects WHERE `+predicate+` ORDER BY updated_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}
	defer rows.Close()
	projects := make([]Project, 0)
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list project rows: %w", err)
	}
	return projects, nil
}

func (s *Store) Get(ctx context.Context, projectID string, includeTrash bool) (Project, error) {
	predicate := `AND deleted_at IS NULL`
	if includeTrash {
		predicate = ``
	}
	project, err := scanProject(s.pool.QueryRow(ctx, `SELECT `+projectColumns+` FROM projects WHERE id = $1 `+predicate, projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("get project: %w", err)
	}
	return project, nil
}

func (s *Store) Update(ctx context.Context, actorID, projectID, title, slug string) (Project, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Project{}, fmt.Errorf("begin update project: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var currentSlug string
	var activeVersionID *string
	if err := tx.QueryRow(ctx, `SELECT slug, active_version_id FROM projects WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, projectID).Scan(&currentSlug, &activeVersionID); errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	} else if err != nil {
		return Project{}, fmt.Errorf("lock project for update: %w", err)
	}
	if activeVersionID != nil && slug != currentSlug {
		return Project{}, ErrSlugImmutable
	}
	project, err := scanProject(tx.QueryRow(ctx, `
        UPDATE projects
        SET title = $3, slug = $4, updated_by = $2, updated_at = now()
        WHERE id = $1 AND deleted_at IS NULL
        RETURNING `+projectColumns, projectID, actorID, title, slug))
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Project{}, ErrDuplicateSlug
		}
		return Project{}, fmt.Errorf("update project: %w", err)
	}
	if err := audit.Insert(ctx, tx, &actorID, "project.updated", "project", projectID, map[string]any{"slug": slug}); err != nil {
		return Project{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, fmt.Errorf("commit update project: %w", err)
	}
	return project, nil
}

func (s *Store) SetTrash(ctx context.Context, actorID, projectID string, trash bool) (Project, error) {
	statusExpression := `'trash'`
	previousStatusExpression := `status`
	deletedExpression := `now()`
	action := "project.trashed"
	predicate := `deleted_at IS NULL`
	if !trash {
		statusExpression = `coalesce(status_before_trash, CASE WHEN active_version_id IS NULL THEN 'draft' ELSE 'published' END)`
		previousStatusExpression = `NULL`
		deletedExpression = `NULL`
		action = "project.restored"
		predicate = `deleted_at IS NOT NULL`
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Project{}, fmt.Errorf("begin project trash operation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	project, err := scanProject(tx.QueryRow(ctx, `
        UPDATE projects
	        SET status = `+statusExpression+`, status_before_trash = `+previousStatusExpression+`, deleted_at = `+deletedExpression+`, updated_by = $2, updated_at = now()
        WHERE id = $1 AND `+predicate+`
		        RETURNING `+projectColumns, projectID, actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("update project trash state: %w", err)
	}
	if err := audit.Insert(ctx, tx, &actorID, action, "project", projectID, nil); err != nil {
		return Project{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, fmt.Errorf("commit project trash operation: %w", err)
	}
	return project, nil
}

func (s *Store) SetClosed(ctx context.Context, actorID, projectID string, closed bool) (Project, error) {
	statusExpression := `'closed'`
	predicate := `status = 'published'`
	action := "project.closed"
	if !closed {
		statusExpression = `CASE WHEN active_version_id IS NULL THEN 'draft' ELSE 'published' END`
		predicate = `status = 'closed'`
		action = "project.reopened"
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Project{}, fmt.Errorf("begin project close operation: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	project, err := scanProject(tx.QueryRow(ctx, `
        UPDATE projects
        SET status = `+statusExpression+`, updated_by = $2, updated_at = now()
        WHERE id = $1 AND deleted_at IS NULL AND `+predicate+`
        RETURNING `+projectColumns, projectID, actorID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("update project close state: %w", err)
	}
	if err := audit.Insert(ctx, tx, &actorID, action, "project", projectID, nil); err != nil {
		return Project{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Project{}, fmt.Errorf("commit project close operation: %w", err)
	}
	return project, nil
}
