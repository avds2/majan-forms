package web_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forms/internal/ai"
	"forms/internal/auth"
	"forms/internal/database"
	"forms/internal/form"
	"forms/internal/project"
	"forms/internal/projectpackage"
	"forms/internal/publicform"
	"forms/internal/response"
	formsweb "forms/internal/web"
)

func TestStageThreeResponseBrowserAndReport(t *testing.T) {
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
	location := mustLocation(t, "Asia/Tehran")
	authService, err := auth.NewService(auth.NewStore(pool), 30*time.Minute, 12*time.Hour)
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}
	admin, err := authService.CreateInitialAdmin(ctx, auth.CreateUserInput{
		Email: "phase-three-admin@example.test", Username: "phase-three-admin", DisplayName: "مدیر مرحلهٔ سوم",
		Password: adminPassword, Role: auth.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("create administrator: %v", err)
	}
	dataDir := t.TempDir()
	for _, directory := range []string{"tmp", "uploads/staging", "uploads/files"} {
		if err := os.MkdirAll(filepath.Join(dataDir, directory), 0o700); err != nil {
			t.Fatalf("create data directory %s: %v", directory, err)
		}
	}
	publicService, err := publicform.NewService(publicform.NewStore(pool), bytes.Repeat([]byte{0x53}, 32), location, dataDir, 1<<20)
	if err != nil {
		t.Fatalf("create public form service: %v", err)
	}
	responseService, err := response.NewService(response.NewStore(pool), location)
	if err != nil {
		t.Fatalf("create response service: %v", err)
	}
	projectService := project.NewService(project.NewStore(pool))
	formService := form.NewService(form.NewStore(pool))
	packageService := projectpackage.NewService(projectpackage.NewStore(pool))
	aiService := ai.NewService(ai.NewStore(pool), dataDir, responseService)
	server := httptest.NewServer(formsweb.New(formsweb.Dependencies{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)), Pool: pool, Migrator: migrator,
		Auth: authService, AI: aiService, Projects: projectService, Packages: packageService, Forms: formService, PublicForms: publicService,
		Responses: responseService, DataDir: dataDir, Timezone: "Asia/Tehran", CookieSecure: false,
		SessionAbsolute: 12 * time.Hour,
	}).Handler())
	defer server.Close()
	adminClient := loginClient(t, server.URL, admin.Username, adminPassword)

	created := requestData[project.Project](t, adminClient, http.MethodPost, server.URL+"/api/projects", project.CreateInput{
		Title: "فرم گزارش مرحلهٔ سوم", Slug: "phase-three-report",
	}, http.StatusCreated)
	draft := requestData[form.Draft](t, adminClient, http.MethodGet, server.URL+"/api/projects/"+created.ID+"/draft", nil, http.StatusOK)
	schema, ids := stageThreeSchema(t)
	saved := requestData[form.Draft](t, adminClient, http.MethodPut, server.URL+"/api/projects/"+created.ID+"/draft", map[string]any{
		"expected_revision": draft.Revision, "schema": schema,
	}, http.StatusOK)
	published := requestDataWithHeaders[form.Version](t, adminClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/publish", map[string]any{
		"expected_revision": saved.Revision,
	}, map[string]string{"Idempotency-Key": "phase-three-publish-key"}, http.StatusCreated)

	_, startToken, err := publicService.LoadLanding(ctx, created.Slug)
	if err != nil {
		t.Fatalf("load landing: %v", err)
	}
	session, sessionToken, err := publicService.Start(ctx, created.Slug, startToken, "", "", "127.0.0.1")
	if err != nil {
		t.Fatalf("start response: %v", err)
	}
	if _, err := publicService.SubmitPage(ctx, created.Slug, sessionToken, session.CurrentPageID, url.Values{
		ids.name: {"كيان رضايي"}, ids.score: {"۸.۵"}, ids.choice: {ids.choiceOne}, ids.date: {"۱۴۰۵/۰۶/۱۶"},
	}); err != nil {
		t.Fatalf("submit response page: %v", err)
	}
	submitted, _, _, err := publicService.Submit(ctx, created.Slug, sessionToken, "phase-three-response-key", "integration-test")
	if err != nil {
		t.Fatalf("complete response: %v", err)
	}
	var indexedSearch string
	if err := pool.QueryRow(ctx, `SELECT search_text FROM submissions WHERE id = $1`, submitted.ID).Scan(&indexedSearch); err != nil {
		t.Fatalf("read response search index: %v", err)
	}
	if !strings.Contains(indexedSearch, "کیان") || !strings.Contains(indexedSearch, "8.5") {
		t.Fatalf("response search index is incomplete: %q", indexedSearch)
	}

	list := requestData[response.ListResult](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/responses?search="+url.QueryEscape("کیان ۸"), nil, http.StatusOK)
	if list.Pagination.Total != 1 || len(list.Items) != 1 || list.Items[0].ID != submitted.ID || list.Items[0].SubmittedAtJalali == "" {
		t.Fatalf("unexpected searchable response list: %+v", list)
	}
	detail := requestData[response.Detail](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/responses/"+submitted.ID, nil, http.StatusOK)
	if detail.VersionNumber != 1 || detail.EndingTitle != "پایان گزارش" || len(detail.Answers) != 4 {
		t.Fatalf("unexpected response detail: %+v", detail)
	}
	if answerDisplay(detail.Answers, ids.date) != "۱۴۰۵/۰۶/۱۶" || answerDisplay(detail.Answers, ids.choice) != "گزینهٔ یک" {
		t.Fatalf("typed/Jalali response values were not rendered: %+v", detail.Answers)
	}

	report := requestData[response.Report](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/reports/summary", nil, http.StatusOK)
	if report.KPI.Starts != 1 || report.KPI.Completes != 1 || report.KPI.CompletionRate != 100 || len(report.Trend) != 1 {
		t.Fatalf("unexpected report KPI/trend: %+v", report)
	}
	if len(report.Choices) != 1 || report.Choices[0].Values[0].Label != "گزینهٔ یک" || len(report.Numbers) != 1 || report.Numbers[0].Median != 8.5 {
		t.Fatalf("unexpected typed report breakdown: %+v", report)
	}
	if len(report.PageActivity) != 1 || report.PageActivity[0].Label != "پرسش‌ها" || report.PageActivity[0].Count != 1 {
		t.Fatalf("page activity did not count the completed source page: %+v", report.PageActivity)
	}

	csvExport, csvHeader := requestExport(t, adminClient,
		server.URL+"/api/projects/"+created.ID+"/exports/responses?format=csv", http.StatusOK)
	if !bytes.HasPrefix(csvExport, []byte("\xef\xbb\xbf")) || !bytes.Contains(csvExport, []byte("شناسهٔ پاسخ")) || csvHeader.Get("X-Export-Row-Count") != "1" {
		t.Fatalf("CSV export metadata/content is invalid: header=%v body=%q", csvHeader, csvExport)
	}
	jsonExport, _ := requestExport(t, adminClient,
		server.URL+"/api/projects/"+created.ID+"/exports/responses?format=json", http.StatusOK)
	var exported struct {
		ExportVersion int `json:"export_version"`
		Responses     []struct {
			PublicID string            `json:"public_id"`
			Answers  []response.Answer `json:"answers"`
		} `json:"responses"`
	}
	if err := json.Unmarshal(jsonExport, &exported); err != nil {
		t.Fatalf("decode JSON export: %v; body=%s", err, jsonExport)
	}
	if exported.ExportVersion != 1 || len(exported.Responses) != 1 || exported.Responses[0].PublicID != submitted.PublicID {
		t.Fatalf("JSON export is incomplete: %+v", exported)
	}
	xlsxExport, _ := requestExport(t, adminClient,
		server.URL+"/api/projects/"+created.ID+"/exports/responses?format=xlsx", http.StatusOK)
	archive, err := zip.NewReader(bytes.NewReader(xlsxExport), int64(len(xlsxExport)))
	if err != nil {
		t.Fatalf("open XLSX export: %v", err)
	}
	var sheet []byte
	for _, file := range archive.File {
		if file.Name == "xl/worksheets/sheet1.xml" {
			reader, openErr := file.Open()
			if openErr != nil {
				t.Fatalf("open XLSX worksheet: %v", openErr)
			}
			sheet, err = io.ReadAll(reader)
			reader.Close()
			if err != nil {
				t.Fatalf("read XLSX worksheet: %v", err)
			}
		}
	}
	if !bytes.Contains(sheet, []byte(`rightToLeft="1"`)) || !bytes.Contains(sheet, []byte(`<v>8.5</v>`)) || !bytes.Contains(sheet, []byte("۱۴۰۵/۰۶/۱۶")) {
		t.Fatalf("XLSX does not preserve RTL, numeric type and Jalali display: %s", sheet)
	}
	printable, _ := requestExport(t, adminClient,
		server.URL+"/api/projects/"+created.ID+"/reports/print", http.StatusOK)
	if !bytes.Contains(printable, []byte(`lang="fa" dir="rtl"`)) || !bytes.Contains(printable, []byte("<svg")) ||
		!bytes.Contains(printable, []byte("/report-assets/report.css")) || bytes.Contains(printable, []byte("ZgotmplZ")) {
		t.Fatalf("printable report is not a standalone Persian/SVG document: %s", printable)
	}
	printCSS, _ := requestExport(t, adminClient, server.URL+"/report-assets/report.css", http.StatusOK)
	if !bytes.Contains(printCSS, []byte("@page")) || !bytes.Contains(printCSS, []byte("size: A4")) {
		t.Fatalf("print stylesheet does not define A4 output")
	}

	deleted := requestData[map[string]int64](t, adminClient, http.MethodPost,
		server.URL+"/api/projects/"+created.ID+"/responses/delete", map[string]any{"ids": []string{submitted.ID}}, http.StatusOK)
	if deleted["updated"] != 1 {
		t.Fatalf("response was not soft-deleted: %+v", deleted)
	}
	active := requestData[response.ListResult](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/responses", nil, http.StatusOK)
	trash := requestData[response.ListResult](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/responses?deleted=deleted", nil, http.StatusOK)
	if active.Pagination.Total != 0 || trash.Pagination.Total != 1 || trash.Items[0].DeletedAt == nil {
		t.Fatalf("soft-delete filters are inconsistent: active=%+v trash=%+v", active, trash)
	}
	restored := requestData[map[string]int64](t, adminClient, http.MethodPost,
		server.URL+"/api/projects/"+created.ID+"/responses/restore", map[string]any{"ids": []string{submitted.ID}}, http.StatusOK)
	if restored["updated"] != 1 {
		t.Fatalf("response was not restored: %+v", restored)
	}

	projectPackage, packageHeaders := requestExport(t, adminClient,
		server.URL+"/api/projects/"+created.ID+"/package?versions=all", http.StatusOK)
	if packageHeaders.Get("X-Package-Format") != projectpackage.Format || len(packageHeaders.Get("X-Package-SHA256")) != 64 {
		t.Fatalf("project package headers are incomplete: %v", packageHeaders)
	}
	packageArchive, err := zip.NewReader(bytes.NewReader(projectPackage), int64(len(projectPackage)))
	if err != nil {
		t.Fatalf("open project package: %v", err)
	}
	packageFiles := make(map[string]bool)
	for _, item := range packageArchive.File {
		packageFiles[item.Name] = true
		if strings.Contains(item.Name, "response") || strings.Contains(item.Name, "submission") || strings.Contains(item.Name, "uploads") {
			t.Fatalf("respondent data leaked into project package: %s", item.Name)
		}
	}
	if !packageFiles["manifest.json"] || !packageFiles["project.json"] || !packageFiles["draft.json"] || !packageFiles["versions/000001.json"] {
		t.Fatalf("project package is missing required files: %+v", packageFiles)
	}
	preview := requestZipData[projectpackage.Preview](t, adminClient, http.MethodPost,
		server.URL+"/api/project-packages/preview", projectPackage, http.StatusOK)
	if preview.SourceProjectID != created.ID || preview.VersionCount != 1 || preview.FieldCount != 4 || preview.IncludesResponses {
		t.Fatalf("unexpected package preview: %+v", preview)
	}
	imported := requestZipData[projectpackage.ImportResult](t, adminClient, http.MethodPost,
		server.URL+"/api/project-packages/import?title="+url.QueryEscape("نسخهٔ انتقال‌یافته")+"&slug=phase-three-imported",
		projectPackage, http.StatusCreated)
	if imported.Project.ID == created.ID || imported.Project.Slug != "phase-three-imported" || imported.Project.Status != project.StatusDraft || imported.Project.ActiveVersionID != nil || imported.ImportedVersions != 1 {
		t.Fatalf("import did not remap the project safely: %+v", imported)
	}
	importedDraft := requestData[form.Draft](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+imported.Project.ID+"/draft", nil, http.StatusOK)
	importedVersions := requestData[[]form.Version](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+imported.Project.ID+"/versions", nil, http.StatusOK)
	if importedDraft.Checksum != saved.Checksum || importedDraft.ProjectID == saved.ProjectID || len(importedVersions) != 1 || importedVersions[0].ID == published.ID {
		t.Fatalf("imported draft/version identity or checksum is incorrect: draft=%+v versions=%+v", importedDraft, importedVersions)
	}
	originalSchemaJSON, _ := json.Marshal(saved.Schema)
	importedSchemaJSON, _ := json.Marshal(importedDraft.Schema)
	if !bytes.Equal(originalSchemaJSON, importedSchemaJSON) || importedVersions[0].Checksum != saved.Checksum {
		t.Fatalf("project package semantic round-trip changed the form schema")
	}
	var importedResponses int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM submissions WHERE project_id = $1`, imported.Project.ID).Scan(&importedResponses); err != nil {
		t.Fatalf("count imported project responses: %v", err)
	}
	if importedResponses != 0 {
		t.Fatalf("project package imported %d response rows", importedResponses)
	}

	providerSettings := requestData[ai.ProviderSettings](t, adminClient, http.MethodGet,
		server.URL+"/api/ai/provider", nil, http.StatusOK)
	if providerSettings.HasAPIKey || providerSettings.Mode != ai.ModeResponses {
		t.Fatalf("unexpected default AI provider settings: %+v", providerSettings)
	}
	projectAI := requestData[ai.ProjectSettings](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/ai/settings", nil, http.StatusOK)
	if projectAI.Enabled || projectAI.TextSamplingEnabled || projectAI.ProviderConfigured {
		t.Fatalf("AI must be disabled by default: %+v", projectAI)
	}
	var providerHits atomic.Int64
	var providerOnline atomic.Bool
	providerOnline.Store(true)
	providerServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit := providerHits.Add(1)
		if !providerOnline.Load() {
			http.Error(w, "provider temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer phase-three-provider-secret" {
			t.Errorf("unexpected AI provider request: path=%s authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		requestBody, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Errorf("read AI provider request: %v", readErr)
		}
		if bytes.Contains(requestBody, []byte("کیان رضایی")) || bytes.Contains(requestBody, []byte("كيان رضايي")) {
			t.Errorf("aggregate-only AI request leaked a respondent text value: %s", requestBody)
		}
		if !bytes.Contains(requestBody, []byte(`"store":false`)) || !bytes.Contains(requestBody, []byte(`"parallel_tool_calls":false`)) {
			t.Errorf("AI request omitted privacy/tool constraints: %s", requestBody)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch {
		case hit == 1:
			_, _ = io.WriteString(w, "data: {\"type\":\"response.created\"}\n\n")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"name\":\"health_check\"}}\n\n")
		case bytes.Contains(requestBody, []byte("force_unknown_tool")):
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"item-unsafe\",\"type\":\"function_call\",\"call_id\":\"call-unsafe\",\"name\":\"read_secret_file\",\"arguments\":\"{}\"}}\n\n")
		case bytes.Contains(requestBody, []byte(`"type":"function_call_output"`)):
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"یک پاسخ نهایی در این پروژه ثبت شده است.\"}\n\n")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.completed\"}\n\n")
		default:
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"item-count\",\"type\":\"function_call\",\"call_id\":\"call-count\",\"name\":\"count_submissions\",\"arguments\":\"\"}}\n\n")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.function_call_arguments.done\",\"item_id\":\"item-count\",\"arguments\":\"{\\\"version\\\":null,\\\"from\\\":null,\\\"to\\\":null}\"}\n\n")
		}
	}))
	defer providerServer.Close()
	providerSettings = requestData[ai.ProviderSettings](t, adminClient, http.MethodPut,
		server.URL+"/api/ai/provider", ai.SaveProviderInput{
			BaseURL: providerServer.URL, Mode: ai.ModeResponses, Model: "compatible-test-model",
			TimeoutSeconds: 5, MaxOutputTokens: 512, AllowPrivateEndpoint: true,
			APIKey: "phase-three-provider-secret",
		}, http.StatusOK)
	if !providerSettings.HasAPIKey || providerHits.Load() != 0 {
		t.Fatalf("saving provider settings leaked a request or lost the secret: settings=%+v hits=%d", providerSettings, providerHits.Load())
	}
	secretInfo, err := os.Stat(filepath.Join(dataDir, "secrets", "ai-provider.key"))
	if err != nil || secretInfo.Mode().Perm() != 0o600 {
		t.Fatalf("AI provider secret file is not protected: info=%v error=%v", secretInfo, err)
	}
	compatibility := requestData[ai.Compatibility](t, adminClient, http.MethodPost,
		server.URL+"/api/ai/provider/test", nil, http.StatusOK)
	if !compatibility.Streaming || !compatibility.Tools || providerHits.Load() != 1 {
		t.Fatalf("AI provider compatibility was not proven: result=%+v hits=%d", compatibility, providerHits.Load())
	}
	projectAI = requestData[ai.ProjectSettings](t, adminClient, http.MethodPut,
		server.URL+"/api/projects/"+created.ID+"/ai/settings", ai.SaveProjectSettingsInput{Enabled: true}, http.StatusOK)
	if !projectAI.Enabled || projectAI.TextSamplingEnabled || !projectAI.ProviderConfigured {
		t.Fatalf("aggregate-only project AI settings are incorrect: %+v", projectAI)
	}
	conversation := requestData[ai.Conversation](t, adminClient, http.MethodPost,
		server.URL+"/api/projects/"+created.ID+"/ai/conversations", ai.CreateConversationInput{Title: "آمار پاسخ‌ها"}, http.StatusCreated)
	projectAI = requestData[ai.ProjectSettings](t, adminClient, http.MethodPut,
		server.URL+"/api/projects/"+created.ID+"/ai/settings", ai.SaveProjectSettingsInput{}, http.StatusOK)
	hitsBeforeDisabled := providerHits.Load()
	disabledStream, disabledHeaders := requestAIStream(t, adminClient,
		server.URL+"/api/projects/"+created.ID+"/ai/conversations/"+conversation.ID+"/messages/stream", "این درخواست نباید از سرور خارج شود")
	if providerHits.Load() != hitsBeforeDisabled || !bytes.Contains(disabledStream, []byte("ai_not_ready")) ||
		!strings.Contains(disabledHeaders.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("disabled project crossed the provider boundary: hits=%d body=%s headers=%v", providerHits.Load(), disabledStream, disabledHeaders)
	}
	projectAI = requestData[ai.ProjectSettings](t, adminClient, http.MethodPut,
		server.URL+"/api/projects/"+created.ID+"/ai/settings", ai.SaveProjectSettingsInput{Enabled: true}, http.StatusOK)
	successStream, successHeaders := requestAIStream(t, adminClient,
		server.URL+"/api/projects/"+created.ID+"/ai/conversations/"+conversation.ID+"/messages/stream", "تعداد پاسخ‌های نهایی را بگو")
	if providerHits.Load() != hitsBeforeDisabled+2 || !bytes.Contains(successStream, []byte("event: delta")) ||
		!bytes.Contains(successStream, []byte("یک پاسخ نهایی")) || !bytes.Contains(successStream, []byte("event: done")) ||
		successHeaders.Get("Cache-Control") != "no-store" || successHeaders.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("AI tool round-trip/stream is incomplete: hits=%d headers=%v body=%s", providerHits.Load(), successHeaders, successStream)
	}
	messages := requestData[[]ai.Message](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/ai/conversations/"+conversation.ID+"/messages", nil, http.StatusOK)
	if len(messages) != 2 || messages[0].Role != "user" || messages[1].Role != "assistant" ||
		!strings.Contains(messages[1].Content, "یک پاسخ نهایی") || len(messages[1].ContextSummary["tools"].([]any)) != 1 {
		t.Fatalf("AI conversation history/context is incomplete: %+v", messages)
	}
	hitsBeforeUnknown := providerHits.Load()
	unknownStream, _ := requestAIStream(t, adminClient,
		server.URL+"/api/projects/"+created.ID+"/ai/conversations/"+conversation.ID+"/messages/stream", "force_unknown_tool")
	if providerHits.Load() != hitsBeforeUnknown+1 || !bytes.Contains(unknownStream, []byte("ai_provider_error")) {
		t.Fatalf("unknown provider tool was not rejected immediately: hits=%d body=%s", providerHits.Load(), unknownStream)
	}
	messagesAfterFailure := requestData[[]ai.Message](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/ai/conversations/"+conversation.ID+"/messages", nil, http.StatusOK)
	if len(messagesAfterFailure) != len(messages) {
		t.Fatalf("failed provider turn polluted conversation history: before=%d after=%d", len(messages), len(messagesAfterFailure))
	}
	providerOnline.Store(false)
	requestStatus(t, adminClient, http.MethodPost, server.URL+"/api/ai/provider/test", nil, http.StatusBadGateway)
	providerAfterOfflineTest := requestData[ai.ProviderSettings](t, adminClient, http.MethodGet,
		server.URL+"/api/ai/provider", nil, http.StatusOK)
	projectAfterOfflineTest := requestData[ai.ProjectSettings](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/ai/settings", nil, http.StatusOK)
	if providerAfterOfflineTest.Compatible || providerAfterOfflineTest.CompatibilityTestedAt != nil ||
		!providerAfterOfflineTest.HasAPIKey || providerAfterOfflineTest.BaseURL != providerServer.URL ||
		providerAfterOfflineTest.Model != "compatible-test-model" || projectAfterOfflineTest.ProviderConfigured {
		t.Fatalf("failed provider re-test left stale readiness or changed configuration: provider=%+v project=%+v", providerAfterOfflineTest, projectAfterOfflineTest)
	}
	requestStatus(t, adminClient, http.MethodDelete,
		server.URL+"/api/projects/"+created.ID+"/ai/conversations/"+conversation.ID, nil, http.StatusNoContent)
	requestStatus(t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/ai/conversations/"+conversation.ID+"/messages", nil, http.StatusNotFound)
	var remainingMessages int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM ai_messages WHERE conversation_id = $1`, conversation.ID).Scan(&remainingMessages); err != nil || remainingMessages != 0 {
		t.Fatalf("deleting AI conversation did not cascade messages: count=%d error=%v", remainingMessages, err)
	}

	secondVersion := requestDataWithHeaders[form.Version](t, adminClient, http.MethodPost, server.URL+"/api/projects/"+created.ID+"/publish", map[string]any{
		"expected_revision": saved.Revision,
	}, map[string]string{"Idempotency-Key": "phase-three-publish-key-two"}, http.StatusCreated)
	if secondVersion.VersionNumber != 2 {
		t.Fatalf("second report version=%d want=2", secondVersion.VersionNumber)
	}
	_, secondStartToken, err := publicService.LoadLanding(ctx, created.Slug)
	if err != nil {
		t.Fatalf("load second-version landing: %v", err)
	}
	secondSession, secondSessionToken, err := publicService.Start(ctx, created.Slug, secondStartToken, "", "", "127.0.0.2")
	if err != nil {
		t.Fatalf("start second-version response: %v", err)
	}
	if _, err := publicService.SubmitPage(ctx, created.Slug, secondSessionToken, secondSession.CurrentPageID, url.Values{
		ids.name: {"رها"}, ids.score: {"۷"}, ids.choice: {ids.choiceOne}, ids.date: {"۱۴۰۵/۰۶/۱۶"},
	}); err != nil {
		t.Fatalf("submit second-version page: %v", err)
	}
	if _, _, _, err := publicService.Submit(ctx, created.Slug, secondSessionToken, "phase-three-response-key-two", "integration-test-two"); err != nil {
		t.Fatalf("complete second-version response: %v", err)
	}
	combined := requestData[response.Report](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/reports/summary", nil, http.StatusOK)
	if len(combined.Choices) != 1 || !combined.Choices[0].AcrossVersions || combined.Choices[0].Values[0].Count != 2 ||
		len(combined.Numbers) != 1 || !combined.Numbers[0].AcrossVersions || combined.Numbers[0].Count != 2 || combined.Numbers[0].Median != 7.75 {
		t.Fatalf("stable fields were not aggregated across versions: %+v", combined)
	}
	versionOneReport := requestData[response.Report](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/reports/summary?version=1", nil, http.StatusOK)
	if len(versionOneReport.Choices) != 1 || versionOneReport.Choices[0].AcrossVersions || versionOneReport.Choices[0].Values[0].Count != 1 {
		t.Fatalf("version-specific report lost its version boundary: %+v", versionOneReport)
	}
	endingReport := requestData[response.Report](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/reports/summary?ending="+ids.ending, nil, http.StatusOK)
	if !endingReport.Segmented || endingReport.KPI.Completes != 2 || endingReport.KPI.CompletionRate != 0 || len(endingReport.EndingOptions) != 1 {
		t.Fatalf("ending report filter is inconsistent: %+v", endingReport)
	}
	numberReport := requestData[response.Report](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/reports/summary?field="+ids.score+"&field_value=۷", nil, http.StatusOK)
	if !numberReport.Segmented || numberReport.KPI.Completes != 1 || len(numberReport.Numbers) != 1 ||
		numberReport.Numbers[0].Count != 1 || numberReport.Numbers[0].Average != 7 {
		t.Fatalf("numeric answer report filter is inconsistent: %+v", numberReport)
	}
	choiceReport := requestData[response.Report](t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/reports/summary?field="+ids.choice+"&field_value="+ids.choiceOne, nil, http.StatusOK)
	if choiceReport.KPI.Completes != 2 || len(choiceReport.Choices) != 1 || choiceReport.Choices[0].Values[0].Count != 2 {
		t.Fatalf("choice answer report filter is inconsistent: %+v", choiceReport)
	}
	requestStatus(t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/reports/summary?field_value=orphan", nil, http.StatusUnprocessableEntity)
	requestStatus(t, adminClient, http.MethodGet,
		server.URL+"/api/projects/"+created.ID+"/reports/summary?ending="+mustUUID(t), nil, http.StatusUnprocessableEntity)
}

type stageThreeIDs struct {
	name, score, choice, choiceOne, date, ending string
}

func stageThreeSchema(t *testing.T) (form.Schema, stageThreeIDs) {
	t.Helper()
	pageID, endingID := mustUUID(t), mustUUID(t)
	ids := stageThreeIDs{name: mustUUID(t), score: mustUUID(t), choice: mustUUID(t), choiceOne: mustUUID(t), date: mustUUID(t), ending: endingID}
	return form.Schema{
		SchemaVersion: form.SchemaVersion,
		Title:         "فرم گزارش مرحلهٔ سوم",
		Pages: []form.Page{{ID: pageID, Title: "پرسش‌ها", Fields: []form.Field{
			{ID: ids.name, Type: form.FieldShortText, Label: "نام", Required: true},
			{ID: ids.score, Type: form.FieldNumber, Label: "امتیاز", Required: true},
			{ID: ids.choice, Type: form.FieldSingleChoice, Label: "انتخاب", Required: true, Options: []form.Option{{ID: ids.choiceOne, Label: "گزینهٔ یک"}, {ID: mustUUID(t), Label: "گزینهٔ دو"}}},
			{ID: ids.date, Type: form.FieldPersianDate, Label: "تاریخ", Required: true},
		}}},
		Endings: []form.Ending{{ID: endingID, Title: "پایان گزارش"}},
		Flow: form.Flow{StartPageID: pageID, Edges: []form.FlowEdge{{
			ID: mustUUID(t), SourceID: pageID, TargetID: endingID, Priority: 1000, Fallback: true,
		}}},
		Theme:          form.Theme{Preset: "minimal", Alignment: "start"},
		PublicSettings: form.PublicSettings{MinimumFillSeconds: 0},
		EditorMetadata: []byte(`{}`),
	}, ids
}

func answerDisplay(answers []response.Answer, fieldID string) string {
	for _, answer := range answers {
		if answer.FieldID == fieldID {
			return answer.DisplayValue
		}
	}
	return ""
}

func requestExport(t *testing.T, client *http.Client, endpoint string, status int) ([]byte, http.Header) {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		t.Fatalf("create export request: %v", err)
	}
	responseValue, err := client.Do(request)
	if err != nil {
		t.Fatalf("perform export request: %v", err)
	}
	defer responseValue.Body.Close()
	body, err := io.ReadAll(responseValue.Body)
	if err != nil {
		t.Fatalf("read export response: %v", err)
	}
	if responseValue.StatusCode != status {
		t.Fatalf("export status=%d want=%d body=%s", responseValue.StatusCode, status, body)
	}
	return body, responseValue.Header.Clone()
}

func requestAIStream(t *testing.T, client *http.Client, endpoint, content string) ([]byte, http.Header) {
	t.Helper()
	encoded, err := json.Marshal(ai.SendMessageInput{Content: content})
	if err != nil {
		t.Fatalf("encode AI stream request: %v", err)
	}
	request, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("create AI stream request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "text/event-stream")
	responseValue, err := client.Do(request)
	if err != nil {
		t.Fatalf("perform AI stream request: %v", err)
	}
	defer responseValue.Body.Close()
	body, err := io.ReadAll(responseValue.Body)
	if err != nil {
		t.Fatalf("read AI stream response: %v", err)
	}
	if responseValue.StatusCode != http.StatusOK {
		t.Fatalf("AI stream status=%d body=%s", responseValue.StatusCode, body)
	}
	return body, responseValue.Header.Clone()
}

func requestZipData[T any](t *testing.T, client *http.Client, method, endpoint string, packageBytes []byte, status int) T {
	t.Helper()
	request, err := http.NewRequest(method, endpoint, bytes.NewReader(packageBytes))
	if err != nil {
		t.Fatalf("create project package request: %v", err)
	}
	request.Header.Set("Content-Type", "application/zip")
	request.Header.Set("Accept", "application/json")
	responseValue, err := client.Do(request)
	if err != nil {
		t.Fatalf("perform project package request: %v", err)
	}
	defer responseValue.Body.Close()
	body, err := io.ReadAll(responseValue.Body)
	if err != nil {
		t.Fatalf("read project package response: %v", err)
	}
	if responseValue.StatusCode != status {
		t.Fatalf("project package status=%d want=%d body=%s", responseValue.StatusCode, status, body)
	}
	var envelope struct {
		Data T `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode project package response: %v; body=%s", err, body)
	}
	return envelope.Data
}
