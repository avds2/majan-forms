// Package restore applies verified full backups while the web service is
// offline and rolls back database/files if post-restore validation fails.
package restore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"forms/internal/audit"
	formsbackup "forms/internal/backup"
	"forms/internal/database"
	"forms/internal/operations"
)

const requestFormat = "forms-restore-request-v1"

var ErrPendingRequest = errors.New("یک درخواست restore دیگر در انتظار است")

type Options struct {
	DatabaseURL   string
	DataDir       string
	BackupDir     string
	DBMinConns    int32
	DBMaxConns    int32
	PGDumpPath    string
	PGRestorePath string
	Now           func() time.Time
}

type Manager struct {
	options Options
}

type Result struct {
	ArchivePath   string    `json:"archive_path"`
	SafetyBackup  string    `json:"safety_backup"`
	SchemaVersion int64     `json:"schema_version"`
	MigrationsRun int       `json:"migrations_run"`
	RestoredAt    time.Time `json:"restored_at"`
	ArchiveSHA256 string    `json:"archive_sha256"`
}

type PendingResult struct {
	Pending bool    `json:"pending"`
	Result  *Result `json:"result,omitempty"`
}

type request struct {
	Format      string    `json:"format"`
	ArchivePath string    `json:"archive_path"`
	RequestedAt time.Time `json:"requested_at"`
}

func New(options Options) (*Manager, error) {
	if strings.TrimSpace(options.DatabaseURL) == "" || strings.TrimSpace(options.DataDir) == "" || strings.TrimSpace(options.BackupDir) == "" {
		return nil, errors.New("پیکربندی restore کامل نیست")
	}
	if options.PGDumpPath == "" {
		options.PGDumpPath = "pg_dump"
	}
	if options.PGRestorePath == "" {
		options.PGRestorePath = "pg_restore"
	}
	if options.Now == nil {
		options.Now = func() time.Time { return time.Now().UTC() }
	}
	return &Manager{options: options}, nil
}

func (m *Manager) Verify(ctx context.Context, archivePath string) (formsbackup.Summary, error) {
	verifier, err := formsbackup.New(formsbackup.Options{BackupDir: m.options.BackupDir, PGRestorePath: m.options.PGRestorePath})
	if err != nil {
		return formsbackup.Summary{}, err
	}
	return verifier.Verify(ctx, archivePath)
}

func (m *Manager) Prepare(ctx context.Context, archivePath string) (string, error) {
	archivePath, err := filepath.Abs(archivePath)
	if err != nil {
		return "", err
	}
	if _, err := m.Verify(ctx, archivePath); err != nil {
		return "", err
	}
	restoreRequest := request{Format: requestFormat, ArchivePath: archivePath, RequestedAt: m.options.Now().UTC()}
	encoded, err := json.MarshalIndent(restoreRequest, "", "  ")
	if err != nil {
		return "", err
	}
	target := filepath.Join(m.options.DataDir, "restore-request.json")
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		if err == nil {
			existing, readErr := m.readRequest(target)
			if readErr == nil && existing.ArchivePath == archivePath {
				return target, nil
			}
			return "", ErrPendingRequest
		}
		return "", err
	}
	temporary, err := os.CreateTemp(m.options.DataDir, ".restore-request-*.tmp")
	if err != nil {
		return "", err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return "", err
	}
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", err
	}
	if err := temporary.Close(); err != nil {
		return "", err
	}
	// Link publishes without replacing an existing request. os.Rename would
	// overwrite on Unix and make two concurrent administrators race unsafely.
	if err := os.Link(temporaryPath, target); err != nil {
		if os.IsExist(err) {
			existing, readErr := m.readRequest(target)
			if readErr == nil && existing.ArchivePath == archivePath {
				return target, nil
			}
			return "", ErrPendingRequest
		}
		return "", err
	}
	return target, syncDirectory(m.options.DataDir)
}

func (m *Manager) Pending(ctx context.Context) (PendingResult, error) {
	target := filepath.Join(m.options.DataDir, "restore-request.json")
	pending, err := m.readRequest(target)
	if os.IsNotExist(err) {
		return PendingResult{Pending: false}, nil
	}
	if err != nil {
		return PendingResult{}, err
	}
	result, err := m.Apply(ctx, pending.ArchivePath)
	if err != nil {
		return PendingResult{}, err
	}
	if err := os.Remove(target); err != nil {
		return PendingResult{}, fmt.Errorf("remove completed restore request: %w", err)
	}
	if err := syncDirectory(m.options.DataDir); err != nil {
		return PendingResult{}, err
	}
	return PendingResult{Pending: true, Result: &result}, nil
}

func (m *Manager) readRequest(target string) (request, error) {
	stat, statErr := os.Lstat(target)
	if statErr != nil {
		return request{}, statErr
	}
	if !stat.Mode().IsRegular() || stat.Mode().Perm()&0o077 != 0 {
		return request{}, errors.New("فایل درخواست restore باید regular و با دسترسی 0600 باشد")
	}
	file, err := os.Open(target)
	if err != nil {
		return request{}, err
	}
	defer file.Close()
	decoder := json.NewDecoder(io.LimitReader(file, 64<<10))
	decoder.DisallowUnknownFields()
	var pending request
	decodeErr := decoder.Decode(&pending)
	var trailing any
	trailingErr := decoder.Decode(&trailing)
	if decodeErr != nil || !errors.Is(trailingErr, io.EOF) || pending.Format != requestFormat || !filepath.IsAbs(pending.ArchivePath) {
		return request{}, errors.New("درخواست restore معتبر نیست")
	}
	return pending, nil
}

func (m *Manager) Apply(ctx context.Context, archivePath string) (Result, error) {
	if err := os.MkdirAll(filepath.Join(m.options.DataDir, "tmp"), 0o700); err != nil {
		return Result{}, err
	}
	processLock, err := operations.AcquireProcessLock(m.options.DataDir, true)
	if err != nil {
		return Result{}, fmt.Errorf("restore فقط پس از توقف forms.service مجاز است: %w", err)
	}
	defer processLock.Close()

	pool, err := database.Open(ctx, m.options.DatabaseURL, m.options.DBMinConns, m.options.DBMaxConns)
	if err != nil {
		return Result{}, err
	}
	migrator := database.NewMigrator(pool)
	currentStatus, err := migrator.Status(ctx)
	if err != nil {
		pool.Close()
		return Result{}, err
	}
	backupManager, err := formsbackup.New(formsbackup.Options{
		Pool: pool, Migrator: migrator, DatabaseURL: m.options.DatabaseURL,
		DataDir: m.options.DataDir, BackupDir: m.options.BackupDir,
		PGDumpPath: m.options.PGDumpPath, PGRestorePath: m.options.PGRestorePath, Now: m.options.Now,
	})
	if err != nil {
		pool.Close()
		return Result{}, err
	}
	targetSummary, err := backupManager.Verify(ctx, archivePath)
	if err != nil {
		pool.Close()
		return Result{}, err
	}
	if targetSummary.SchemaVersion > currentStatus.Latest {
		pool.Close()
		return Result{}, fmt.Errorf("نسخهٔ schema بکاپ %d از نسخهٔ قابل‌پشتیبانی %d جدیدتر است", targetSummary.SchemaVersion, currentStatus.Latest)
	}
	safetyPath := filepath.Join(m.options.BackupDir, "forms-safety-"+m.options.Now().UTC().Format("20060102T150405.000000000Z")+".tar.gz")
	safetySummary, err := backupManager.Create(ctx, safetyPath, map[string]any{"purpose": "pre-restore-safety"})
	if err != nil {
		pool.Close()
		return Result{}, fmt.Errorf("create safety backup: %w", err)
	}
	targetStage := filepath.Join(m.options.DataDir, "tmp", ".restore-target-"+m.options.Now().UTC().Format("20060102T150405.000000000"))
	safetyStage := targetStage + "-safety"
	defer os.RemoveAll(targetStage)
	defer os.RemoveAll(safetyStage)
	if _, err := backupManager.ExtractVerified(ctx, targetSummary.Path, targetStage); err != nil {
		pool.Close()
		return Result{}, err
	}
	if _, err := backupManager.ExtractVerified(ctx, safetySummary.Path, safetyStage); err != nil {
		pool.Close()
		return Result{}, err
	}
	pool.Close()

	if err := m.restoreDatabase(ctx, filepath.Join(targetStage, "database.dump")); err != nil {
		return Result{}, err
	}
	rollbackFiles, commitFiles, err := m.swapFiles(targetStage)
	if err != nil {
		rollbackErr := m.restoreDatabase(ctx, filepath.Join(safetyStage, "database.dump"))
		return Result{}, errors.Join(err, rollbackErr)
	}
	rollback := func(cause error) error {
		databaseErr := m.restoreDatabase(ctx, filepath.Join(safetyStage, "database.dump"))
		filesErr := rollbackFiles()
		return errors.Join(cause, databaseErr, filesErr)
	}

	restoredPool, err := database.Open(ctx, m.options.DatabaseURL, m.options.DBMinConns, m.options.DBMaxConns)
	if err != nil {
		return Result{}, rollback(err)
	}
	restoredMigrator := database.NewMigrator(restoredPool)
	migrationsRun, err := restoredMigrator.Up(ctx)
	if err == nil {
		err = restoredMigrator.CheckCurrent(ctx)
	}
	if err == nil {
		err = audit.Insert(ctx, restoredPool, nil, "restore.applied", "backup", filepath.Base(targetSummary.Path), map[string]any{
			"archive_sha256": targetSummary.SHA256, "safety_backup": filepath.Base(safetyPath), "migrations_run": migrationsRun,
		})
	}
	restoredPool.Close()
	if err != nil {
		return Result{}, rollback(err)
	}
	if err := commitFiles(); err != nil {
		return Result{}, err
	}
	return Result{
		ArchivePath: targetSummary.Path, SafetyBackup: safetyPath, SchemaVersion: targetSummary.SchemaVersion,
		MigrationsRun: migrationsRun, RestoredAt: m.options.Now().UTC(), ArchiveSHA256: targetSummary.SHA256,
	}, nil
}

func (m *Manager) restoreDatabase(ctx context.Context, dumpPath string) error {
	dump, err := os.Open(dumpPath)
	if err != nil {
		return err
	}
	defer dump.Close()
	command := exec.CommandContext(ctx, m.options.PGRestorePath,
		"--clean", "--if-exists", "--single-transaction", "--no-owner", "--no-privileges",
		"--dbname", m.options.DatabaseURL,
	)
	command.Env = append(os.Environ(), "PGCONNECT_TIMEOUT=10")
	command.Stdin = dump
	var stderr limitedWriter
	command.Stdout = io.Discard
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("pg_restore failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (m *Manager) swapFiles(staging string) (rollback func() error, commit func() error, err error) {
	current := filepath.Join(m.options.DataDir, "uploads", "files")
	incoming := filepath.Join(staging, "uploads")
	if err := os.MkdirAll(incoming, 0o700); err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(current), 0o700); err != nil {
		return nil, nil, err
	}
	if _, statErr := os.Stat(current); os.IsNotExist(statErr) {
		if err := os.Mkdir(current, 0o700); err != nil {
			return nil, nil, err
		}
	} else if statErr != nil {
		return nil, nil, statErr
	}
	old := filepath.Join(m.options.DataDir, "tmp", ".restore-old-files-"+m.options.Now().UTC().Format("20060102T150405.000000000"))
	if err := os.Rename(current, old); err != nil {
		return nil, nil, fmt.Errorf("stage current uploads: %w", err)
	}
	if err := os.Rename(incoming, current); err != nil {
		_ = os.Rename(old, current)
		return nil, nil, fmt.Errorf("activate restored uploads: %w", err)
	}
	rollback = func() error {
		failed := old + ".failed"
		if err := os.Rename(current, failed); err != nil {
			return err
		}
		if err := os.Rename(old, current); err != nil {
			return err
		}
		return os.RemoveAll(failed)
	}
	commit = func() error { return os.RemoveAll(old) }
	return rollback, commit, nil
}

func syncDirectory(directory string) error {
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

type limitedWriter struct{ data []byte }

func (w *limitedWriter) Write(value []byte) (int, error) {
	const limit = 16 << 10
	remaining := limit - len(w.data)
	if remaining > 0 {
		amount := len(value)
		if amount > remaining {
			amount = remaining
		}
		w.data = append(w.data, value[:amount]...)
	}
	return len(value), nil
}

func (w *limitedWriter) String() string { return string(w.data) }
