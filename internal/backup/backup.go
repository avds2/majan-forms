// Package backup creates and validates full, versioned application backups.
package backup

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"forms/internal/audit"
	"forms/internal/database"
	"forms/internal/id"
	"forms/internal/version"
)

const (
	Format             = "forms-backup-v1"
	FormatVersion      = 1
	maximumManifest    = 1 << 20
	maximumArchiveSize = int64(1 << 40)
	leaseDuration      = 2 * time.Minute
)

var (
	ErrInvalidArchive  = errors.New("فایل بکاپ معتبر نیست")
	ErrMaintenanceBusy = errors.New("سامانه در حالت نگهداری دیگری است")
	managedBackupName  = regexp.MustCompile(`^forms-[0-9]{8}T[0-9]{6}Z\.tar\.gz$`)
)

type Component struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Manifest struct {
	Format         string      `json:"format"`
	FormatVersion  int         `json:"format_version"`
	AppVersion     string      `json:"app_version"`
	SchemaVersion  int64       `json:"schema_version"`
	CreatedAt      time.Time   `json:"created_at"`
	DatabaseFormat string      `json:"database_format"`
	Components     []Component `json:"components"`
}

type Summary struct {
	Path          string    `json:"path"`
	Valid         bool      `json:"valid"`
	Error         string    `json:"error,omitempty"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	CreatedAt     time.Time `json:"created_at"`
	AppVersion    string    `json:"app_version"`
	SchemaVersion int64     `json:"schema_version"`
	Files         int       `json:"files"`
}

type Options struct {
	Pool          *pgxpool.Pool
	Migrator      *database.Migrator
	DatabaseURL   string
	DataDir       string
	BackupDir     string
	PGDumpPath    string
	PGRestorePath string
	Now           func() time.Time
}

type Manager struct {
	pool          *pgxpool.Pool
	migrator      *database.Migrator
	databaseURL   string
	dataDir       string
	backupDir     string
	pgDumpPath    string
	pgRestorePath string
	now           func() time.Time
}

func New(options Options) (*Manager, error) {
	if strings.TrimSpace(options.BackupDir) == "" {
		return nil, errors.New("پیکربندی بکاپ کامل نیست")
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
	return &Manager{
		pool: options.Pool, migrator: options.Migrator, databaseURL: options.DatabaseURL,
		dataDir: options.DataDir, backupDir: options.BackupDir,
		pgDumpPath: options.PGDumpPath, pgRestorePath: options.PGRestorePath, now: options.Now,
	}, nil
}

func (m *Manager) Create(ctx context.Context, output string, settings any) (summary Summary, err error) {
	if m.pool == nil || m.migrator == nil || m.databaseURL == "" || m.dataDir == "" {
		return Summary{}, errors.New("ساخت بکاپ به اتصال پایگاه‌داده و data_dir نیاز دارد")
	}
	if _, err := CheckPostgreSQLToolCompatibility(ctx, m.pool, m.pgDumpPath, m.pgRestorePath); err != nil {
		return Summary{}, err
	}
	if err := ensurePrivateDirectory(m.backupDir); err != nil {
		return Summary{}, err
	}
	if output == "" {
		output = filepath.Join(m.backupDir, "forms-"+m.now().UTC().Format("20060102T150405Z")+".tar.gz")
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return Summary{}, fmt.Errorf("resolve backup output: %w", err)
	}
	if !strings.HasSuffix(output, ".tar.gz") {
		return Summary{}, errors.New("نام بکاپ باید با .tar.gz تمام شود")
	}
	if err := ensurePrivateDirectory(filepath.Dir(output)); err != nil {
		return Summary{}, err
	}
	if _, statErr := os.Lstat(output); !os.IsNotExist(statErr) {
		if statErr == nil {
			return Summary{}, fmt.Errorf("فایل خروجی از قبل وجود دارد: %s", output)
		}
		return Summary{}, statErr
	}

	token, err := m.acquireMaintenance(ctx, "backup")
	if err != nil {
		return Summary{}, err
	}
	leaseContext, cancelLease := context.WithCancel(context.Background())
	defer cancelLease()
	go m.renewLease(leaseContext, token)
	defer func() {
		releaseErr := m.releaseMaintenance(context.WithoutCancel(ctx), token)
		if err == nil && releaseErr != nil {
			err = releaseErr
		}
	}()

	status, err := m.migrator.Status(ctx)
	if err != nil {
		return Summary{}, err
	}
	if status.Pending != 0 {
		return Summary{}, errors.New("پیش از بکاپ migrationهای معوق را اجرا کنید")
	}
	uploads, uploadBytes, err := m.finalizedUploads(ctx)
	if err != nil {
		return Summary{}, err
	}
	var databaseBytes int64
	if err := m.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())`).Scan(&databaseBytes); err != nil {
		return Summary{}, fmt.Errorf("estimate database size: %w", err)
	}
	if err := requireFreeSpace(filepath.Dir(output), databaseBytes+uploadBytes); err != nil {
		return Summary{}, err
	}

	staging, err := os.MkdirTemp(filepath.Dir(output), ".forms-backup-stage-*")
	if err != nil {
		return Summary{}, fmt.Errorf("create backup staging directory: %w", err)
	}
	defer os.RemoveAll(staging)
	if err := os.Chmod(staging, 0o700); err != nil {
		return Summary{}, err
	}
	dumpPath := filepath.Join(staging, "database.dump")
	if err := m.dumpDatabase(ctx, dumpPath); err != nil {
		return Summary{}, err
	}
	settingsPath := filepath.Join(staging, "settings.json")
	if err := writeJSONFile(settingsPath, settings); err != nil {
		return Summary{}, err
	}

	sources := []archiveSource{{Name: "database.dump", Path: dumpPath}, {Name: "settings.json", Path: settingsPath}}
	for _, upload := range uploads {
		sources = append(sources, archiveSource{Name: "uploads/" + upload.StorageKey, Path: upload.Path, ExpectedSize: upload.Size, ExpectedSHA256: upload.SHA256})
	}
	components := make([]Component, 0, len(sources))
	for _, source := range sources {
		component, hashErr := hashSource(source)
		if hashErr != nil {
			return Summary{}, hashErr
		}
		components = append(components, component)
	}
	manifest := Manifest{
		Format: Format, FormatVersion: FormatVersion, AppVersion: version.Current().Version,
		SchemaVersion: status.Current, CreatedAt: m.now().UTC(), DatabaseFormat: "postgresql-custom", Components: components,
	}
	manifestPath := filepath.Join(staging, "manifest.json")
	if err := writeJSONFile(manifestPath, manifest); err != nil {
		return Summary{}, err
	}
	sources = append([]archiveSource{{Name: "manifest.json", Path: manifestPath}}, sources...)

	partial, err := os.CreateTemp(filepath.Dir(output), ".forms-backup-*.partial")
	if err != nil {
		return Summary{}, fmt.Errorf("create partial backup: %w", err)
	}
	partialPath := partial.Name()
	defer os.Remove(partialPath)
	if err := partial.Chmod(0o600); err != nil {
		partial.Close()
		return Summary{}, err
	}
	if err := writeArchive(partial, sources, manifest.CreatedAt); err != nil {
		partial.Close()
		return Summary{}, err
	}
	if err := partial.Sync(); err != nil {
		partial.Close()
		return Summary{}, fmt.Errorf("sync backup: %w", err)
	}
	if err := partial.Close(); err != nil {
		return Summary{}, fmt.Errorf("close backup: %w", err)
	}
	if err := os.Rename(partialPath, output); err != nil {
		return Summary{}, fmt.Errorf("publish backup atomically: %w", err)
	}
	if err := syncDirectory(filepath.Dir(output)); err != nil {
		return Summary{}, err
	}
	summary, err = m.Verify(ctx, output)
	if err != nil {
		return Summary{}, fmt.Errorf("verify freshly-created backup: %w", err)
	}
	if err := audit.Insert(ctx, m.pool, nil, "backup.created", "backup", filepath.Base(output), map[string]any{
		"size": summary.Size, "sha256": summary.SHA256, "schema_version": summary.SchemaVersion,
	}); err != nil {
		return Summary{}, err
	}
	return summary, nil
}

func (m *Manager) Verify(ctx context.Context, archivePath string) (Summary, error) {
	archivePath, err := filepath.Abs(archivePath)
	if err != nil {
		return Summary{}, err
	}
	stat, err := os.Lstat(archivePath)
	if err != nil {
		return Summary{}, err
	}
	if !stat.Mode().IsRegular() || stat.Mode()&os.ModeSymlink != 0 || stat.Size() > maximumArchiveSize {
		return Summary{}, ErrInvalidArchive
	}
	archiveSHA, err := hashFile(archivePath)
	if err != nil {
		return Summary{}, err
	}
	file, err := os.Open(archivePath)
	if err != nil {
		return Summary{}, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return Summary{}, ErrInvalidArchive
	}
	gzipReader.Multistream(false)
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	seen := make(map[string]Component)
	var manifest Manifest
	manifestSeen := false
	tempDump, err := os.CreateTemp("", ".forms-verify-*.dump")
	if err != nil {
		return Summary{}, err
	}
	tempDumpPath := tempDump.Name()
	defer os.Remove(tempDumpPath)
	defer tempDump.Close()
	for {
		header, nextErr := tarReader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil || header.Typeflag != tar.TypeReg || !safeArchivePath(header.Name) || header.Size < 0 || header.Size > maximumArchiveSize {
			return Summary{}, ErrInvalidArchive
		}
		if _, duplicate := seen[header.Name]; duplicate || header.Name == "manifest.json" && manifestSeen {
			return Summary{}, ErrInvalidArchive
		}
		if header.Name == "manifest.json" {
			if header.Size > maximumManifest {
				return Summary{}, ErrInvalidArchive
			}
			contents, readErr := io.ReadAll(io.LimitReader(tarReader, header.Size+1))
			if readErr != nil || int64(len(contents)) != header.Size {
				return Summary{}, ErrInvalidArchive
			}
			decoder := json.NewDecoder(bytes.NewReader(contents))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&manifest); err != nil {
				return Summary{}, ErrInvalidArchive
			}
			var trailing any
			if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
				return Summary{}, ErrInvalidArchive
			}
			manifestSeen = true
			continue
		}
		digest := sha256.New()
		writer := io.Writer(digest)
		if header.Name == "database.dump" {
			writer = io.MultiWriter(digest, tempDump)
		}
		copied, copyErr := io.Copy(writer, tarReader)
		if copyErr != nil || copied != header.Size {
			return Summary{}, ErrInvalidArchive
		}
		seen[header.Name] = Component{Path: header.Name, Size: copied, SHA256: hex.EncodeToString(digest.Sum(nil))}
	}
	if !manifestSeen || manifest.Format != Format || manifest.FormatVersion != FormatVersion ||
		manifest.DatabaseFormat != "postgresql-custom" || manifest.SchemaVersion < 1 || manifest.CreatedAt.IsZero() {
		return Summary{}, ErrInvalidArchive
	}
	expected := make(map[string]Component, len(manifest.Components))
	for _, component := range manifest.Components {
		if !safeArchivePath(component.Path) || component.Path == "manifest.json" || component.Size < 0 || len(component.SHA256) != 64 {
			return Summary{}, ErrInvalidArchive
		}
		if _, duplicate := expected[component.Path]; duplicate {
			return Summary{}, ErrInvalidArchive
		}
		expected[component.Path] = component
	}
	if len(expected) != len(seen) {
		return Summary{}, ErrInvalidArchive
	}
	for name, component := range expected {
		actual, ok := seen[name]
		if !ok || actual.Size != component.Size || actual.SHA256 != component.SHA256 {
			return Summary{}, ErrInvalidArchive
		}
	}
	if _, ok := expected["database.dump"]; !ok {
		return Summary{}, ErrInvalidArchive
	}
	if _, ok := expected["settings.json"]; !ok {
		return Summary{}, ErrInvalidArchive
	}
	if err := tempDump.Close(); err != nil {
		return Summary{}, err
	}
	if err := m.inspectDump(ctx, tempDumpPath); err != nil {
		return Summary{}, err
	}
	return Summary{
		Path: archivePath, Valid: true, Size: stat.Size(), SHA256: archiveSHA, CreatedAt: manifest.CreatedAt,
		AppVersion: manifest.AppVersion, SchemaVersion: manifest.SchemaVersion, Files: len(manifest.Components),
	}, nil
}

// ExtractVerified validates an archive before extracting its regular files to
// a new private directory. The caller owns the returned directory afterward.
func (m *Manager) ExtractVerified(ctx context.Context, archivePath, destination string) (summary Summary, err error) {
	summary, err = m.Verify(ctx, archivePath)
	if err != nil {
		return Summary{}, err
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return Summary{}, fmt.Errorf("create restore staging directory: %w", err)
	}
	remove := true
	defer func() {
		if remove {
			_ = os.RemoveAll(destination)
		}
	}()
	file, err := os.Open(summary.Path)
	if err != nil {
		return Summary{}, err
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return Summary{}, ErrInvalidArchive
	}
	gzipReader.Multistream(false)
	defer gzipReader.Close()
	tape := tar.NewReader(gzipReader)
	for {
		header, nextErr := tape.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil || header.Typeflag != tar.TypeReg || !safeArchivePath(header.Name) {
			return Summary{}, ErrInvalidArchive
		}
		target := filepath.Join(destination, filepath.FromSlash(header.Name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return Summary{}, err
		}
		output, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return Summary{}, err
		}
		written, copyErr := io.Copy(output, tape)
		syncErr := output.Sync()
		closeErr := output.Close()
		if copyErr != nil || written != header.Size {
			return Summary{}, ErrInvalidArchive
		}
		if syncErr != nil {
			return Summary{}, syncErr
		}
		if closeErr != nil {
			return Summary{}, closeErr
		}
	}
	after, err := m.Verify(ctx, archivePath)
	if err != nil || after.SHA256 != summary.SHA256 {
		return Summary{}, ErrInvalidArchive
	}
	remove = false
	return summary, nil
}

func (m *Manager) List(ctx context.Context) ([]Summary, error) {
	if err := ensurePrivateDirectory(m.backupDir); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(m.backupDir)
	if err != nil {
		return nil, err
	}
	result := make([]Summary, 0)
	for _, entry := range entries {
		if entry.IsDir() || !managedBackupName.MatchString(entry.Name()) {
			continue
		}
		summary, verifyErr := m.Verify(ctx, filepath.Join(m.backupDir, entry.Name()))
		if verifyErr != nil {
			result = append(result, Summary{Path: filepath.Join(m.backupDir, entry.Name()), Valid: false, Error: verifyErr.Error()})
			continue
		}
		result = append(result, summary)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.After(result[j].CreatedAt) })
	return result, nil
}

func (m *Manager) Prune(ctx context.Context, keep int) ([]string, error) {
	if keep < 1 || keep > 10000 {
		return nil, errors.New("مقدار --keep باید بین ۱ و ۱۰۰۰۰ باشد")
	}
	backups, err := m.List(ctx)
	if err != nil {
		return nil, err
	}
	valid := make([]Summary, 0, len(backups))
	for _, item := range backups {
		if item.Valid {
			valid = append(valid, item)
		}
	}
	removed := make([]string, 0)
	for _, backup := range valid[minimum(keep, len(valid)):] {
		if filepath.Dir(backup.Path) != filepath.Clean(m.backupDir) {
			return nil, errors.New("مسیر بکاپ خارج از پوشهٔ مجاز است")
		}
		if err := os.Remove(backup.Path); err != nil {
			return removed, err
		}
		removed = append(removed, backup.Path)
	}
	if len(removed) > 0 {
		if err := syncDirectory(m.backupDir); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

type uploadSource struct {
	StorageKey, Path, SHA256 string
	Size                     int64
}

func (m *Manager) finalizedUploads(ctx context.Context) ([]uploadSource, int64, error) {
	rows, err := m.pool.Query(ctx, `SELECT storage_key, size_bytes, checksum FROM stored_files WHERE status = 'finalized' ORDER BY storage_key`)
	if err != nil {
		return nil, 0, fmt.Errorf("list finalized uploads: %w", err)
	}
	defer rows.Close()
	values := make([]uploadSource, 0)
	var total int64
	for rows.Next() {
		var value uploadSource
		if err := rows.Scan(&value.StorageKey, &value.Size, &value.SHA256); err != nil {
			return nil, 0, err
		}
		if value.StorageKey != filepath.Base(value.StorageKey) || strings.ContainsAny(value.StorageKey, `/\\`) {
			return nil, 0, errors.New("storage key نامعتبر در پایگاه‌داده")
		}
		value.Path = filepath.Join(m.dataDir, "uploads", "files", value.StorageKey)
		total += value.Size
		values = append(values, value)
	}
	return values, total, rows.Err()
}

func (m *Manager) dumpDatabase(ctx context.Context, target string) error {
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	command := exec.CommandContext(ctx, m.pgDumpPath, "--format=custom", "--no-owner", "--no-privileges", "--dbname", m.databaseURL)
	command.Env = append(os.Environ(), "PGCONNECT_TIMEOUT=10")
	command.Stdout = file
	var stderr limitedBuffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		file.Close()
		return fmt.Errorf("pg_dump failed: %w: %s", err, stderr.String())
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func (m *Manager) inspectDump(ctx context.Context, dumpPath string) error {
	file, err := os.Open(dumpPath)
	if err != nil {
		return err
	}
	defer file.Close()
	command := exec.CommandContext(ctx, m.pgRestorePath, "--list")
	command.Stdin = file
	command.Stdout = io.Discard
	var stderr limitedBuffer
	command.Stderr = &stderr
	if err := command.Run(); err != nil {
		return fmt.Errorf("%w: pg_restore: %s", ErrInvalidArchive, stderr.String())
	}
	return nil
}

func (m *Manager) acquireMaintenance(ctx context.Context, reason string) (string, error) {
	token, err := id.New()
	if err != nil {
		return "", err
	}
	var acquired bool
	err = m.pool.QueryRow(ctx, `
		UPDATE maintenance_state
		SET write_locked = true, reason = $2, owner_token = $1, locked_at = now(), lease_until = now() + $3::interval
		WHERE singleton = true AND (NOT write_locked OR lease_until <= now())
		RETURNING true`, token, reason, leaseDuration.String()).Scan(&acquired)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrMaintenanceBusy
	}
	if err != nil || !acquired {
		return "", fmt.Errorf("acquire maintenance lock: %w", err)
	}
	if err := m.waitForActiveWrites(ctx); err != nil {
		_ = m.releaseMaintenance(context.WithoutCancel(ctx), token)
		return "", err
	}
	return token, nil
}

func (m *Manager) waitForActiveWrites(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := m.pool.Exec(ctx, `DELETE FROM maintenance_writes WHERE lease_until <= now()`); err != nil {
			return fmt.Errorf("clean expired write leases: %w", err)
		}
		var active int
		if err := m.pool.QueryRow(ctx, `SELECT count(*) FROM maintenance_writes`).Scan(&active); err != nil {
			return fmt.Errorf("count active writes: %w", err)
		}
		if active == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (m *Manager) renewLease(ctx context.Context, token string) {
	ticker := time.NewTicker(leaseDuration / 3)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = m.pool.Exec(ctx, `UPDATE maintenance_state SET lease_until = now() + $2::interval WHERE singleton = true AND owner_token = $1`, token, leaseDuration.String())
		}
	}
}

func (m *Manager) releaseMaintenance(ctx context.Context, token string) error {
	_, err := m.pool.Exec(ctx, `
		UPDATE maintenance_state SET write_locked = false, reason = '', owner_token = NULL, locked_at = NULL, lease_until = NULL
		WHERE singleton = true AND owner_token = $1`, token)
	if err != nil {
		return fmt.Errorf("release maintenance lock: %w", err)
	}
	return nil
}

type archiveSource struct {
	Name, Path     string
	ExpectedSize   int64
	ExpectedSHA256 string
}

func hashSource(source archiveSource) (Component, error) {
	stat, err := os.Lstat(source.Path)
	if err != nil {
		return Component{}, fmt.Errorf("inspect backup component %q: %w", source.Name, err)
	}
	if !stat.Mode().IsRegular() || stat.Mode()&os.ModeSymlink != 0 {
		return Component{}, fmt.Errorf("backup component %q is not a regular file", source.Name)
	}
	digest, err := hashFile(source.Path)
	if err != nil {
		return Component{}, err
	}
	if source.ExpectedSize > 0 && stat.Size() != source.ExpectedSize || source.ExpectedSHA256 != "" && digest != source.ExpectedSHA256 {
		return Component{}, fmt.Errorf("upload %q does not match database metadata", source.Name)
	}
	return Component{Path: source.Name, Size: stat.Size(), SHA256: digest}, nil
}

func writeArchive(output *os.File, sources []archiveSource, timestamp time.Time) error {
	gzipWriter := gzip.NewWriter(output)
	gzipWriter.Header.ModTime = timestamp
	gzipWriter.Header.OS = 255
	tarWriter := tar.NewWriter(gzipWriter)
	for _, source := range sources {
		stat, err := os.Stat(source.Path)
		if err != nil {
			return err
		}
		header := &tar.Header{Name: source.Name, Mode: 0o600, Size: stat.Size(), ModTime: timestamp, Typeflag: tar.TypeReg, Format: tar.FormatPAX}
		if err := tarWriter.WriteHeader(header); err != nil {
			return fmt.Errorf("write backup header: %w", err)
		}
		file, err := os.Open(source.Path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tarWriter, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := tarWriter.Close(); err != nil {
		return err
	}
	return gzipWriter.Close()
}

func writeJSONFile(target string, value any) error {
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(value)
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func hashFile(filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

func safeArchivePath(value string) bool {
	return value != "" && !strings.Contains(value, "\\") && !path.IsAbs(value) && path.Clean(value) == value && value != "." && !strings.HasPrefix(value, "../")
}

func ensurePrivateDirectory(directory string) error {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create private directory %q: %w", directory, err)
	}
	stat, err := os.Stat(directory)
	if err != nil {
		return err
	}
	if !stat.IsDir() || stat.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("دسترسی پوشهٔ %s باید 0700 یا محدودتر باشد", directory)
	}
	return nil
}

func requireFreeSpace(directory string, estimated int64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(directory, &stat); err != nil {
		return fmt.Errorf("check backup free space: %w", err)
	}
	available := int64(stat.Bavail) * int64(stat.Bsize)
	required := estimated + estimated/5 + 64<<20
	if available < required {
		return fmt.Errorf("فضای آزاد برای بکاپ کافی نیست: available=%d required=%d", available, required)
	}
	return nil
}

func syncDirectory(directory string) error {
	file, err := os.Open(directory)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

type limitedBuffer struct {
	mu   sync.Mutex
	data []byte
}

func (b *limitedBuffer) Write(value []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	const limit = 4096
	remaining := limit - len(b.data)
	if remaining > 0 {
		b.data = append(b.data, value[:minimum(remaining, len(value))]...)
	}
	return len(value), nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.data))
}

func minimum(left, right int) int {
	if left < right {
		return left
	}
	return right
}
