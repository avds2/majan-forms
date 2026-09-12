package web_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"forms/internal/auth"
	"forms/internal/database"
	"forms/internal/form"
	"forms/internal/id"
	"forms/internal/project"
	formsweb "forms/internal/web"
)

const (
	adminPassword    = "stage-one-admin-password"
	designerPassword = "stage-one-designer-password"
	viewerPassword   = "stage-one-viewer-password"
)

func TestStageOneWorkflow(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("FORMS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("FORMS_TEST_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, cleanup := isolatedPool(t, ctx, databaseURL)
	defer cleanup()

	migrator := database.NewMigrator(pool)
	applied, err := migrator.Up(ctx)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if applied == 0 {
		t.Fatal("expected at least one migration in a fresh schema")
	}
	if reapplied, err := migrator.Up(ctx); err != nil || reapplied != 0 {
		t.Fatalf("migrations are not idempotent: applied=%d error=%v", reapplied, err)
	}
	status, err := migrator.Status(ctx)
	if err != nil || status.Pending != 0 || status.Current != status.Latest {
		t.Fatalf("unexpected migration status: %+v error=%v", status, err)
	}

	authService, err := auth.NewService(auth.NewStore(pool), 30*time.Minute, 12*time.Hour)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	initialAdmin := auth.CreateUserInput{
		Email: "admin@example.test", Username: "admin", DisplayName: "مدیر سامانه",
		Password: adminPassword, Role: auth.RoleAdmin,
	}
	type initialAdminResult struct {
		user auth.User
		err  error
	}
	results := make(chan initialAdminResult, 2)
	start := make(chan struct{})
	for range 2 {
		go func() {
			<-start
			user, err := authService.CreateInitialAdmin(ctx, initialAdmin)
			results <- initialAdminResult{user: user, err: err}
		}()
	}
	close(start)
	var admin auth.User
	adminsCreated, adminsRejected := 0, 0
	for range 2 {
		result := <-results
		switch {
		case result.err == nil:
			admin = result.user
			adminsCreated++
		case errors.Is(result.err, auth.ErrUsersExist):
			adminsRejected++
		default:
			t.Fatalf("create initial administrator concurrently: %v", result.err)
		}
	}
	if adminsCreated != 1 || adminsRejected != 1 {
		t.Fatalf("initial administrator creation is not atomic: created=%d rejected=%d", adminsCreated, adminsRejected)
	}

	dataDir := t.TempDir()
	if err := os.MkdirAll(dataDir+"/tmp", 0o700); err != nil {
		t.Fatalf("create health-check directory: %v", err)
	}
	logger := slog.New(slog.NewJSONHandler(io.Discard, nil))
	server := httptest.NewServer(formsweb.New(formsweb.Dependencies{
		Logger: logger, Pool: pool, Migrator: migrator, Auth: authService,
		Projects: project.NewService(project.NewStore(pool)), Forms: form.NewService(form.NewStore(pool)),
		DataDir: dataDir, Timezone: "Asia/Tehran", CookieSecure: false, SessionAbsolute: 12 * time.Hour,
	}).Handler())
	defer server.Close()

	adminClient := newHTTPClient(t)
	runtimeConfig := requestData[map[string]string](t, adminClient, http.MethodGet, server.URL+"/api/runtime-config", nil, http.StatusOK)
	if runtimeConfig["timezone"] != "Asia/Tehran" {
		t.Fatalf("unexpected runtime timezone: %q", runtimeConfig["timezone"])
	}
	loggedInAdmin := requestData[auth.User](t, adminClient, http.MethodPost, server.URL+"/api/auth/login", map[string]any{
		"identifier": "ADMIN", "password": adminPassword,
	}, http.StatusOK)
	if loggedInAdmin.ID != admin.ID || loggedInAdmin.Role != auth.RoleAdmin {
		t.Fatalf("unexpected logged-in admin: %+v", loggedInAdmin)
	}
	me := requestData[auth.User](t, adminClient, http.MethodGet, server.URL+"/api/auth/me", nil, http.StatusOK)
	if me.ID != admin.ID {
		t.Fatalf("session resolved to wrong user: %s", me.ID)
	}

	designer := requestData[auth.User](t, adminClient, http.MethodPost, server.URL+"/api/users", auth.CreateUserInput{
		Email: "designer@example.test", Username: "designer", DisplayName: "طراح فرم",
		Password: designerPassword, Role: auth.RoleDesigner,
	}, http.StatusCreated)
	viewer := requestData[auth.User](t, adminClient, http.MethodPost, server.URL+"/api/users", auth.CreateUserInput{
		Email: "viewer@example.test", Username: "viewer", DisplayName: "مشاهده‌گر",
		Password: viewerPassword, Role: auth.RoleViewer,
	}, http.StatusCreated)
	users := requestData[[]auth.User](t, adminClient, http.MethodGet, server.URL+"/api/users", nil, http.StatusOK)
	if len(users) != 3 {
		t.Fatalf("expected three users, got %d", len(users))
	}
	viewer = requestData[auth.User](t, adminClient, http.MethodPatch, server.URL+"/api/users/"+viewer.ID, auth.UpdateUserInput{
		DisplayName: "بازبین فرم", Role: auth.RoleViewer, Active: true,
	}, http.StatusOK)
	if viewer.DisplayName != "بازبین فرم" {
		t.Fatalf("viewer update did not persist: %+v", viewer)
	}
	requestStatus(t, adminClient, http.MethodPatch, server.URL+"/api/users/"+admin.ID, auth.UpdateUserInput{
		DisplayName: admin.DisplayName, Role: auth.RoleViewer, Active: true,
	}, http.StatusConflict)

	designerClient := loginClient(t, server.URL, "designer", designerPassword)
	requestStatus(t, designerClient, http.MethodGet, server.URL+"/api/users", nil, http.StatusForbidden)
	created := requestData[project.Project](t, designerClient, http.MethodPost, server.URL+"/api/projects", project.CreateInput{
		Title: "فرم درخواست همکاری", Slug: "hiring-form-1405",
	}, http.StatusCreated)
	if created.CreatedBy != designer.ID || created.Status != project.StatusDraft {
		t.Fatalf("unexpected created project: %+v", created)
	}
	created = requestData[project.Project](t, designerClient, http.MethodPatch, server.URL+"/api/projects/"+created.ID, project.UpdateInput{
		Title: "فرم همکاری به‌روز", Slug: "hiring-form-1405-updated",
	}, http.StatusOK)
	if created.Slug != "hiring-form-1405-updated" {
		t.Fatalf("project update did not persist: %+v", created)
	}
	projects := requestData[[]project.Project](t, designerClient, http.MethodGet, server.URL+"/api/projects", nil, http.StatusOK)
	if len(projects) != 1 || projects[0].ID != created.ID {
		t.Fatalf("unexpected project list: %+v", projects)
	}

	draft := requestData[form.Draft](t, designerClient, http.MethodGet, server.URL+"/api/projects/"+created.ID+"/draft", nil, http.StatusOK)
	fieldID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	draft.Schema.Pages[0].Fields = append(draft.Schema.Pages[0].Fields, form.Field{
		ID: fieldID, Type: form.FieldPersianDate, Label: "تاریخ شروع",
		Date: &form.DateRule{Minimum: "2026-03-21", Maximum: "2027-03-20"},
	})
	saved := requestData[form.Draft](t, designerClient, http.MethodPut, server.URL+"/api/projects/"+created.ID+"/draft", map[string]any{
		"expected_revision": draft.Revision, "schema": draft.Schema,
	}, http.StatusOK)
	if saved.Revision != draft.Revision+1 || saved.Checksum == draft.Checksum {
		t.Fatalf("draft revision/checksum did not change: before=%+v after=%+v", draft, saved)
	}
	requestStatus(t, designerClient, http.MethodPut, server.URL+"/api/projects/"+created.ID+"/draft", map[string]any{
		"expected_revision": draft.Revision, "schema": draft.Schema,
	}, http.StatusConflict)

	viewerClient := loginClient(t, server.URL, "viewer@example.test", viewerPassword)
	requestData[[]project.Project](t, viewerClient, http.MethodGet, server.URL+"/api/projects", nil, http.StatusOK)
	requestData[form.Draft](t, viewerClient, http.MethodGet, server.URL+"/api/projects/"+created.ID+"/draft", nil, http.StatusOK)
	requestStatus(t, viewerClient, http.MethodPost, server.URL+"/api/projects", project.CreateInput{Title: "نباید ساخته شود", Slug: "forbidden"}, http.StatusForbidden)
	requestStatus(t, viewerClient, http.MethodPut, server.URL+"/api/projects/"+created.ID+"/draft", map[string]any{
		"expected_revision": saved.Revision, "schema": saved.Schema,
	}, http.StatusForbidden)
	requestStatus(t, viewerClient, http.MethodGet, server.URL+"/api/projects?trash=true", nil, http.StatusForbidden)

	requestData[project.Project](t, designerClient, http.MethodDelete, server.URL+"/api/projects/"+created.ID, nil, http.StatusOK)
	requestStatus(t, designerClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/restore", nil, http.StatusForbidden)
	trash := requestData[[]project.Project](t, adminClient, http.MethodGet, server.URL+"/api/projects?trash=true", nil, http.StatusOK)
	if len(trash) != 1 || trash[0].ID != created.ID {
		t.Fatalf("unexpected trash contents: %+v", trash)
	}
	requestData[project.Project](t, adminClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/restore", nil, http.StatusOK)

	requestStatus(t, adminClient, http.MethodGet, server.URL+"/health/live", nil, http.StatusOK)
	requestStatus(t, adminClient, http.MethodGet, server.URL+"/health/ready", nil, http.StatusOK)
	requestStatus(t, adminClient, http.MethodGet, server.URL+"/api/system/health", nil, http.StatusOK)
	requestStatus(t, viewerClient, http.MethodGet, server.URL+"/api/system/health", nil, http.StatusForbidden)
	if _, err := pool.Exec(ctx, `UPDATE maintenance_state SET write_locked = true, reason = 'test', owner_token = 'aaaaaaaa-1111-4111-8111-aaaaaaaaaaaa', locked_at = now(), lease_until = now() + interval '2 minutes' WHERE singleton = true`); err != nil {
		t.Fatalf("enable maintenance state: %v", err)
	}
	requestStatus(t, adminClient, http.MethodGet, server.URL+"/api/projects", nil, http.StatusOK)
	requestStatus(t, adminClient, http.MethodGet, server.URL+"/health/ready", nil, http.StatusServiceUnavailable)
	requestStatus(t, adminClient, http.MethodPost, server.URL+"/api/projects", project.CreateInput{Title: "در نگهداری", Slug: "maintenance-write"}, http.StatusServiceUnavailable)
	if _, err := pool.Exec(ctx, `UPDATE maintenance_state SET write_locked = false, reason = '', owner_token = NULL, locked_at = NULL, lease_until = NULL WHERE singleton = true`); err != nil {
		t.Fatalf("disable maintenance state: %v", err)
	}

	if err := authService.ResetPasswordCLI(ctx, viewer.Username, "viewer-password-after-reset"); err != nil {
		t.Fatalf("reset password through CLI service: %v", err)
	}
	requestStatus(t, viewerClient, http.MethodGet, server.URL+"/api/auth/me", nil, http.StatusUnauthorized)
	requestStatus(t, newHTTPClient(t), http.MethodPost, server.URL+"/api/auth/login", map[string]any{
		"identifier": viewer.Username, "password": viewerPassword,
	}, http.StatusUnauthorized)
	resetViewerClient := loginClient(t, server.URL, viewer.Username, "viewer-password-after-reset")
	requestStatus(t, resetViewerClient, http.MethodPost, server.URL+"/api/auth/logout", nil, http.StatusNoContent)
	requestStatus(t, resetViewerClient, http.MethodGet, server.URL+"/api/auth/me", nil, http.StatusUnauthorized)
	requestStatus(t, newHTTPClient(t), http.MethodPost, server.URL+"/api/auth/login", map[string]any{
		"identifier": "missing-user", "password": "wrong-password-value",
	}, http.StatusUnauthorized)

	var auditCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_log`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit entries: %v", err)
	}
	if auditCount < 12 {
		t.Fatalf("expected workflow audit trail, got %d entries", auditCount)
	}
	var unhashedSessions int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM sessions WHERE octet_length(token_hash) <> 32`).Scan(&unhashedSessions); err != nil {
		t.Fatalf("inspect session hashes: %v", err)
	}
	if unhashedSessions != 0 {
		t.Fatalf("found %d malformed session token hashes", unhashedSessions)
	}
}

func isolatedPool(t *testing.T, ctx context.Context, databaseURL string) (*pgxpool.Pool, func()) {
	t.Helper()
	rootConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	rootConfig.MaxConns = 2
	root, err := pgxpool.NewWithConfig(ctx, rootConfig)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	if err := root.Ping(ctx); err != nil {
		root.Close()
		t.Fatalf("ping test database: %v", err)
	}

	randomID, err := id.New()
	if err != nil {
		root.Close()
		t.Fatal(err)
	}
	schemaName := "forms_test_" + strings.ReplaceAll(randomID, "-", "")
	quotedSchema := pgx.Identifier{schemaName}.Sanitize()
	if _, err := root.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		root.Close()
		t.Fatalf("create isolated schema: %v", err)
	}

	testConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		root.Close()
		t.Fatal(err)
	}
	testConfig.MaxConns = 4
	testConfig.ConnConfig.RuntimeParams["search_path"] = schemaName
	testConfig.ConnConfig.RuntimeParams["timezone"] = "UTC"
	pool, err := pgxpool.NewWithConfig(ctx, testConfig)
	if err != nil {
		root.Close()
		t.Fatalf("connect to isolated schema: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		root.Close()
		t.Fatalf("ping isolated schema: %v", err)
	}

	return pool, func() {
		pool.Close()
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := root.Exec(cleanupContext, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
		root.Close()
	}
}

func newHTTPClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Timeout: 15 * time.Second}
}

func loginClient(t *testing.T, baseURL, identifier, password string) *http.Client {
	t.Helper()
	client := newHTTPClient(t)
	requestData[auth.User](t, client, http.MethodPost, baseURL+"/api/auth/login", map[string]any{
		"identifier": identifier, "password": password,
	}, http.StatusOK)
	parsed, err := url.Parse(baseURL)
	if err != nil {
		t.Fatal(err)
	}
	cookies := client.Jar.Cookies(parsed)
	if len(cookies) != 1 || cookies[0].Name != "forms_session" || cookies[0].Value == "" {
		t.Fatalf("login did not establish the expected session cookie: %+v", cookies)
	}
	return client
}

func requestData[T any](t *testing.T, client *http.Client, method, requestURL string, body any, wantStatus int) T {
	t.Helper()
	contents := request(t, client, method, requestURL, body, wantStatus)
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(contents, &envelope); err != nil {
		t.Fatalf("decode response data: %v; body=%s", err, contents)
	}
	return envelope.Data
}

func requestStatus(t *testing.T, client *http.Client, method, requestURL string, body any, wantStatus int) {
	t.Helper()
	request(t, client, method, requestURL, body, wantStatus)
}

func request(t *testing.T, client *http.Client, method, requestURL string, body any, wantStatus int) []byte {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("encode request body: %v", err)
		}
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(context.Background(), method, requestURL, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, requestURL, err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, requestURL, response.StatusCode, wantStatus, contents)
	}
	return contents
}
