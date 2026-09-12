package publicform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"forms/internal/form"
	"forms/internal/id"
	"forms/internal/normalize"
)

var (
	ErrUnavailable      = errors.New("این فرم در حال حاضر پاسخ جدید نمی‌پذیرد")
	ErrInvalidSession   = errors.New("نشست فرم معتبر نیست یا منقضی شده است")
	ErrAlreadySubmitted = errors.New("این پاسخ قبلاً ثبت شده است")
	ErrTooManyFiles     = errors.New("تعداد فایل‌ها از محدودیت این فیلد بیشتر است")
)

const publicSessionLifetime = 24 * time.Hour

type PublishedProject struct {
	ProjectID string
	Title     string
	Slug      string
	Status    string
	Version   form.Version
}

type Session struct {
	ID            string
	ProjectID     string
	VersionID     string
	Revision      int64
	Answers       map[string]any
	History       []string
	CurrentPageID string
	StartedAt     time.Time
	LastSeenAt    time.Time
	ExpiresAt     time.Time
	SubmittedAt   *time.Time
	Project       PublishedProject
}

type Submission struct {
	ID          string
	PublicID    string
	ProjectID   string
	VersionID   string
	EndingID    string
	SubmittedAt time.Time
	Theme       string `json:"-"`
	Alignment   string `json:"-"`
}

type StoredFile struct {
	ID              string
	PublicSessionID *string
	SubmissionID    *string
	FieldID         string
	StorageKey      string
	OriginalName    string
	SizeBytes       int64
	DetectedMIME    string
	Extension       string
	Checksum        string
	Status          string
	CreatedAt       time.Time
}

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) FindPublishedProject(ctx context.Context, slug string) (PublishedProject, error) {
	var project PublishedProject
	var encoded []byte
	err := s.pool.QueryRow(ctx, `
        SELECT p.id, p.title, p.slug, p.status,
               v.id, v.project_id, v.version_number, v.schema, v.checksum, v.published_by, v.published_at
        FROM projects p
        JOIN form_versions v ON v.id = p.active_version_id
        WHERE p.slug = $1 AND p.deleted_at IS NULL`, slug,
	).Scan(
		&project.ProjectID, &project.Title, &project.Slug, &project.Status,
		&project.Version.ID, &project.Version.ProjectID, &project.Version.VersionNumber, &encoded,
		&project.Version.Checksum, &project.Version.PublishedBy, &project.Version.PublishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublishedProject{}, ErrUnavailable
	}
	if err != nil {
		return PublishedProject{}, fmt.Errorf("find published form: %w", err)
	}
	if err := json.Unmarshal(encoded, &project.Version.Schema); err != nil {
		return PublishedProject{}, fmt.Errorf("decode published form: %w", err)
	}
	return project, nil
}

func (s *Store) CreateSession(ctx context.Context, project PublishedProject, tokenHash []byte, now time.Time, eventDate string) (Session, error) {
	if project.Status != "published" {
		return Session{}, ErrUnavailable
	}
	sessionID, err := id.New()
	if err != nil {
		return Session{}, fmt.Errorf("create public session ID: %w", err)
	}
	answers := map[string]any{}
	history := []string{}
	expiresAt := now.Add(publicSessionLifetime)
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Session{}, fmt.Errorf("begin public session: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	_, err = tx.Exec(ctx, `
        INSERT INTO public_sessions(id, token_hash, project_id, version_id, answers, history, current_page_id, started_at, last_seen_at, expires_at)
        VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7, $8, $8, $9)`,
		sessionID, tokenHash, project.ProjectID, project.Version.ID, `{}`, `[]`, project.Version.Schema.Flow.StartPageID, now, expiresAt,
	)
	if err != nil {
		return Session{}, fmt.Errorf("create public session: %w", err)
	}
	if err := incrementAnalytics(ctx, tx, project.ProjectID, project.Version.ID, "", "starts", eventDate); err != nil {
		return Session{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Session{}, fmt.Errorf("commit public session: %w", err)
	}
	return Session{
		ID: sessionID, ProjectID: project.ProjectID, VersionID: project.Version.ID,
		Revision: 1,
		Answers:  answers, History: history, CurrentPageID: project.Version.Schema.Flow.StartPageID,
		StartedAt: now, LastSeenAt: now, ExpiresAt: expiresAt, Project: project,
	}, nil
}

func (s *Store) GetSession(ctx context.Context, slug string, tokenHash []byte, now time.Time) (Session, error) {
	return s.getSession(ctx, slug, tokenHash, now, false)
}

// GetSessionForSubmit also returns an already-submitted session so a retry with
// the same idempotency key can resolve to the original submission. All other
// public-session operations continue to reject submitted sessions.
func (s *Store) GetSessionForSubmit(ctx context.Context, slug string, tokenHash []byte, now time.Time) (Session, error) {
	return s.getSession(ctx, slug, tokenHash, now, true)
}

func (s *Store) getSession(ctx context.Context, slug string, tokenHash []byte, now time.Time, allowSubmitted bool) (Session, error) {
	var session Session
	var answers, history, schema []byte
	err := s.pool.QueryRow(ctx, `
	        SELECT s.id, s.project_id, s.version_id, s.revision, s.answers, s.history, s.current_page_id,
               s.started_at, s.last_seen_at, s.expires_at, s.submitted_at,
               p.id, p.title, p.slug, p.status,
               v.id, v.project_id, v.version_number, v.schema, v.checksum, v.published_by, v.published_at
        FROM public_sessions s
        JOIN projects p ON p.id = s.project_id
        JOIN form_versions v ON v.id = s.version_id
	        WHERE s.token_hash = $1 AND p.slug = $2 AND p.deleted_at IS NULL`, tokenHash, slug,
	).Scan(
		&session.ID, &session.ProjectID, &session.VersionID, &session.Revision, &answers, &history, &session.CurrentPageID,
		&session.StartedAt, &session.LastSeenAt, &session.ExpiresAt, &session.SubmittedAt,
		&session.Project.ProjectID, &session.Project.Title, &session.Project.Slug, &session.Project.Status,
		&session.Project.Version.ID, &session.Project.Version.ProjectID, &session.Project.Version.VersionNumber, &schema,
		&session.Project.Version.Checksum, &session.Project.Version.PublishedBy, &session.Project.Version.PublishedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, ErrInvalidSession
	}
	if err != nil {
		return Session{}, fmt.Errorf("load public session: %w", err)
	}
	if session.SubmittedAt != nil && !allowSubmitted {
		return Session{}, ErrAlreadySubmitted
	}
	if session.SubmittedAt == nil && !now.Before(session.ExpiresAt) {
		_, _ = s.pool.Exec(ctx, `DELETE FROM public_sessions WHERE id = $1 AND submitted_at IS NULL`, session.ID)
		return Session{}, ErrInvalidSession
	}
	if err := json.Unmarshal(answers, &session.Answers); err != nil {
		return Session{}, fmt.Errorf("decode public answers: %w", err)
	}
	if err := json.Unmarshal(history, &session.History); err != nil {
		return Session{}, fmt.Errorf("decode public history: %w", err)
	}
	if err := json.Unmarshal(schema, &session.Project.Version.Schema); err != nil {
		return Session{}, fmt.Errorf("decode public schema: %w", err)
	}
	if session.SubmittedAt == nil {
		newExpiry := now.Add(publicSessionLifetime)
		if _, err := s.pool.Exec(ctx, `UPDATE public_sessions SET last_seen_at = $2, expires_at = $3 WHERE id = $1`, session.ID, now, newExpiry); err != nil {
			return Session{}, fmt.Errorf("refresh public session: %w", err)
		}
		session.LastSeenAt, session.ExpiresAt = now, newExpiry
	}
	return session, nil
}

func (s *Store) UpdateSession(ctx context.Context, session *Session, completedPageID, eventDate string) error {
	answers, err := json.Marshal(session.Answers)
	if err != nil {
		return fmt.Errorf("encode public answers: %w", err)
	}
	history, err := json.Marshal(session.History)
	if err != nil {
		return fmt.Errorf("encode public history: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin public session update: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	command, err := tx.Exec(ctx, `
        UPDATE public_sessions
	        SET answers = $2::jsonb, history = $3::jsonb, current_page_id = $4,
	            last_seen_at = $5, expires_at = $6, revision = revision + 1
	        WHERE id = $1 AND submitted_at IS NULL AND revision = $7`,
		session.ID, string(answers), string(history), session.CurrentPageID, session.LastSeenAt, session.ExpiresAt, session.Revision,
	)
	if err != nil {
		return fmt.Errorf("update public session: %w", err)
	}
	if command.RowsAffected() != 1 {
		return ErrPathChanged
	}
	if completedPageID != "" {
		if err := incrementAnalytics(ctx, tx, session.ProjectID, session.VersionID, completedPageID, "steps", eventDate); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit public session update: %w", err)
	}
	session.Revision++
	return nil
}

func (s *Store) CreateSubmission(ctx context.Context, session Session, endingID string, idempotencyHash, fingerprintHash []byte, now time.Time, eventDate string) (Submission, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Submission{}, false, fmt.Errorf("begin public submission: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if existing, err := scanSubmission(tx.QueryRow(ctx, `
        SELECT id, public_id, project_id, version_id, ending_id, submitted_at
        FROM submissions WHERE project_id = $1 AND idempotency_key_hash = $2`, session.ProjectID, idempotencyHash,
	)); err == nil {
		return existing, true, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Submission{}, false, fmt.Errorf("find idempotent submission: %w", err)
	}
	var submittedAt *time.Time
	var expiresAt time.Time
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT submitted_at, expires_at, revision FROM public_sessions WHERE id = $1 FOR UPDATE`, session.ID).Scan(&submittedAt, &expiresAt, &revision); err != nil {
		return Submission{}, false, fmt.Errorf("lock public session for submit: %w", err)
	}
	if submittedAt != nil {
		if existing, err := scanSubmission(tx.QueryRow(ctx, `
            SELECT id, public_id, project_id, version_id, ending_id, submitted_at
            FROM submissions WHERE public_session_id = $1`, session.ID)); err == nil {
			return existing, true, nil
		}
		return Submission{}, false, ErrAlreadySubmitted
	}
	if !now.Before(expiresAt) {
		return Submission{}, false, ErrInvalidSession
	}
	if revision != session.Revision {
		return Submission{}, false, ErrPathChanged
	}
	submissionID, err := id.New()
	if err != nil {
		return Submission{}, false, fmt.Errorf("create submission ID: %w", err)
	}
	publicID, err := id.New()
	if err != nil {
		return Submission{}, false, fmt.Errorf("create public submission ID: %w", err)
	}
	searchText := submissionSearchText(publicID, session.Answers)
	submission, err := scanSubmission(tx.QueryRow(ctx, `
		INSERT INTO submissions(id, public_id, public_session_id, project_id, version_id, ending_id, idempotency_key_hash, started_at, submitted_at, fingerprint_hash, search_text)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, public_id, project_id, version_id, ending_id, submitted_at`,
		submissionID, publicID, session.ID, session.ProjectID, session.VersionID, endingID, idempotencyHash, session.StartedAt, now, nullableBytes(fingerprintHash), searchText,
	))
	if err != nil {
		return Submission{}, false, fmt.Errorf("insert submission: %w", err)
	}
	fields := fieldsByID(session.Project.Version.Schema)
	for fieldID, value := range session.Answers {
		field, ok := fields[fieldID]
		if !ok || isUnstoredField(field.Type) || value == nil {
			continue
		}
		if err := insertAnswer(ctx, tx, submission.ID, field, value); err != nil {
			return Submission{}, false, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE public_sessions SET submitted_at = $2 WHERE id = $1`, session.ID, now); err != nil {
		return Submission{}, false, fmt.Errorf("mark public session submitted: %w", err)
	}
	if _, err := tx.Exec(ctx, `
        UPDATE stored_files
        SET public_session_id = NULL, submission_id = $2
        WHERE public_session_id = $1 AND status = 'staging'`, session.ID, submission.ID); err != nil {
		return Submission{}, false, fmt.Errorf("attach staged files to submission: %w", err)
	}
	if err := incrementAnalytics(ctx, tx, session.ProjectID, session.VersionID, "", "completes", eventDate); err != nil {
		return Submission{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Submission{}, false, fmt.Errorf("commit public submission: %w", err)
	}
	return submission, false, nil
}

func (s *Store) CreateStagedFile(ctx context.Context, file StoredFile, maximumFiles int, expectedRevision int64, expectedPageID string) error {
	if file.PublicSessionID == nil {
		return ErrInvalidSession
	}
	sessionID := *file.PublicSessionID
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin staged file: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var currentPageID string
	var revision int64
	if err := tx.QueryRow(ctx, `SELECT current_page_id, revision FROM public_sessions WHERE id = $1 AND submitted_at IS NULL FOR UPDATE`, sessionID).Scan(&currentPageID, &revision); errors.Is(err, pgx.ErrNoRows) {
		return ErrInvalidSession
	} else if err != nil {
		return fmt.Errorf("lock public session for file: %w", err)
	}
	if revision != expectedRevision || currentPageID != expectedPageID {
		return ErrPathChanged
	}
	var count int
	if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM stored_files WHERE public_session_id = $1 AND field_id = $2 AND status = 'staging'`, sessionID, file.FieldID).Scan(&count); err != nil {
		return fmt.Errorf("count files before staging: %w", err)
	}
	if count >= maximumFiles {
		return ErrTooManyFiles
	}
	_, err = tx.Exec(ctx, `
        INSERT INTO stored_files(
            id, public_session_id, field_id, storage_key, original_name,
            size_bytes, detected_mime, extension, checksum, status
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'staging')`,
		file.ID, sessionID, file.FieldID, file.StorageKey, file.OriginalName,
		file.SizeBytes, file.DetectedMIME, file.Extension, file.Checksum,
	)
	if err != nil {
		return fmt.Errorf("record staged file: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit staged file: %w", err)
	}
	return nil
}

func (s *Store) CountStagedFiles(ctx context.Context, sessionID, fieldID string) (int, error) {
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM stored_files WHERE public_session_id = $1 AND field_id = $2 AND status = 'staging'`, sessionID, fieldID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count staged files: %w", err)
	}
	return count, nil
}

func (s *Store) ListStagedFiles(ctx context.Context, sessionID string) ([]StoredFile, error) {
	rows, err := s.pool.Query(ctx, `
        SELECT id, public_session_id, submission_id, field_id, storage_key, original_name,
               size_bytes, detected_mime, extension, checksum, status, created_at
        FROM stored_files WHERE public_session_id = $1 AND status = 'staging'
        ORDER BY created_at, id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list staged files: %w", err)
	}
	defer rows.Close()
	files := make([]StoredFile, 0)
	for rows.Next() {
		var file StoredFile
		if err := rows.Scan(
			&file.ID, &file.PublicSessionID, &file.SubmissionID, &file.FieldID, &file.StorageKey, &file.OriginalName,
			&file.SizeBytes, &file.DetectedMIME, &file.Extension, &file.Checksum, &file.Status, &file.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan staged file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list staged files rows: %w", err)
	}
	return files, nil
}

func (s *Store) DeleteStagedFile(ctx context.Context, sessionID, fileID string) (string, error) {
	var storageKey string
	err := s.pool.QueryRow(ctx, `
        DELETE FROM stored_files
        WHERE id = $1 AND public_session_id = $2 AND status = 'staging'
        RETURNING storage_key`, fileID, sessionID).Scan(&storageKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("delete staged file: %w", err)
	}
	return storageKey, nil
}

func (s *Store) GetFinalizedFile(ctx context.Context, fileID string) (StoredFile, error) {
	var file StoredFile
	err := s.pool.QueryRow(ctx, `
        SELECT f.id, f.public_session_id, f.submission_id, f.field_id, f.storage_key, f.original_name,
               f.size_bytes, f.detected_mime, f.extension, f.checksum, f.status, f.created_at
        FROM stored_files f
        JOIN submissions s ON s.id = f.submission_id
        JOIN projects p ON p.id = s.project_id
        WHERE f.id = $1 AND f.status = 'finalized' AND s.deleted_at IS NULL AND p.deleted_at IS NULL`, fileID).Scan(
		&file.ID, &file.PublicSessionID, &file.SubmissionID, &file.FieldID, &file.StorageKey, &file.OriginalName,
		&file.SizeBytes, &file.DetectedMIME, &file.Extension, &file.Checksum, &file.Status, &file.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return StoredFile{}, ErrInvalidUpload
	}
	if err != nil {
		return StoredFile{}, fmt.Errorf("get finalized file: %w", err)
	}
	return file, nil
}

func (s *Store) MarkFilesFinalized(ctx context.Context, submissionID string, now time.Time) error {
	_, err := s.pool.Exec(ctx, `
        UPDATE stored_files SET status = 'finalized', finalized_at = $2
        WHERE submission_id = $1 AND status = 'staging'`, submissionID, now)
	if err != nil {
		return fmt.Errorf("mark files finalized: %w", err)
	}
	return nil
}

func (s *Store) ListPendingSubmissionFiles(ctx context.Context, submissionID string) ([]StoredFile, error) {
	rows, err := s.pool.Query(ctx, `
        SELECT id, public_session_id, submission_id, field_id, storage_key, original_name,
               size_bytes, detected_mime, extension, checksum, status, created_at
        FROM stored_files WHERE submission_id = $1 AND status = 'staging'
        ORDER BY created_at, id`, submissionID)
	if err != nil {
		return nil, fmt.Errorf("list pending submission files: %w", err)
	}
	defer rows.Close()
	files := make([]StoredFile, 0)
	for rows.Next() {
		var file StoredFile
		if err := rows.Scan(
			&file.ID, &file.PublicSessionID, &file.SubmissionID, &file.FieldID, &file.StorageKey, &file.OriginalName,
			&file.SizeBytes, &file.DetectedMIME, &file.Extension, &file.Checksum, &file.Status, &file.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending submission file: %w", err)
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending submission files rows: %w", err)
	}
	return files, nil
}

func (s *Store) ListPendingSubmissionIDs(ctx context.Context) ([]string, error) {
	rows, err := s.pool.Query(ctx, `
        SELECT DISTINCT submission_id
        FROM stored_files
        WHERE submission_id IS NOT NULL AND status = 'staging'
        ORDER BY submission_id`)
	if err != nil {
		return nil, fmt.Errorf("list submissions with pending files: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var submissionID string
		if err := rows.Scan(&submissionID); err != nil {
			return nil, fmt.Errorf("scan submission with pending files: %w", err)
		}
		result = append(result, submissionID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list submissions with pending files rows: %w", err)
	}
	return result, nil
}

func (s *Store) DeleteExpiredSessions(ctx context.Context, now time.Time) ([]string, int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("begin expired public session cleanup: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	rows, err := tx.Query(ctx, `
        SELECT id FROM public_sessions
        WHERE submitted_at IS NULL AND expires_at <= $1
        ORDER BY expires_at
        FOR UPDATE SKIP LOCKED`, now)
	if err != nil {
		return nil, 0, fmt.Errorf("lock expired public sessions: %w", err)
	}
	sessionIDs := make([]string, 0)
	for rows.Next() {
		var sessionID string
		if err := rows.Scan(&sessionID); err != nil {
			rows.Close()
			return nil, 0, fmt.Errorf("scan expired public session: %w", err)
		}
		sessionIDs = append(sessionIDs, sessionID)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, 0, fmt.Errorf("expired public session rows: %w", err)
	}
	rows.Close()
	if len(sessionIDs) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return nil, 0, fmt.Errorf("commit empty public cleanup: %w", err)
		}
		return nil, 0, nil
	}
	fileRows, err := tx.Query(ctx, `SELECT storage_key FROM stored_files WHERE public_session_id = ANY($1::uuid[])`, sessionIDs)
	if err != nil {
		return nil, 0, fmt.Errorf("list files for expired public sessions: %w", err)
	}
	storageKeys := make([]string, 0)
	for fileRows.Next() {
		var key string
		if err := fileRows.Scan(&key); err != nil {
			fileRows.Close()
			return nil, 0, fmt.Errorf("scan expired staged file: %w", err)
		}
		storageKeys = append(storageKeys, key)
	}
	if err := fileRows.Err(); err != nil {
		fileRows.Close()
		return nil, 0, fmt.Errorf("expired staged file rows: %w", err)
	}
	fileRows.Close()
	command, err := tx.Exec(ctx, `DELETE FROM public_sessions WHERE id = ANY($1::uuid[]) AND submitted_at IS NULL`, sessionIDs)
	if err != nil {
		return nil, 0, fmt.Errorf("delete expired public sessions: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit expired public session cleanup: %w", err)
	}
	return storageKeys, command.RowsAffected(), nil
}

func (s *Store) StagingStorageKeys(ctx context.Context) (map[string]bool, error) {
	rows, err := s.pool.Query(ctx, `SELECT storage_key FROM stored_files WHERE status = 'staging'`)
	if err != nil {
		return nil, fmt.Errorf("list staging storage keys: %w", err)
	}
	defer rows.Close()
	keys := make(map[string]bool)
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan staging storage key: %w", err)
		}
		keys[key] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("staging storage key rows: %w", err)
	}
	return keys, nil
}

func scanSubmission(row interface{ Scan(...any) error }) (Submission, error) {
	var submission Submission
	err := row.Scan(&submission.ID, &submission.PublicID, &submission.ProjectID, &submission.VersionID, &submission.EndingID, &submission.SubmittedAt)
	return submission, err
}

type analyticsExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (s *Store) incrementAnalytics(ctx context.Context, projectID, versionID, pageID, column, eventDate string) error {
	return incrementAnalytics(ctx, s.pool, projectID, versionID, pageID, column, eventDate)
}

func incrementAnalytics(ctx context.Context, executor analyticsExecutor, projectID, versionID, pageID, column, eventDate string) error {
	if column != "views" && column != "starts" && column != "steps" && column != "completes" {
		return fmt.Errorf("unsupported analytics column %q", column)
	}
	if _, err := time.Parse(time.DateOnly, eventDate); err != nil {
		return fmt.Errorf("invalid analytics date: %w", err)
	}
	if pageID == "" {
		pageID = "00000000-0000-0000-0000-000000000000"
	}
	query := `
        INSERT INTO analytics_daily(project_id, version_id, event_date, page_id, ` + column + `)
	        VALUES ($1, $2, $4::date, $3, 1)
        ON CONFLICT (project_id, version_id, event_date, page_id)
        DO UPDATE SET ` + column + ` = analytics_daily.` + column + ` + 1`
	if _, err := executor.Exec(ctx, query, projectID, versionID, pageID, eventDate); err != nil {
		return fmt.Errorf("record public analytics: %w", err)
	}
	return nil
}

func insertAnswer(ctx context.Context, tx pgx.Tx, submissionID string, field form.Field, value any) error {
	var textValue *string
	var numberValue *float64
	var boolValue *bool
	var dateValue *string
	var timeValue *string
	var timestampValue *string
	var jsonValue *string
	switch {
	case storedNumericField(field.Type):
		number, ok := numberFrom(value)
		if !ok {
			return fmt.Errorf("store numeric answer for field %s", field.ID)
		}
		numberValue = &number
	case field.Type == form.FieldYesNo || field.Type == form.FieldConsent:
		boolean, ok := value.(bool)
		if !ok {
			return fmt.Errorf("store boolean answer for field %s", field.ID)
		}
		boolValue = &boolean
	case field.Type == form.FieldPersianDate:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("store date answer for field %s", field.ID)
		}
		dateValue = &text
	case field.Type == form.FieldTime:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("store time answer for field %s", field.ID)
		}
		timeValue = &text
	case field.Type == form.FieldDateTime:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("store timestamp answer for field %s", field.ID)
		}
		timestampValue = &text
	case field.Type == form.FieldMultiChoice || field.Type == form.FieldMultiDropdown || field.Type == form.FieldMatrix || field.Type == form.FieldDateRange || field.Type == form.FieldFullName || field.Type == form.FieldAddress:
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode structured answer: %w", err)
		}
		text := string(encoded)
		jsonValue = &text
	default:
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("store text answer for field %s", field.ID)
		}
		textValue = &text
	}
	_, err := tx.Exec(ctx, `
        INSERT INTO submission_answers(
            submission_id, field_id, field_type, value_text, value_number, value_bool,
            value_date, value_time, value_timestamp, value_json
        ) VALUES ($1, $2, $3, $4, $5, $6, $7::date, $8::time, $9::timestamptz, $10::jsonb)`,
		submissionID, field.ID, field.Type, textValue, numberValue, boolValue, dateValue, timeValue, timestampValue, jsonValue,
	)
	if err != nil {
		return fmt.Errorf("insert typed answer: %w", err)
	}
	return nil
}

func fieldsByID(schema form.Schema) map[string]form.Field {
	fields := make(map[string]form.Field)
	for _, page := range schema.Pages {
		for _, field := range page.Fields {
			fields[field.ID] = field
		}
	}
	return fields
}

func isUnstoredField(fieldType form.FieldType) bool {
	return fieldType == form.FieldHeading || fieldType == form.FieldDescription || fieldType == form.FieldDivider || fieldType == form.FieldImage || fieldType == form.FieldSpacer || fieldType == form.FieldFile || fieldType == form.FieldSignature
}

func storedNumericField(fieldType form.FieldType) bool {
	return fieldType == form.FieldNumber || fieldType == form.FieldMoney || fieldType == form.FieldPercent || fieldType == form.FieldRating || fieldType == form.FieldScale || fieldType == form.FieldNPS || fieldType == form.FieldCalculated
}

func numberFrom(value any) (float64, bool) {
	switch number := value.(type) {
	case float64:
		return number, true
	case int:
		return float64(number), true
	case json.Number:
		parsed, err := number.Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func submissionSearchText(publicID string, answers map[string]any) string {
	parts := []string{publicID}
	keys := make([]string, 0, len(answers))
	for key := range answers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		appendSearchValues(&parts, answers[key])
	}
	return normalize.SearchText(strings.Join(parts, " "))
}

func appendSearchValues(parts *[]string, value any) {
	switch typed := value.(type) {
	case string:
		*parts = append(*parts, typed)
	case json.Number:
		*parts = append(*parts, typed.String())
	case float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, bool:
		*parts = append(*parts, fmt.Sprint(typed))
	case []string:
		*parts = append(*parts, typed...)
	case []any:
		for _, item := range typed {
			appendSearchValues(parts, item)
		}
	case map[string]string:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			*parts = append(*parts, typed[key])
		}
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			appendSearchValues(parts, typed[key])
		}
	}
}
