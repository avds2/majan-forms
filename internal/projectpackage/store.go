package projectpackage

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"forms/internal/audit"
	"forms/internal/form"
	"forms/internal/id"
	"forms/internal/project"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) Load(ctx context.Context, projectID string) (sourceData, error) {
	var source sourceData
	err := s.pool.QueryRow(ctx, `
        SELECT id, title, slug, status, created_by, updated_by, created_at, updated_at, deleted_at, active_version_id
        FROM projects
        WHERE id = $1 AND deleted_at IS NULL`, projectID,
	).Scan(
		&source.Project.ID, &source.Project.Title, &source.Project.Slug, &source.Project.Status,
		&source.Project.CreatedBy, &source.Project.UpdatedBy, &source.Project.CreatedAt,
		&source.Project.UpdatedAt, &source.Project.DeletedAt, &source.Project.ActiveVersionID,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return sourceData{}, project.ErrNotFound
	}
	if err != nil {
		return sourceData{}, fmt.Errorf("load package project: %w", err)
	}
	var draftSchema []byte
	if err := s.pool.QueryRow(ctx, `
        SELECT schema, revision, checksum
        FROM form_drafts
        WHERE project_id = $1`, projectID,
	).Scan(&draftSchema, &source.Draft.Revision, &source.Draft.Checksum); err != nil {
		return sourceData{}, fmt.Errorf("load package draft: %w", err)
	}
	if err := json.Unmarshal(draftSchema, &source.Draft.Schema); err != nil {
		return sourceData{}, fmt.Errorf("decode package draft: %w", err)
	}
	rows, err := s.pool.Query(ctx, `
        SELECT id, version_number, schema, checksum, published_at
        FROM form_versions
        WHERE project_id = $1
        ORDER BY version_number`, projectID)
	if err != nil {
		return sourceData{}, fmt.Errorf("load package versions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var version VersionDocument
		var encoded []byte
		if err := rows.Scan(&version.sourceID, &version.VersionNumber, &encoded, &version.Checksum, &version.PublishedAt); err != nil {
			return sourceData{}, fmt.Errorf("scan package version: %w", err)
		}
		if err := json.Unmarshal(encoded, &version.Schema); err != nil {
			return sourceData{}, fmt.Errorf("decode package version: %w", err)
		}
		source.Versions = append(source.Versions, version)
	}
	if err := rows.Err(); err != nil {
		return sourceData{}, fmt.Errorf("iterate package versions: %w", err)
	}
	return source, nil
}

func (s *Store) Import(ctx context.Context, actorID string, input project.CreateInput, decoded decodedPackage) (project.Project, error) {
	projectID, err := id.New()
	if err != nil {
		return project.Project{}, fmt.Errorf("create imported project ID: %w", err)
	}
	versionIDs := make([]string, len(decoded.Versions))
	publishKeys := make([][]byte, len(decoded.Versions))
	for index := range decoded.Versions {
		versionIDs[index], err = id.New()
		if err != nil {
			return project.Project{}, fmt.Errorf("create imported version ID: %w", err)
		}
		publishKeys[index] = make([]byte, 32)
		if _, err := rand.Read(publishKeys[index]); err != nil {
			return project.Project{}, fmt.Errorf("create imported version key: %w", err)
		}
	}
	draftBytes, draftChecksum, err := form.EncodeAndChecksum(decoded.Draft.Schema)
	if err != nil {
		return project.Project{}, err
	}
	if draftChecksum != decoded.Draft.Checksum {
		return project.Project{}, ErrChecksumMismatch
	}
	versionBytes := make([][]byte, len(decoded.Versions))
	for index, item := range decoded.Versions {
		versionBytes[index], _, err = form.EncodeAndChecksum(item.Schema)
		if err != nil {
			return project.Project{}, err
		}
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return project.Project{}, fmt.Errorf("begin project package import: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var imported project.Project
	err = tx.QueryRow(ctx, `
        INSERT INTO projects(id, title, slug, status, created_by, updated_by)
        VALUES ($1, $2, $3, 'draft', $4, $4)
        RETURNING id, title, slug, status, created_by, updated_by, created_at, updated_at, deleted_at, active_version_id`,
		projectID, input.Title, input.Slug, actorID,
	).Scan(
		&imported.ID, &imported.Title, &imported.Slug, &imported.Status,
		&imported.CreatedBy, &imported.UpdatedBy, &imported.CreatedAt, &imported.UpdatedAt,
		&imported.DeletedAt, &imported.ActiveVersionID,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return project.Project{}, project.ErrDuplicateSlug
		}
		return project.Project{}, fmt.Errorf("insert imported project: %w", err)
	}
	if _, err := tx.Exec(ctx, `
        INSERT INTO form_drafts(project_id, schema, revision, checksum, updated_by)
        VALUES ($1, $2::jsonb, $3, $4, $5)`,
		projectID, string(draftBytes), decoded.Draft.Revision, decoded.Draft.Checksum, actorID,
	); err != nil {
		return project.Project{}, fmt.Errorf("insert imported draft: %w", err)
	}
	for index, item := range decoded.Versions {
		if _, err := tx.Exec(ctx, `
            INSERT INTO form_versions(id, project_id, version_number, schema, checksum, publish_key_hash, published_by, published_at)
            VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7, $8)`,
			versionIDs[index], projectID, item.VersionNumber, string(versionBytes[index]), item.Checksum,
			publishKeys[index], actorID, item.PublishedAt,
		); err != nil {
			return project.Project{}, fmt.Errorf("insert imported version %d: %w", item.VersionNumber, err)
		}
	}
	if err := audit.Insert(ctx, tx, &actorID, "project.package_imported", "project", projectID, map[string]any{
		"format": Format, "package_sha256": decoded.SHA256, "source_project_id": decoded.Manifest.SourceProjectID,
		"versions": len(decoded.Versions), "responses": 0, "assets": decoded.Manifest.Counts.Assets,
	}); err != nil {
		return project.Project{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return project.Project{}, fmt.Errorf("commit project package import: %w", err)
	}
	return imported, nil
}
