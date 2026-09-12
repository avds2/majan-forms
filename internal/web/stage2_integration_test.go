package web_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"forms/internal/auth"
	"forms/internal/database"
	"forms/internal/form"
	"forms/internal/id"
	"forms/internal/project"
	"forms/internal/publicform"
	formsweb "forms/internal/web"
)

const onePixelPNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="

func TestStageTwoPublishBranchBackAndSubmitWorkflow(t *testing.T) {
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
	authService, err := auth.NewService(auth.NewStore(pool), 30*time.Minute, 12*time.Hour)
	if err != nil {
		t.Fatalf("create auth service: %v", err)
	}
	admin, err := authService.CreateInitialAdmin(ctx, auth.CreateUserInput{
		Email: "phase-two-admin@example.test", Username: "phase-two-admin", DisplayName: "مدیر مرحلهٔ دوم",
		Password: adminPassword, Role: auth.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("create phase-two administrator: %v", err)
	}

	dataDir := t.TempDir()
	for _, directory := range []string{"tmp", "uploads/staging", "uploads/files"} {
		if err := os.MkdirAll(filepath.Join(dataDir, directory), 0o700); err != nil {
			t.Fatalf("create data directory %s: %v", directory, err)
		}
	}
	publicService, err := publicform.NewService(
		publicform.NewStore(pool), bytes.Repeat([]byte{0x42}, 32), mustLocation(t, "Asia/Tehran"), dataDir, 1<<20,
	)
	if err != nil {
		t.Fatalf("create public form service: %v", err)
	}
	turnstile := &testTurnstileVerifier{}
	if err := publicService.ConfigureTurnstile("phase-two-site-key", turnstile); err != nil {
		t.Fatalf("configure test turnstile: %v", err)
	}
	projectService := project.NewService(project.NewStore(pool))
	formService := form.NewService(form.NewStore(pool))
	server := httptest.NewServer(formsweb.New(formsweb.Dependencies{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Pool: pool, Migrator: migrator,
		Auth: authService, Projects: projectService, Forms: formService, PublicForms: publicService,
		DataDir: dataDir, Timezone: "Asia/Tehran", CookieSecure: false, SessionAbsolute: 12 * time.Hour,
	}).Handler())
	defer server.Close()

	adminClient := loginClient(t, server.URL, admin.Username, adminPassword)
	created := requestData[project.Project](t, adminClient, http.MethodPost, server.URL+"/api/projects", project.CreateInput{
		Title: "فرم استخدام شاخه‌ای", Slug: "employment-stage-two",
	}, http.StatusCreated)
	draft := requestData[form.Draft](t, adminClient, http.MethodGet, server.URL+"/api/projects/"+created.ID+"/draft", nil, http.StatusOK)
	testSchema, ids := employmentSchema(t)
	saved := requestData[form.Draft](t, adminClient, http.MethodPut, server.URL+"/api/projects/"+created.ID+"/draft", map[string]any{
		"expected_revision": draft.Revision, "schema": testSchema,
	}, http.StatusOK)
	validation := requestData[map[string]any](t, adminClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/publish/validate", nil, http.StatusOK)
	if validation["valid"] != true || int64(validation["revision"].(float64)) != saved.Revision {
		t.Fatalf("unexpected publish validation: %#v", validation)
	}

	publishKey := "phase-two-publication-key-0001"
	version := requestDataWithHeaders[form.Version](t, adminClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/publish", map[string]any{
		"expected_revision": saved.Revision,
	}, map[string]string{"Idempotency-Key": publishKey}, http.StatusCreated)
	repeatedVersion := requestDataWithHeaders[form.Version](t, adminClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/publish", map[string]any{
		"expected_revision": saved.Revision,
	}, map[string]string{"Idempotency-Key": publishKey}, http.StatusCreated)
	if version.ID != repeatedVersion.ID || version.VersionNumber != 1 {
		t.Fatalf("publication is not idempotent: first=%+v second=%+v", version, repeatedVersion)
	}
	versions := requestData[[]form.Version](t, adminClient, http.MethodGet, server.URL+"/api/projects/"+created.ID+"/versions", nil, http.StatusOK)
	if len(versions) != 1 || versions[0].Checksum != saved.Checksum {
		t.Fatalf("unexpected immutable version history: %+v", versions)
	}
	if _, err := pool.Exec(ctx, `UPDATE form_versions SET checksum = repeat('0', 64) WHERE id = $1`, version.ID); err == nil {
		t.Fatal("published version unexpectedly allowed an update")
	} else {
		var postgresError *pgconn.PgError
		if !errors.As(err, &postgresError) || postgresError.Code != "55000" {
			t.Fatalf("unexpected immutable-version error: %v", err)
		}
	}

	landing := requestHTML(t, adminClient, http.MethodGet, server.URL+"/f/"+created.Slug, nil, "", http.StatusOK)
	if !strings.Contains(landing, `lang="fa" dir="rtl"`) || !strings.Contains(landing, "فرم استخدام شاخه‌ای") {
		t.Fatalf("public landing is not Persian/RTL: %s", landing)
	}
	if !strings.Contains(landing, `data-sitekey="phase-two-site-key"`) || !strings.Contains(landing, "challenges.cloudflare.com/turnstile") {
		t.Fatal("Turnstile-enabled form did not render its challenge")
	}
	startToken := hiddenValue(t, landing, "start_token")
	requestHTML(t, adminClient, http.MethodPost, server.URL+"/f/"+created.Slug+"/start", url.Values{
		"start_token": {startToken}, "website": {""},
	}, "application/x-www-form-urlencoded", http.StatusUnprocessableEntity)
	pageOneHTML := requestHTML(t, adminClient, http.MethodPost, server.URL+"/f/"+created.Slug+"/start", url.Values{
		"start_token": {startToken}, "website": {""}, "cf-turnstile-response": {"verified-token"},
	}, "application/x-www-form-urlencoded", http.StatusOK)
	if turnstile.calls != 1 || turnstile.lastToken != "verified-token" || turnstile.lastIP == "" {
		t.Fatalf("Turnstile was not verified server-side: %+v", turnstile)
	}
	sessionToken := hiddenValue(t, pageOneHTML, "session_token")
	if !strings.Contains(pageOneHTML, "چند سال سابقهٔ کار دارید؟") || !strings.Contains(pageOneHTML, `<form method="post"`) {
		t.Fatalf("first public page was not server-rendered: %s", pageOneHTML)
	}

	pageTwoHTML := postMultipart(t, adminClient, server.URL+"/f/"+created.Slug+"/step", []multipartValue{
		{name: "session_token", value: sessionToken}, {name: "page_id", value: ids.pageOne},
		{name: ids.experience, value: "۸"}, {name: "_action", value: "next"},
	}, nil, http.StatusOK)
	if !strings.Contains(pageTwoHTML, "مشخصات نامزد") || !strings.Contains(pageTwoHTML, "تاریخ را به‌صورت شمسی وارد کنید") {
		t.Fatalf("senior branch did not render its Persian date page: %s", pageTwoHTML)
	}

	png, err := base64.StdEncoding.DecodeString(onePixelPNGBase64)
	if err != nil {
		t.Fatal(err)
	}
	oversized := &byteCountingReader{reader: io.MultiReader(bytes.NewReader(png), io.LimitReader(zeroReader{}, 64<<20))}
	if _, err := publicService.StageUpload(ctx, created.Slug, sessionToken, ids.pageTwo, ids.resume, "too-large.png", oversized); !errors.Is(err, publicform.ErrUploadTooLarge) {
		t.Fatalf("oversized upload was not rejected: %v", err)
	}
	if oversized.read > 1025 {
		t.Fatalf("bounded upload read %d bytes for a 1024-byte field limit", oversized.read)
	}
	if entries := directoryEntries(t, filepath.Join(dataDir, "uploads/staging")); len(entries) != 0 {
		t.Fatalf("rejected upload left staged data behind: %+v", entries)
	}

	endingHTML := postMultipart(t, adminClient, server.URL+"/f/"+created.Slug+"/step", []multipartValue{
		{name: "session_token", value: sessionToken}, {name: "page_id", value: ids.pageTwo},
		{name: ids.fullName + "_first", value: "سارا"}, {name: ids.fullName + "_last", value: "احمدی"},
		{name: ids.startDate, value: "۱۴۰۵/۰۶/۱۵"}, {name: "_action", value: "next"},
	}, []multipartFile{{name: ids.resume, filename: "رزومه.png", contents: png}}, http.StatusOK)
	if !strings.Contains(endingHTML, "دعوت به مصاحبه") {
		t.Fatalf("senior ending did not render: %s", endingHTML)
	}

	pageTwoAgain := postMultipart(t, adminClient, server.URL+"/f/"+created.Slug+"/step", []multipartValue{
		{name: "session_token", value: sessionToken}, {name: "_action", value: "back"},
	}, nil, http.StatusOK)
	if !strings.Contains(pageTwoAgain, "مشخصات نامزد") {
		t.Fatalf("back from ending skipped the final page: %s", pageTwoAgain)
	}
	if !strings.Contains(pageTwoAgain, `name="`+ids.fullName+`_first" value="سارا"`) ||
		!strings.Contains(pageTwoAgain, `name="`+ids.fullName+`_last" value="احمدی"`) ||
		!strings.Contains(pageTwoAgain, `name="`+ids.startDate+`" value="۱۴۰۵/۰۶/۱۵"`) {
		t.Fatalf("back navigation did not restore Persian values from canonical storage: %s", pageTwoAgain)
	}
	pageOneAgain := postMultipart(t, adminClient, server.URL+"/f/"+created.Slug+"/step", []multipartValue{
		{name: "session_token", value: sessionToken}, {name: "_action", value: "back"},
	}, nil, http.StatusOK)
	if !strings.Contains(pageOneAgain, "چند سال سابقهٔ کار دارید؟") {
		t.Fatalf("second back did not return to the first page: %s", pageOneAgain)
	}
	pageThreeHTML := postMultipart(t, adminClient, server.URL+"/f/"+created.Slug+"/step", []multipartValue{
		{name: "session_token", value: sessionToken}, {name: "page_id", value: ids.pageOne},
		{name: ids.experience, value: "۳"}, {name: "_action", value: "next"},
	}, nil, http.StatusOK)
	if !strings.Contains(pageThreeHTML, "توضیح تکمیلی") {
		t.Fatalf("changed answer did not select the intermediate branch: %s", pageThreeHTML)
	}
	var oldAnswerPresent, stagedFileCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM public_sessions WHERE answers ? $1`, ids.fullName).Scan(&oldAnswerPresent); err != nil {
		t.Fatalf("inspect pruned answers: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM stored_files WHERE public_session_id IS NOT NULL`).Scan(&stagedFileCount); err != nil {
		t.Fatalf("inspect pruned staged files: %v", err)
	}
	if oldAnswerPresent != 0 || stagedFileCount != 0 || len(directoryEntries(t, filepath.Join(dataDir, "uploads/staging"))) != 0 {
		t.Fatalf("old branch was not pruned: answer=%d files=%d", oldAnswerPresent, stagedFileCount)
	}

	rejectedEndingHTML := postMultipart(t, adminClient, server.URL+"/f/"+created.Slug+"/step", []multipartValue{
		{name: "session_token", value: sessionToken}, {name: "page_id", value: ids.pageThree},
		{name: ids.note, value: "برای موقعیت کارآموزی آماده‌ام."}, {name: "_action", value: "next"},
	}, nil, http.StatusOK)
	if !strings.Contains(rejectedEndingHTML, "ثبت در فهرست آینده") {
		t.Fatalf("intermediate branch ending did not render: %s", rejectedEndingHTML)
	}
	submitKey := hiddenValue(t, rejectedEndingHTML, "idempotency_key")
	successHTML := requestHTML(t, adminClient, http.MethodPost, server.URL+"/f/"+created.Slug+"/submit", url.Values{
		"session_token": {sessionToken}, "idempotency_key": {submitKey},
	}, "application/x-www-form-urlencoded", http.StatusOK)
	repeatedSuccess := requestHTML(t, adminClient, http.MethodPost, server.URL+"/f/"+created.Slug+"/submit", url.Values{
		"session_token": {sessionToken}, "idempotency_key": {submitKey},
	}, "application/x-www-form-urlencoded", http.StatusOK)
	if !strings.Contains(successHTML, "پاسخ شما ثبت شد") || successHTML != repeatedSuccess {
		t.Fatal("final submission retry did not return the same successful receipt")
	}
	var submissions, typedNumbers, typedTexts, prunedAnswers int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM submissions WHERE project_id = $1`, created.ID).Scan(&submissions); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM submission_answers WHERE field_id = $1 AND value_number = 3`, ids.experience).Scan(&typedNumbers); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM submission_answers WHERE field_id = $1 AND value_text IS NOT NULL`, ids.note).Scan(&typedTexts); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM submission_answers WHERE field_id = ANY($1::uuid[])`, []string{ids.fullName, ids.startDate}).Scan(&prunedAnswers); err != nil {
		t.Fatal(err)
	}
	if submissions != 1 || typedNumbers != 1 || typedTexts != 1 || prunedAnswers != 0 {
		t.Fatalf("unexpected authoritative submission: submissions=%d numbers=%d texts=%d pruned=%d", submissions, typedNumbers, typedTexts, prunedAnswers)
	}
	var pageOneSteps, pageTwoSteps, pageThreeSteps, endingSteps int
	if err := pool.QueryRow(ctx, `SELECT coalesce(sum(steps), 0) FROM analytics_daily WHERE project_id = $1 AND page_id = $2`, created.ID, ids.pageOne).Scan(&pageOneSteps); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT coalesce(sum(steps), 0) FROM analytics_daily WHERE project_id = $1 AND page_id = $2`, created.ID, ids.pageTwo).Scan(&pageTwoSteps); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT coalesce(sum(steps), 0) FROM analytics_daily WHERE project_id = $1 AND page_id = $2`, created.ID, ids.pageThree).Scan(&pageThreeSteps); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT coalesce(sum(steps), 0) FROM analytics_daily WHERE project_id = $1 AND page_id = ANY($2::uuid[])`, created.ID, []string{ids.seniorEnding, ids.juniorEnding}).Scan(&endingSteps); err != nil {
		t.Fatal(err)
	}
	if pageOneSteps != 2 || pageTwoSteps != 1 || pageThreeSteps != 1 || endingSteps != 0 {
		t.Fatalf("page activity counted arrivals/backtracking instead of completed pages: page1=%d page2=%d page3=%d endings=%d", pageOneSteps, pageTwoSteps, pageThreeSteps, endingSteps)
	}

	requestData[project.Project](t, adminClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/close", nil, http.StatusOK)
	secondVersion := requestDataWithHeaders[form.Version](t, adminClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/publish", map[string]any{
		"expected_revision": saved.Revision,
	}, map[string]string{"Idempotency-Key": "phase-two-publication-key-0002"}, http.StatusCreated)
	if secondVersion.VersionNumber != 2 {
		t.Fatalf("second publication version=%d want=2", secondVersion.VersionNumber)
	}
	stillClosed := requestData[project.Project](t, adminClient, http.MethodGet, server.URL+"/api/projects/"+created.ID, nil, http.StatusOK)
	if stillClosed.Status != project.StatusClosed {
		t.Fatalf("publishing silently reopened a closed project: %+v", stillClosed)
	}
	requestStatus(t, adminClient, http.MethodPatch, server.URL+"/api/projects/"+created.ID, project.UpdateInput{
		Title: created.Title, Slug: "changed-after-publish",
	}, http.StatusConflict)
	requestData[project.Project](t, adminClient, http.MethodDelete, server.URL+"/api/projects/"+created.ID, nil, http.StatusOK)
	if hiddenVersions := requestData[[]form.Version](t, adminClient, http.MethodGet, server.URL+"/api/projects/"+created.ID+"/versions", nil, http.StatusOK); len(hiddenVersions) != 0 {
		t.Fatalf("trashed project leaked version history: %+v", hiddenVersions)
	}
	restoredClosed := requestData[project.Project](t, adminClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/restore", nil, http.StatusOK)
	if restoredClosed.Status != project.StatusClosed {
		t.Fatalf("restored project lost its pre-trash closed state: %+v", restoredClosed)
	}
	closedLanding := requestHTML(t, adminClient, http.MethodGet, server.URL+"/f/"+created.Slug, nil, "", http.StatusOK)
	if !strings.Contains(closedLanding, "پذیرش پاسخ متوقف است") {
		t.Fatalf("closed public form did not show its closed state: %s", closedLanding)
	}
	requestHTML(t, adminClient, http.MethodPost, server.URL+"/f/"+created.Slug+"/start", url.Values{
		"start_token": {startToken},
	}, "application/x-www-form-urlencoded", http.StatusConflict)
	requestData[project.Project](t, adminClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/reopen", nil, http.StatusOK)

	_, abandonedStartToken, err := publicService.LoadLanding(ctx, created.Slug)
	if err != nil {
		t.Fatalf("load form for trash-session test: %v", err)
	}
	_, abandonedSessionToken, err := publicService.Start(ctx, created.Slug, abandonedStartToken, "", "verified-token", "127.0.0.1")
	if err != nil {
		t.Fatalf("start trash-session test: %v", err)
	}
	requestData[project.Project](t, adminClient, http.MethodDelete, server.URL+"/api/projects/"+created.ID, nil, http.StatusOK)
	if _, err := publicService.Resume(ctx, created.Slug, abandonedSessionToken); !errors.Is(err, publicform.ErrInvalidSession) {
		t.Fatalf("trashed project kept an existing public session usable: %v", err)
	}
	restoredPublished := requestData[project.Project](t, adminClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/restore", nil, http.StatusOK)
	if restoredPublished.Status != project.StatusPublished {
		t.Fatalf("restored project lost its pre-trash published state: %+v", restoredPublished)
	}

	_, downloadStartToken, err := publicService.LoadLanding(ctx, created.Slug)
	if err != nil {
		t.Fatalf("load form for finalized download test: %v", err)
	}
	_, downloadSessionToken, err := publicService.Start(ctx, created.Slug, downloadStartToken, "", "verified-token", "127.0.0.1")
	if err != nil {
		t.Fatalf("start finalized download test: %v", err)
	}
	if _, err := publicService.SubmitPage(ctx, created.Slug, downloadSessionToken, ids.pageOne, url.Values{ids.experience: {"8"}}); err != nil {
		t.Fatalf("navigate finalized download test: %v", err)
	}
	finalizedFile, err := publicService.StageUpload(ctx, created.Slug, downloadSessionToken, ids.pageTwo, ids.resume, "رزومه.png", bytes.NewReader(png))
	if err != nil {
		t.Fatalf("stage finalized download test file: %v", err)
	}
	if _, err := publicService.SubmitPage(ctx, created.Slug, downloadSessionToken, ids.pageTwo, url.Values{
		ids.fullName + "_first": {"مریم"}, ids.fullName + "_last": {"رضایی"}, ids.startDate: {"۱۴۰۵/۰۶/۱۶"},
	}); err != nil {
		t.Fatalf("reach ending for finalized download test: %v", err)
	}
	if _, _, _, err := publicService.Submit(ctx, created.Slug, downloadSessionToken, "phase-two-file-submit-key", "integration-test"); err != nil {
		t.Fatalf("finalize download test submission: %v", err)
	}
	unauthenticatedResponse, err := http.Get(server.URL + "/api/files/" + finalizedFile.ID)
	if err != nil {
		t.Fatalf("request file without authentication: %v", err)
	}
	unauthenticatedResponse.Body.Close()
	if unauthenticatedResponse.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated file download status=%d want=%d", unauthenticatedResponse.StatusCode, http.StatusUnauthorized)
	}
	downloadResponse, err := adminClient.Get(server.URL + "/api/files/" + finalizedFile.ID)
	if err != nil {
		t.Fatalf("download finalized file: %v", err)
	}
	downloaded, readErr := io.ReadAll(downloadResponse.Body)
	downloadResponse.Body.Close()
	if readErr != nil {
		t.Fatalf("read finalized file: %v", readErr)
	}
	if downloadResponse.StatusCode != http.StatusOK || !bytes.Equal(downloaded, png) ||
		!strings.Contains(downloadResponse.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("unexpected finalized download: status=%d disposition=%q bytes=%d", downloadResponse.StatusCode, downloadResponse.Header.Get("Content-Disposition"), len(downloaded))
	}

	_, concurrencyStartToken, err := publicService.LoadLanding(ctx, created.Slug)
	if err != nil {
		t.Fatalf("load form for concurrency test: %v", err)
	}
	concurrencySession, concurrencyToken, err := publicService.Start(ctx, created.Slug, concurrencyStartToken, "", "verified-token", "127.0.0.1")
	if err != nil {
		t.Fatalf("start concurrency test: %v", err)
	}
	tokenHash := sha256.Sum256([]byte(concurrencyToken))
	publicStore := publicform.NewStore(pool)
	firstCopy, err := publicStore.GetSession(ctx, created.Slug, tokenHash[:], time.Now().UTC())
	if err != nil {
		t.Fatalf("load first concurrent session copy: %v", err)
	}
	staleCopy, err := publicStore.GetSession(ctx, created.Slug, tokenHash[:], time.Now().UTC())
	if err != nil {
		t.Fatalf("load stale concurrent session copy: %v", err)
	}
	firstCopy.Answers[ids.experience] = float64(8)
	if err := publicStore.UpdateSession(ctx, &firstCopy, "", time.Now().In(mustLocation(t, "Asia/Tehran")).Format(time.DateOnly)); err != nil {
		t.Fatalf("update first concurrent session copy: %v", err)
	}
	staleCopy.Answers[ids.experience] = float64(3)
	if err := publicStore.UpdateSession(ctx, &staleCopy, "", time.Now().In(mustLocation(t, "Asia/Tehran")).Format(time.DateOnly)); !errors.Is(err, publicform.ErrPathChanged) {
		t.Fatalf("stale concurrent update was not rejected: %v", err)
	}
	if firstCopy.Revision != concurrencySession.Revision+1 {
		t.Fatalf("public session revision did not advance: started=%d updated=%d", concurrencySession.Revision, firstCopy.Revision)
	}

	_, cleanupStartToken, err := publicService.LoadLanding(ctx, created.Slug)
	if err != nil {
		t.Fatalf("load form for cleanup test: %v", err)
	}
	cleanupSession, cleanupToken, err := publicService.Start(ctx, created.Slug, cleanupStartToken, "", "verified-token", "127.0.0.1")
	if err != nil {
		t.Fatalf("start form for cleanup test: %v", err)
	}
	if _, err := publicService.SubmitPage(ctx, created.Slug, cleanupToken, ids.pageOne, url.Values{ids.experience: {"8"}}); err != nil {
		t.Fatalf("navigate cleanup session to upload page: %v", err)
	}
	if _, err := publicService.StageUpload(ctx, created.Slug, cleanupToken, ids.pageTwo, ids.resume, "expired.png", bytes.NewReader(png)); err != nil {
		t.Fatalf("stage cleanup test file: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE public_sessions SET expires_at = now() - interval '1 minute' WHERE id = $1`, cleanupSession.ID); err != nil {
		t.Fatalf("expire cleanup test session: %v", err)
	}
	report, err := publicService.CleanupTemporary(ctx)
	if err != nil {
		t.Fatalf("clean public temporary data: %v", err)
	}
	if report.ExpiredSessions != 1 || report.RemovedStagedFiles != 1 || len(directoryEntries(t, filepath.Join(dataDir, "uploads/staging"))) != 0 {
		t.Fatalf("unexpected cleanup report: %+v", report)
	}
}

func TestStageTwoRegistrationAndSatisfactionNoJavaScriptWorkflow(t *testing.T) {
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
	authService, err := auth.NewService(auth.NewStore(pool), 30*time.Minute, 12*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := authService.CreateInitialAdmin(ctx, auth.CreateUserInput{
		Email: "phase-two-samples@example.test", Username: "phase-two-samples", DisplayName: "مدیر نمونه‌ها",
		Password: adminPassword, Role: auth.RoleAdmin,
	})
	if err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	for _, directory := range []string{"tmp", "uploads/staging", "uploads/files"} {
		if err := os.MkdirAll(filepath.Join(dataDir, directory), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	publicService, err := publicform.NewService(publicform.NewStore(pool), bytes.Repeat([]byte{0x24}, 32), mustLocation(t, "Asia/Tehran"), dataDir, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(formsweb.New(formsweb.Dependencies{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Pool: pool, Migrator: migrator,
		Auth: authService, Projects: project.NewService(project.NewStore(pool)), Forms: form.NewService(form.NewStore(pool)), PublicForms: publicService,
		DataDir: dataDir, Timezone: "Asia/Tehran", CookieSecure: false, SessionAbsolute: 12 * time.Hour,
	}).Handler())
	defer server.Close()
	adminClient := loginClient(t, server.URL, admin.Username, adminPassword)
	publicClient := &http.Client{Timeout: 10 * time.Second}

	for _, sample := range stageTwoPublicSamples(t) {
		t.Run(sample.slug, func(t *testing.T) {
			created := requestData[project.Project](t, adminClient, http.MethodPost, server.URL+"/api/projects", project.CreateInput{Title: sample.schema.Title, Slug: sample.slug}, http.StatusCreated)
			draft := requestData[form.Draft](t, adminClient, http.MethodGet, server.URL+"/api/projects/"+created.ID+"/draft", nil, http.StatusOK)
			saved := requestData[form.Draft](t, adminClient, http.MethodPut, server.URL+"/api/projects/"+created.ID+"/draft", map[string]any{
				"expected_revision": draft.Revision, "schema": sample.schema,
			}, http.StatusOK)
			requestDataWithHeaders[form.Version](t, adminClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/publish", map[string]any{
				"expected_revision": saved.Revision,
			}, map[string]string{"Idempotency-Key": "publish-" + sample.slug + "-0001"}, http.StatusCreated)

			landing := requestHTML(t, publicClient, http.MethodGet, server.URL+"/f/"+sample.slug, nil, "", http.StatusOK)
			startToken := hiddenValue(t, landing, "start_token")
			page := requestHTML(t, publicClient, http.MethodPost, server.URL+"/f/"+sample.slug+"/start", url.Values{
				"start_token": {startToken}, "website": {""},
			}, "application/x-www-form-urlencoded", http.StatusOK)
			if !strings.Contains(page, sample.pageMarker) || !strings.Contains(page, `<form method="post"`) {
				t.Fatalf("sample page is not server-rendered: %s", page)
			}
			if sample.hasPersianDateFallback && (!strings.Contains(page, "تاریخ را به‌صورت شمسی وارد کنید") || !strings.Contains(page, `inputmode="numeric"`)) {
				t.Fatalf("Persian date text fallback is missing: %s", page)
			}
			values := []multipartValue{{name: "session_token", value: hiddenValue(t, page, "session_token")}, {name: "page_id", value: sample.pageID}}
			values = append(values, sample.values...)
			values = append(values, multipartValue{name: "_action", value: "next"})
			ending := postMultipart(t, publicClient, server.URL+"/f/"+sample.slug+"/step", values, nil, http.StatusOK)
			if !strings.Contains(ending, sample.endingMarker) {
				t.Fatalf("sample ending was not reached: %s", ending)
			}
			sessionToken := hiddenValue(t, ending, "session_token")
			submitKey := hiddenValue(t, ending, "idempotency_key")
			if sample.enforcesMinimumFill {
				tooFast := requestHTML(t, publicClient, http.MethodPost, server.URL+"/f/"+sample.slug+"/submit", url.Values{
					"session_token": {sessionToken}, "idempotency_key": {submitKey},
				}, "application/x-www-form-urlencoded", http.StatusUnprocessableEntity)
				if !strings.Contains(tooFast, publicform.ErrTooFast.Error()) {
					t.Fatalf("minimum completion time was not enforced: %s", tooFast)
				}
				if _, err := pool.Exec(ctx, `UPDATE public_sessions SET started_at = now() - interval '2 hours' WHERE project_id = $1`, created.ID); err != nil {
					t.Fatalf("age sample session: %v", err)
				}
			}
			success := requestHTML(t, publicClient, http.MethodPost, server.URL+"/f/"+sample.slug+"/submit", url.Values{
				"session_token": {sessionToken}, "idempotency_key": {submitKey},
			}, "application/x-www-form-urlencoded", http.StatusOK)
			if !strings.Contains(success, "پاسخ شما ثبت شد") || !strings.Contains(success, "theme-"+sample.schema.Theme.Preset) {
				t.Fatalf("sample did not preserve its theme on success: %s", success)
			}
			var submissionCount int
			if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM submissions WHERE project_id = $1`, created.ID).Scan(&submissionCount); err != nil || submissionCount != 1 {
				t.Fatalf("sample submission count=%d err=%v", submissionCount, err)
			}
			var finalizedFiles int
			if err := pool.QueryRow(ctx, `
				SELECT COUNT(*)
				FROM stored_files AS sf
				JOIN submissions AS s ON s.id = sf.submission_id
				WHERE s.project_id = $1 AND sf.status = 'finalized'
			`, created.ID).Scan(&finalizedFiles); err != nil || finalizedFiles != sample.expectedFinalizedFiles {
				t.Fatalf("sample finalized file count=%d want=%d err=%v", finalizedFiles, sample.expectedFinalizedFiles, err)
			}
		})
	}
}

type stageTwoPublicSample struct {
	slug, pageID, pageMarker, endingMarker string
	schema                                 form.Schema
	values                                 []multipartValue
	hasPersianDateFallback                 bool
	enforcesMinimumFill                    bool
	expectedFinalizedFiles                 int
}

func stageTwoPublicSamples(t *testing.T) []stageTwoPublicSample {
	t.Helper()
	registrationPage, registrationEnding := mustUUID(t), mustUUID(t)
	nameID, emailID, dateID, consentID, signatureID := mustUUID(t), mustUUID(t), mustUUID(t), mustUUID(t), mustUUID(t)
	registration := stageTwoPublicSample{
		slug: "event-registration-stage-two", pageID: registrationPage, pageMarker: "نام‌نویسی رویداد", endingMarker: "ثبت‌نام تکمیل شد", hasPersianDateFallback: true, expectedFinalizedFiles: 1,
		values: []multipartValue{
			{name: nameID + "_first", value: "علی"}, {name: nameID + "_last", value: "محمدی"},
			{name: emailID, value: "ali@example.test"}, {name: dateID, value: "۱۴۰۵/۰۶/۱۵"}, {name: consentID, value: "accepted"},
			{name: signatureID, value: "data:image/png;base64," + onePixelPNGBase64},
		},
		schema: onePageSampleSchema(t, "فرم ثبت‌نام رویداد", registrationPage, registrationEnding, "warm", []form.Field{
			{ID: nameID, Type: form.FieldFullName, Label: "نام شرکت‌کننده", Required: true},
			{ID: emailID, Type: form.FieldEmail, Label: "ایمیل", Required: true},
			{ID: dateID, Type: form.FieldPersianDate, Label: "تاریخ حضور", Required: true},
			{ID: consentID, Type: form.FieldConsent, Label: "رضایت", Required: true, Consent: &form.ConsentRule{AgreementText: "شرایط حضور را می‌پذیرم."}},
			{ID: signatureID, Type: form.FieldSignature, Label: "امضا", Required: true},
		}, "نام‌نویسی رویداد", "ثبت‌نام تکمیل شد"),
	}

	satisfactionPage, satisfactionEnding := mustUUID(t), mustUUID(t)
	npsID, matrixID := mustUUID(t), mustUUID(t)
	rowOne, rowTwo, columnOne, columnTwo := mustUUID(t), mustUUID(t), mustUUID(t), mustUUID(t)
	satisfaction := stageTwoPublicSample{
		slug: "satisfaction-stage-two", pageID: satisfactionPage, pageMarker: "ارزیابی تجربه", endingMarker: "سپاس از بازخورد شما", enforcesMinimumFill: true,
		values: []multipartValue{{name: npsID, value: "۹"}, {name: rowOne, value: columnTwo}, {name: rowTwo, value: columnOne}},
		schema: onePageSampleSchema(t, "فرم رضایت‌سنجی", satisfactionPage, satisfactionEnding, "minimal", []form.Field{
			{ID: npsID, Type: form.FieldNPS, Label: "احتمال معرفی ما", Required: true, Scale: &form.ScaleRule{Minimum: 0, Maximum: 10}},
			{ID: matrixID, Type: form.FieldMatrix, Label: "کیفیت بخش‌ها", Required: true, Matrix: &form.MatrixRule{
				Rows:    []form.Option{{ID: rowOne, Label: "پشتیبانی"}, {ID: rowTwo, Label: "رابط کاربری"}},
				Columns: []form.Option{{ID: columnOne, Label: "خوب"}, {ID: columnTwo, Label: "عالی"}},
			}},
		}, "ارزیابی تجربه", "سپاس از بازخورد شما"),
	}
	satisfaction.schema.PublicSettings.MinimumFillSeconds = 3600
	return []stageTwoPublicSample{registration, satisfaction}
}

func onePageSampleSchema(t *testing.T, title, pageID, endingID, theme string, fields []form.Field, pageTitle, endingTitle string) form.Schema {
	t.Helper()
	return form.Schema{
		SchemaVersion: form.SchemaVersion, Title: title,
		Pages:   []form.Page{{ID: pageID, Title: pageTitle, Fields: fields}},
		Endings: []form.Ending{{ID: endingID, Title: endingTitle, Message: "پاسخ با موفقیت دریافت شد."}},
		Flow:    form.Flow{StartPageID: pageID, Edges: []form.FlowEdge{{ID: mustUUID(t), SourceID: pageID, TargetID: endingID, Priority: 1000, Fallback: true}}},
		Theme:   form.Theme{Preset: theme, Alignment: "start"}, PublicSettings: form.PublicSettings{MinimumFillSeconds: 0}, EditorMetadata: json.RawMessage(`{}`),
	}
}

type employmentIDs struct {
	pageOne, pageTwo, pageThree string
	experience, fullName        string
	startDate, resume, note     string
	seniorEnding, juniorEnding  string
}

func employmentSchema(t *testing.T) (form.Schema, employmentIDs) {
	t.Helper()
	ids := employmentIDs{
		pageOne: mustUUID(t), pageTwo: mustUUID(t), pageThree: mustUUID(t),
		experience: mustUUID(t), fullName: mustUUID(t), startDate: mustUUID(t), resume: mustUUID(t), note: mustUUID(t),
	}
	hiredEnding, futureEnding := mustUUID(t), mustUUID(t)
	ids.seniorEnding, ids.juniorEnding = hiredEnding, futureEnding
	return form.Schema{
		SchemaVersion: form.SchemaVersion,
		Title:         "فرم استخدام شاخه‌ای",
		Pages: []form.Page{
			{ID: ids.pageOne, Title: "ارزیابی اولیه", Fields: []form.Field{
				{ID: ids.experience, Type: form.FieldNumber, Label: "چند سال سابقهٔ کار دارید؟", Required: true, Number: &form.NumberRule{Minimum: float64Pointer(0), Maximum: float64Pointer(60)}},
			}},
			{ID: ids.pageTwo, Title: "مشخصات نامزد", Fields: []form.Field{
				{ID: ids.fullName, Type: form.FieldFullName, Label: "نام و نام خانوادگی", Required: true},
				{ID: ids.startDate, Type: form.FieldPersianDate, Label: "تاریخ پیشنهادی شروع", Required: true},
				{ID: ids.resume, Type: form.FieldFile, Label: "رزومه", Required: true, File: &form.FileRule{MaximumBytes: 1024, MaximumFiles: 1, Extensions: []string{"png"}}},
			}},
			{ID: ids.pageThree, Title: "توضیح تکمیلی", Fields: []form.Field{
				{ID: ids.note, Type: form.FieldLongText, Label: "دربارهٔ مسیر مدنظر بنویسید", Required: true, Text: &form.TextRule{MinimumLength: 3, MaximumLength: 1000}},
			}},
		},
		Endings: []form.Ending{
			{ID: hiredEnding, Title: "دعوت به مصاحبه", Message: "همکاران ما با شما تماس می‌گیرند."},
			{ID: futureEnding, Title: "ثبت در فهرست آینده", Message: "اطلاعات شما برای فرصت‌های بعدی نگه‌داری می‌شود."},
		},
		Flow: form.Flow{StartPageID: ids.pageOne, Edges: []form.FlowEdge{
			{ID: mustUUID(t), SourceID: ids.pageOne, TargetID: ids.pageTwo, Priority: 10, Condition: &form.ConditionGroup{Match: "all", Conditions: []form.Condition{{FieldID: ids.experience, Operator: "greater_than", Value: 5}}}},
			{ID: mustUUID(t), SourceID: ids.pageOne, TargetID: ids.pageThree, Priority: 20, Condition: &form.ConditionGroup{Match: "all", Conditions: []form.Condition{{FieldID: ids.experience, Operator: "between", Value: []any{2, 5}}}}},
			{ID: mustUUID(t), SourceID: ids.pageOne, TargetID: futureEnding, Priority: 1000, Fallback: true},
			{ID: mustUUID(t), SourceID: ids.pageTwo, TargetID: hiredEnding, Priority: 1000, Fallback: true},
			{ID: mustUUID(t), SourceID: ids.pageThree, TargetID: futureEnding, Priority: 1000, Fallback: true},
		}},
		Theme:          form.Theme{Preset: "formal", PrimaryColor: "#1f5f99", Alignment: "start"},
		PublicSettings: form.PublicSettings{MinimumFillSeconds: 0, TurnstileEnabled: true},
		EditorMetadata: json.RawMessage(`{}`),
	}, ids
}

type multipartValue struct{ name, value string }
type multipartFile struct {
	name, filename string
	contents       []byte
}

type byteCountingReader struct {
	reader io.Reader
	read   int64
}

func (r *byteCountingReader) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	r.read += int64(count)
	return count, err
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	clear(buffer)
	return len(buffer), nil
}

func postMultipart(t *testing.T, client *http.Client, requestURL string, values []multipartValue, files []multipartFile, wantStatus int) string {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, value := range values {
		if err := writer.WriteField(value.name, value.value); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range files {
		part, err := writer.CreateFormFile(file.name, file.filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.contents); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return requestHTML(t, client, http.MethodPost, requestURL, body.Bytes(), writer.FormDataContentType(), wantStatus)
}

func requestHTML(t *testing.T, client *http.Client, method, requestURL string, body any, contentType string, wantStatus int) string {
	t.Helper()
	var reader io.Reader
	switch typed := body.(type) {
	case nil:
	case url.Values:
		reader = strings.NewReader(typed.Encode())
	case []byte:
		reader = bytes.NewReader(typed)
	default:
		t.Fatalf("unsupported HTML request body %T", body)
	}
	request, err := http.NewRequestWithContext(context.Background(), method, requestURL, reader)
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, requestURL, response.StatusCode, wantStatus, contents)
	}
	return string(contents)
}

func requestDataWithHeaders[T any](t *testing.T, client *http.Client, method, requestURL string, body any, headers map[string]string, wantStatus int) T {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequestWithContext(context.Background(), method, requestURL, bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	contents, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s: status=%d want=%d body=%s", method, requestURL, response.StatusCode, wantStatus, contents)
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(contents, &envelope); err != nil {
		t.Fatalf("decode response: %v body=%s", err, contents)
	}
	return envelope.Data
}

func hiddenValue(t *testing.T, page, name string) string {
	t.Helper()
	pattern := regexp.MustCompile(`name="` + regexp.QuoteMeta(name) + `" value="([^"]+)"`)
	match := pattern.FindStringSubmatch(page)
	if len(match) != 2 {
		t.Fatalf("hidden field %q was not found", name)
	}
	return match[1]
}

func directoryEntries(t *testing.T, directory string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func mustUUID(t *testing.T) string {
	t.Helper()
	value, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func mustLocation(t *testing.T, name string) *time.Location {
	t.Helper()
	location, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return location
}

func float64Pointer(value float64) *float64 { return &value }

type testTurnstileVerifier struct {
	calls     int
	lastToken string
	lastIP    string
}

func (v *testTurnstileVerifier) Verify(_ context.Context, token, remoteIP string) (bool, error) {
	v.calls++
	v.lastToken, v.lastIP = token, remoteIP
	return token == "verified-token", nil
}
