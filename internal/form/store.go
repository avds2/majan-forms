package form

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"forms/internal/audit"
	"forms/internal/id"
)

var (
	ErrDraftNotFound         = errors.New("پیش‌نویس پیدا نشد")
	ErrVersionNotFound       = errors.New("نسخهٔ منتشرشده پیدا نشد")
	ErrProjectNotPublishable = errors.New("پروژه برای انتشار در دسترس نیست")
)

type Draft struct {
	ProjectID string    `json:"project_id"`
	Schema    Schema    `json:"schema"`
	Revision  int64     `json:"revision"`
	Checksum  string    `json:"checksum"`
	UpdatedBy string    `json:"updated_by"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ConflictError struct {
	CurrentRevision int64     `json:"current_revision"`
	UpdatedBy       string    `json:"updated_by"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Version struct {
	ID            string    `json:"id"`
	ProjectID     string    `json:"project_id"`
	VersionNumber int64     `json:"version_number"`
	Schema        Schema    `json:"schema"`
	Checksum      string    `json:"checksum"`
	PublishedBy   string    `json:"published_by"`
	PublishedAt   time.Time `json:"published_at"`
}

func (e *ConflictError) Error() string {
	return "پیش‌نویس در جای دیگری تغییر کرده است"
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func scanVersion(row interface{ Scan(...any) error }) (Version, error) {
	var version Version
	var encoded []byte
	err := row.Scan(&version.ID, &version.ProjectID, &version.VersionNumber, &encoded, &version.Checksum, &version.PublishedBy, &version.PublishedAt)
	if err != nil {
		return Version{}, err
	}
	if err := json.Unmarshal(encoded, &version.Schema); err != nil {
		return Version{}, fmt.Errorf("decode published schema: %w", err)
	}
	return version, nil
}

const versionColumns = `id, project_id, version_number, schema, checksum, published_by, published_at`

func (s *Store) Get(ctx context.Context, projectID string) (Draft, error) {
	var draft Draft
	var encoded []byte
	err := s.pool.QueryRow(ctx, `
        SELECT d.project_id, d.schema, d.revision, d.checksum, d.updated_by, d.updated_at
        FROM form_drafts d
        JOIN projects p ON p.id = d.project_id
        WHERE d.project_id = $1 AND p.deleted_at IS NULL`, projectID,
	).Scan(&draft.ProjectID, &encoded, &draft.Revision, &draft.Checksum, &draft.UpdatedBy, &draft.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Draft{}, ErrDraftNotFound
	}
	if err != nil {
		return Draft{}, fmt.Errorf("get draft: %w", err)
	}
	if err := json.Unmarshal(encoded, &draft.Schema); err != nil {
		return Draft{}, fmt.Errorf("decode stored draft: %w", err)
	}
	return draft, nil
}

func (s *Store) Save(ctx context.Context, projectID, actorID string, expectedRevision int64, schema Schema) (Draft, error) {
	encoded, checksum, err := EncodeAndChecksum(schema)
	if err != nil {
		return Draft{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Draft{}, fmt.Errorf("begin save draft: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var draft Draft
	var stored []byte
	err = tx.QueryRow(ctx, `
        UPDATE form_drafts d
        SET schema = $4::jsonb,
            revision = d.revision + 1,
            checksum = $5,
            updated_by = $2,
            updated_at = now()
        FROM projects p
        WHERE d.project_id = $1
          AND p.id = d.project_id
          AND p.deleted_at IS NULL
          AND d.revision = $3
        RETURNING d.project_id, d.schema, d.revision, d.checksum, d.updated_by, d.updated_at`,
		projectID, actorID, expectedRevision, string(encoded), checksum,
	).Scan(&draft.ProjectID, &stored, &draft.Revision, &draft.Checksum, &draft.UpdatedBy, &draft.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		if rollbackErr := tx.Rollback(ctx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			return Draft{}, fmt.Errorf("rollback conflicted draft save: %w", rollbackErr)
		}
		var conflict ConflictError
		lookupErr := s.pool.QueryRow(ctx, `
            SELECT d.revision, d.updated_by, d.updated_at
            FROM form_drafts d
            JOIN projects p ON p.id = d.project_id
            WHERE d.project_id = $1 AND p.deleted_at IS NULL`, projectID,
		).Scan(&conflict.CurrentRevision, &conflict.UpdatedBy, &conflict.UpdatedAt)
		if errors.Is(lookupErr, pgx.ErrNoRows) {
			return Draft{}, ErrDraftNotFound
		}
		if lookupErr != nil {
			return Draft{}, fmt.Errorf("inspect draft conflict: %w", lookupErr)
		}
		return Draft{}, &conflict
	}
	if err != nil {
		return Draft{}, fmt.Errorf("save draft: %w", err)
	}
	if err := json.Unmarshal(stored, &draft.Schema); err != nil {
		return Draft{}, fmt.Errorf("decode saved draft: %w", err)
	}
	if err := audit.Insert(ctx, tx, &actorID, "form.draft_saved", "project", projectID, map[string]any{"revision": draft.Revision, "checksum": draft.Checksum}); err != nil {
		return Draft{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Draft{}, fmt.Errorf("commit save draft: %w", err)
	}
	return draft, nil
}

func (s *Store) Publish(ctx context.Context, projectID, actorID string, expectedRevision int64, idempotencyKey string) (Version, error) {
	keyHash := sha256.Sum256([]byte(idempotencyKey))
	versionID, err := id.New()
	if err != nil {
		return Version{}, fmt.Errorf("create form version ID: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Version{}, fmt.Errorf("begin publish form: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var status string
	var deletedAt *time.Time
	if err := tx.QueryRow(ctx, `SELECT status, deleted_at FROM projects WHERE id = $1 FOR UPDATE`, projectID).Scan(&status, &deletedAt); errors.Is(err, pgx.ErrNoRows) {
		return Version{}, ErrProjectNotPublishable
	} else if err != nil {
		return Version{}, fmt.Errorf("lock project for publish: %w", err)
	}
	if deletedAt != nil || status == "trash" {
		return Version{}, ErrProjectNotPublishable
	}
	if existing, err := scanVersion(tx.QueryRow(ctx, `SELECT `+versionColumns+` FROM form_versions WHERE project_id = $1 AND publish_key_hash = $2`, projectID, keyHash[:])); err == nil {
		return existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Version{}, fmt.Errorf("find idempotent publication: %w", err)
	}
	var draft Draft
	var encoded []byte
	if err := tx.QueryRow(ctx, `
        SELECT project_id, schema, revision, checksum, updated_by, updated_at
        FROM form_drafts WHERE project_id = $1 FOR UPDATE`, projectID,
	).Scan(&draft.ProjectID, &encoded, &draft.Revision, &draft.Checksum, &draft.UpdatedBy, &draft.UpdatedAt); errors.Is(err, pgx.ErrNoRows) {
		return Version{}, ErrDraftNotFound
	} else if err != nil {
		return Version{}, fmt.Errorf("lock draft for publish: %w", err)
	}
	if draft.Revision != expectedRevision {
		return Version{}, &ConflictError{CurrentRevision: draft.Revision, UpdatedBy: draft.UpdatedBy, UpdatedAt: draft.UpdatedAt}
	}
	if err := json.Unmarshal(encoded, &draft.Schema); err != nil {
		return Version{}, fmt.Errorf("decode draft for publish: %w", err)
	}
	canonical, checksum, err := EncodeAndChecksum(draft.Schema)
	if err != nil {
		return Version{}, err
	}
	if err := draft.Schema.ValidateForPublish(); err != nil {
		return Version{}, err
	}
	if checksum != draft.Checksum {
		return Version{}, fmt.Errorf("draft checksum mismatch")
	}
	var nextVersion int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(MAX(version_number), 0) + 1 FROM form_versions WHERE project_id = $1`, projectID).Scan(&nextVersion); err != nil {
		return Version{}, fmt.Errorf("allocate form version: %w", err)
	}
	version, err := scanVersion(tx.QueryRow(ctx, `
        INSERT INTO form_versions(id, project_id, version_number, schema, checksum, publish_key_hash, published_by)
        VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7)
        RETURNING `+versionColumns,
		versionID, projectID, nextVersion, string(canonical), checksum, keyHash[:], actorID,
	))
	if err != nil {
		return Version{}, fmt.Errorf("insert form version: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE projects SET active_version_id = $2, status = CASE WHEN status = 'closed' THEN 'closed' ELSE 'published' END, updated_by = $3, updated_at = now() WHERE id = $1`, projectID, version.ID, actorID); err != nil {
		return Version{}, fmt.Errorf("activate form version: %w", err)
	}
	if err := audit.Insert(ctx, tx, &actorID, "form.published", "project", projectID, map[string]any{"version": version.VersionNumber, "checksum": version.Checksum}); err != nil {
		return Version{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Version{}, fmt.Errorf("commit publish form: %w", err)
	}
	return version, nil
}

func (s *Store) ListVersions(ctx context.Context, projectID string) ([]Version, error) {
	rows, err := s.pool.Query(ctx, `SELECT v.id, v.project_id, v.version_number, v.schema, v.checksum, v.published_by, v.published_at FROM form_versions v JOIN projects p ON p.id = v.project_id AND p.deleted_at IS NULL WHERE v.project_id = $1 ORDER BY v.version_number DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list form versions: %w", err)
	}
	defer rows.Close()
	versions := make([]Version, 0)
	for rows.Next() {
		version, err := scanVersion(rows)
		if err != nil {
			return nil, fmt.Errorf("scan form version: %w", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list form versions rows: %w", err)
	}
	return versions, nil
}
