package web_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forms/internal/auth"
	formsbackup "forms/internal/backup"
	"forms/internal/database"
	"forms/internal/operations"
	formsrestore "forms/internal/restore"
	formsweb "forms/internal/web"
)

func TestStageFourBackupCreateVerifyListAndPrune(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("FORMS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("FORMS_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, cleanup := isolatedPool(t, ctx, databaseURL)
	defer cleanup()
	migrator := database.NewMigrator(pool)
	if _, err := migrator.Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	var schemaName string
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schemaName); err != nil {
		t.Fatal(err)
	}
	var serverVersionNumber int
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&serverVersionNumber); err != nil {
		t.Fatal(err)
	}
	serverMajor := serverVersionNumber / 10000
	parsedDatabaseURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	query := parsedDatabaseURL.Query()
	query.Set("options", "-csearch_path="+schemaName)
	parsedDatabaseURL.RawQuery = query.Encode()
	isolatedDatabaseURL := parsedDatabaseURL.String()

	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	backupDir := filepath.Join(root, "backups")
	for _, directory := range []string{filepath.Join(dataDir, "uploads", "files"), backupDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	pgDump := filepath.Join(root, "pg_dump")
	pgRestore := filepath.Join(root, "pg_restore")
	if err := os.WriteFile(pgDump, []byte(fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'pg_dump (PostgreSQL) %d.0'; else printf 'stage-four-dump'; fi\n", serverMajor)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pgRestore, []byte(fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'pg_restore (PostgreSQL) %d.0'; fi\nexit 0\n", serverMajor)), 0o700); err != nil {
		t.Fatal(err)
	}
	wrongDump := filepath.Join(root, "pg_dump-wrong-major")
	if err := os.WriteFile(wrongDump, []byte(fmt.Sprintf("#!/bin/sh\necho 'pg_dump (PostgreSQL) %d.0'\n", serverMajor+1)), 0o700); err != nil {
		t.Fatal(err)
	}
	incompatible, err := formsbackup.New(formsbackup.Options{
		Pool: pool, Migrator: migrator, DatabaseURL: isolatedDatabaseURL, DataDir: dataDir, BackupDir: backupDir,
		PGDumpPath: wrongDump, PGRestorePath: pgRestore,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := incompatible.Create(ctx, filepath.Join(backupDir, "forms-20260909T055900Z.tar.gz"), nil); err == nil || !strings.Contains(err.Error(), "server=") {
		t.Fatalf("mismatched PostgreSQL tools were not rejected: %v", err)
	}
	firstTime := time.Date(2026, time.September, 9, 6, 0, 0, 0, time.UTC)
	manager, err := formsbackup.New(formsbackup.Options{
		Pool: pool, Migrator: migrator, DatabaseURL: isolatedDatabaseURL, DataDir: dataDir, BackupDir: backupDir,
		PGDumpPath: pgDump, PGRestorePath: pgRestore, Now: func() time.Time { return firstTime },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO maintenance_writes(request_id, lease_until) VALUES ('in-flight-test', now() + interval '1 minute')`); err != nil {
		t.Fatal(err)
	}
	go func() {
		timer := time.NewTimer(150 * time.Millisecond)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_, _ = pool.Exec(context.Background(), `DELETE FROM maintenance_writes WHERE request_id = 'in-flight-test'`)
		}
	}()
	firstPath := filepath.Join(backupDir, "forms-20260909T060000Z.tar.gz")
	backupStarted := time.Now()
	first, err := manager.Create(ctx, firstPath, map[string]any{"timezone": "Asia/Tehran"})
	if err != nil {
		t.Fatalf("create first backup: %v", err)
	}
	if first.Files != 2 || first.SchemaVersion != 8 || len(first.SHA256) != 64 {
		t.Fatalf("unexpected first backup summary: %+v", first)
	}
	if time.Since(backupStarted) < 100*time.Millisecond {
		t.Fatal("backup did not wait for the in-flight write lease")
	}
	verified, err := manager.Verify(ctx, firstPath)
	if err != nil || verified.SHA256 != first.SHA256 {
		t.Fatalf("verify first backup: summary=%+v error=%v", verified, err)
	}
	var locked bool
	if err := pool.QueryRow(ctx, `SELECT write_locked AND lease_until > now() FROM maintenance_state WHERE singleton = true`).Scan(&locked); err != nil || locked {
		t.Fatalf("maintenance lease was not released: locked=%v error=%v", locked, err)
	}

	secondTime := firstTime.Add(time.Minute)
	secondManager, err := formsbackup.New(formsbackup.Options{
		Pool: pool, Migrator: migrator, DatabaseURL: isolatedDatabaseURL, DataDir: dataDir, BackupDir: backupDir,
		PGDumpPath: pgDump, PGRestorePath: pgRestore, Now: func() time.Time { return secondTime },
	})
	if err != nil {
		t.Fatal(err)
	}
	secondPath := filepath.Join(backupDir, "forms-20260909T060100Z.tar.gz")
	if _, err := secondManager.Create(ctx, secondPath, map[string]any{"timezone": "Asia/Tehran"}); err != nil {
		t.Fatalf("create second backup: %v", err)
	}
	listed, err := secondManager.List(ctx)
	if err != nil || len(listed) != 2 || listed[0].Path != secondPath {
		t.Fatalf("list backups: values=%+v error=%v", listed, err)
	}
	removed, err := secondManager.Prune(ctx, 1)
	if err != nil || len(removed) != 1 || removed[0] != firstPath {
		t.Fatalf("prune backups: removed=%+v error=%v", removed, err)
	}
	if _, err := os.Stat(firstPath); !os.IsNotExist(err) {
		t.Fatalf("pruned backup still exists: %v", err)
	}
	if _, err := os.Stat(secondPath); err != nil {
		t.Fatalf("newest backup was removed: %v", err)
	}
	invalidPath := filepath.Join(backupDir, "forms-invalid.tar.gz")
	if err := os.WriteFile(invalidPath, []byte("not a backup"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := secondManager.Verify(ctx, invalidPath); err == nil {
		t.Fatal("invalid backup unexpectedly verified")
	}

	restoreManager, err := formsrestore.New(formsrestore.Options{
		DatabaseURL: isolatedDatabaseURL, DataDir: dataDir, BackupDir: backupDir, DBMaxConns: 2,
		PGDumpPath: pgDump, PGRestorePath: pgRestore, Now: func() time.Time { return secondTime.Add(time.Minute) },
	})
	if err != nil {
		t.Fatal(err)
	}
	serverLock, err := operations.AcquireProcessLock(dataDir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restoreManager.Apply(ctx, secondPath); !errors.Is(err, operations.ErrServiceRunning) {
		t.Fatalf("online restore error=%v, want service-running rejection", err)
	}
	if err := serverLock.Close(); err != nil {
		t.Fatal(err)
	}
	requestPath, err := restoreManager.Prepare(ctx, secondPath)
	if err != nil {
		t.Fatalf("prepare restore: %v", err)
	}
	if filepath.Base(requestPath) != "restore-request.json" {
		t.Fatalf("unexpected restore request path: %s", requestPath)
	}
	requestPathAgain, err := restoreManager.Prepare(ctx, secondPath)
	if err != nil || requestPathAgain != requestPath {
		t.Fatalf("repeat prepare must be idempotent: path=%q error=%v", requestPathAgain, err)
	}
	alternatePath := filepath.Join(backupDir, "forms-alternate.tar.gz")
	if err := os.Link(secondPath, alternatePath); err != nil {
		t.Fatal(err)
	}
	if _, err := restoreManager.Prepare(ctx, alternatePath); !errors.Is(err, formsrestore.ErrPendingRequest) {
		t.Fatalf("second pending restore error=%v, want ErrPendingRequest", err)
	}
	pending, err := restoreManager.Pending(ctx)
	if err != nil || !pending.Pending || pending.Result == nil || pending.Result.ArchiveSHA256 == "" {
		t.Fatalf("apply pending restore: result=%+v error=%v", pending, err)
	}
	if _, err := os.Stat(requestPath); !os.IsNotExist(err) {
		t.Fatalf("completed restore request was not removed: %v", err)
	}
	emptyPending, err := restoreManager.Pending(ctx)
	if err != nil || emptyPending.Pending {
		t.Fatalf("empty pending state: result=%+v error=%v", emptyPending, err)
	}
}

func TestStageFourBackupPanelCreateListAndDownload(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("FORMS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("FORMS_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, cleanup := isolatedPool(t, ctx, databaseURL)
	defer cleanup()
	migrator := database.NewMigrator(pool)
	if _, err := migrator.Up(ctx); err != nil {
		t.Fatal(err)
	}
	var serverVersionNumber int
	if err := pool.QueryRow(ctx, `SELECT current_setting('server_version_num')::int`).Scan(&serverVersionNumber); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	dataDir, backupDir := filepath.Join(root, "data"), filepath.Join(root, "backups")
	for _, directory := range []string{filepath.Join(dataDir, "tmp"), filepath.Join(dataDir, "uploads", "files"), backupDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	pgDump, pgRestore := filepath.Join(root, "pg_dump"), filepath.Join(root, "pg_restore")
	serverMajor := serverVersionNumber / 10000
	if err := os.WriteFile(pgDump, []byte(fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'pg_dump (PostgreSQL) %d.0'; else printf 'panel-dump'; fi\n", serverMajor)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pgRestore, []byte(fmt.Sprintf("#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'pg_restore (PostgreSQL) %d.0'; fi\nexit 0\n", serverMajor)), 0o700); err != nil {
		t.Fatal(err)
	}
	manager, err := formsbackup.New(formsbackup.Options{
		Pool: pool, Migrator: migrator, DatabaseURL: databaseURL, DataDir: dataDir, BackupDir: backupDir,
		PGDumpPath: pgDump, PGRestorePath: pgRestore,
	})
	if err != nil {
		t.Fatal(err)
	}
	restoreManager, err := formsrestore.New(formsrestore.Options{
		DatabaseURL: databaseURL, DataDir: dataDir, BackupDir: backupDir, DBMaxConns: 2,
		PGDumpPath: pgDump, PGRestorePath: pgRestore,
	})
	if err != nil {
		t.Fatal(err)
	}
	restartRequested := make(chan struct{}, 1)
	authService, err := auth.NewService(auth.NewStore(pool), 30*time.Minute, 12*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := authService.CreateInitialAdmin(ctx, auth.CreateUserInput{
		Email: "backup-admin@example.test", Username: "backup-admin", DisplayName: "مدیر بکاپ",
		Password: adminPassword, Role: auth.RoleAdmin,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(formsweb.New(formsweb.Dependencies{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Pool: pool, Migrator: migrator, Auth: authService,
		DataDir: dataDir, BackupDir: backupDir, Backups: manager, Restores: restoreManager,
		BackupSettings: map[string]any{"timezone": "Asia/Tehran"}, Lifecycle: ctx,
		RequestRestart: func() {
			select {
			case restartRequested <- struct{}{}:
			default:
			}
		},
		Timezone: "Asia/Tehran", SessionAbsolute: 12 * time.Hour,
	}).Handler())
	defer server.Close()
	client := loginClient(t, server.URL, admin.Username, adminPassword)

	type job struct {
		Running bool `json:"running"`
	}
	started := requestData[job](t, client, http.MethodPost, server.URL+"/api/system/backups", nil, http.StatusAccepted)
	if !started.Running {
		t.Fatal("backup panel did not report a running job")
	}
	type item struct {
		Name  string `json:"name"`
		Valid bool   `json:"valid"`
	}
	type listing struct {
		Items []item `json:"items"`
		Job   job    `json:"job"`
	}
	var completed listing
	for range 100 {
		completed = requestData[listing](t, client, http.MethodGet, server.URL+"/api/system/backups", nil, http.StatusOK)
		if !completed.Job.Running {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if completed.Job.Running || len(completed.Items) != 1 || !completed.Items[0].Valid {
		t.Fatalf("unexpected panel backup state: %+v", completed)
	}
	response, err := client.Get(server.URL + "/api/system/backups/" + url.PathEscape(completed.Items[0].Name))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK || len(contents) == 0 || response.Header.Get("Content-Type") != "application/gzip" {
		t.Fatalf("download backup: status=%d bytes=%d type=%q error=%v", response.StatusCode, len(contents), response.Header.Get("Content-Type"), err)
	}
	requestData[map[string]any](t, client, http.MethodPost,
		server.URL+"/api/system/backups/"+url.PathEscape(completed.Items[0].Name)+"/restore",
		map[string]string{"confirmation": "اشتباه"}, http.StatusUnprocessableEntity)
	type restoreRequest struct {
		Pending bool `json:"pending"`
	}
	prepared := requestData[restoreRequest](t, client, http.MethodPost,
		server.URL+"/api/system/backups/"+url.PathEscape(completed.Items[0].Name)+"/restore",
		map[string]string{"confirmation": "بازیابی"}, http.StatusAccepted)
	if !prepared.Pending {
		t.Fatal("restore panel did not report a pending request")
	}
	requestPath := filepath.Join(dataDir, "restore-request.json")
	requestStat, err := os.Stat(requestPath)
	if err != nil {
		t.Fatalf("stat restore request: %v", err)
	}
	if requestStat.Mode().Perm() != 0o600 {
		t.Fatalf("restore request mode=%v, want 0600", requestStat.Mode().Perm())
	}
	select {
	case <-restartRequested:
	case <-time.After(2 * time.Second):
		t.Fatal("restore panel did not request a graceful restart")
	}
}
