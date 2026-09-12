package web_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"forms/internal/auth"
	"forms/internal/database"
	"forms/internal/form"
	"forms/internal/project"
	"forms/internal/response"
)

const stageThreeBenchmarkRows = 100_000

func TestStageThreeLargeDatasetBenchmark(t *testing.T) {
	databaseURL := strings.TrimSpace(os.Getenv("FORMS_TEST_DATABASE_URL"))
	if databaseURL == "" {
		t.Skip("FORMS_TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool, cleanup := isolatedPool(t, ctx, databaseURL)
	defer cleanup()
	if _, err := database.NewMigrator(pool).Up(ctx); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	authService, err := auth.NewService(auth.NewStore(pool), 30*time.Minute, 12*time.Hour)
	if err != nil {
		t.Fatalf("create authentication service: %v", err)
	}
	actor, err := authService.CreateInitialAdmin(ctx, auth.CreateUserInput{
		Email: "benchmark@example.test", Username: "stage-three-benchmark", DisplayName: "معیار کارایی",
		Password: adminPassword, Role: auth.RoleAdmin,
	})
	if err != nil {
		t.Fatalf("create benchmark administrator: %v", err)
	}
	projectService := project.NewService(project.NewStore(pool))
	formService := form.NewService(form.NewStore(pool))
	created, err := projectService.Create(ctx, actor, project.CreateInput{Title: "دادهٔ صد هزار پاسخ", Slug: "stage-three-benchmark"})
	if err != nil {
		t.Fatalf("create benchmark project: %v", err)
	}
	draft, err := formService.GetDraft(ctx, actor, created.ID)
	if err != nil {
		t.Fatalf("load benchmark draft: %v", err)
	}
	schema, ids := stageThreeSchema(t)
	saved, err := formService.SaveDraft(ctx, actor, created.ID, draft.Revision, schema)
	if err != nil {
		t.Fatalf("save benchmark schema: %v", err)
	}
	version, err := formService.Publish(ctx, actor, created.ID, saved.Revision, "stage-three-benchmark-publish")
	if err != nil {
		t.Fatalf("publish benchmark schema: %v", err)
	}

	seedStarted := time.Now()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin benchmark seed: %v", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	pageID, endingID := schema.Pages[0].ID, schema.Endings[0].ID
	_, err = tx.Exec(ctx, `
		INSERT INTO public_sessions(
			id, token_hash, project_id, version_id, answers, history, current_page_id,
			started_at, last_seen_at, expires_at, submitted_at
		)
		SELECT ('10000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid,
		       decode(md5('benchmark-session-' || g), 'hex'), $1, $2, '{}'::jsonb, '[]'::jsonb, $3,
		       moment - interval '90 seconds', moment, moment + interval '1 hour', moment
		FROM (
			SELECT g, timestamptz '2026-01-01 00:00:00+00'
			       + ((g - 1) % 365) * interval '1 day' + (g % 86400) * interval '1 second' AS moment
			FROM generate_series(1, $4) AS g
		) generated`, created.ID, version.ID, pageID, stageThreeBenchmarkRows)
	if err != nil {
		t.Fatalf("seed public sessions: %v", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO submissions(
			id, public_id, public_session_id, project_id, version_id, ending_id,
			idempotency_key_hash, started_at, submitted_at, search_text
		)
		SELECT ('20000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid,
		       ('30000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid,
		       ('10000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid,
		       $1, $2, $3, decode(md5('benchmark-submit-' || g), 'hex'),
		       moment - interval '90 seconds', moment, 'benchmark ' || g
		FROM (
			SELECT g, timestamptz '2026-01-01 00:00:00+00'
			       + ((g - 1) % 365) * interval '1 day' + (g % 86400) * interval '1 second' AS moment
			FROM generate_series(1, $4) AS g
		) generated`, created.ID, version.ID, endingID, stageThreeBenchmarkRows)
	if err != nil {
		t.Fatalf("seed submissions: %v", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO submission_answers(submission_id, field_id, field_type, value_text)
		SELECT ('20000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid,
		       $1, 'single_choice', CASE WHEN g % 3 = 0 THEN $2 ELSE $3 END
		FROM generate_series(1, $4) AS g`, ids.choice, ids.choiceOne, schema.Pages[0].Fields[2].Options[1].ID, stageThreeBenchmarkRows)
	if err != nil {
		t.Fatalf("seed choice answers: %v", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO submission_answers(submission_id, field_id, field_type, value_number)
		SELECT ('20000000-0000-4000-8000-' || lpad(g::text, 12, '0'))::uuid,
		       $1, 'number', (g % 101)::numeric
		FROM generate_series(1, $2) AS g`, ids.score, stageThreeBenchmarkRows)
	if err != nil {
		t.Fatalf("seed numeric answers: %v", err)
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO analytics_daily(project_id, version_id, event_date, page_id, views, starts, steps, completes)
		SELECT $1, $2, date '2026-01-01' + g, '00000000-0000-0000-0000-000000000000', 360, 320, 0, 0
		FROM generate_series(0, 364) AS g`, created.ID, version.ID)
	if err != nil {
		t.Fatalf("seed analytics: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit benchmark seed: %v", err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE public_sessions, submissions, submission_answers, analytics_daily`); err != nil {
		t.Fatalf("analyze benchmark tables: %v", err)
	}
	seedDuration := time.Since(seedStarted)

	location, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	service, err := response.NewService(response.NewStore(pool), location)
	if err != nil {
		t.Fatalf("create response service: %v", err)
	}
	listStarted := time.Now()
	listed, err := service.List(ctx, actor, created.ID, response.Filter{Page: 1, PageSize: 25})
	listDuration := time.Since(listStarted)
	if err != nil || listed.Pagination.Total != stageThreeBenchmarkRows || len(listed.Items) != 25 {
		t.Fatalf("large response list failed: total=%d items=%d error=%v", listed.Pagination.Total, len(listed.Items), err)
	}
	reportStarted := time.Now()
	report, err := service.Report(ctx, actor, created.ID, response.ReportFilter{})
	reportDuration := time.Since(reportStarted)
	if err != nil || report.KPI.Completes != stageThreeBenchmarkRows || len(report.Choices) != 1 || len(report.Numbers) != 1 {
		t.Fatalf("large report failed: report=%+v error=%v", report.KPI, err)
	}
	prepareStarted := time.Now()
	export, err := service.PrepareExport(ctx, actor, created.ID, response.Filter{}, response.ExportOptions{Format: response.ExportCSV, BOM: true})
	prepareDuration := time.Since(prepareStarted)
	if err != nil || export.Count() != stageThreeBenchmarkRows {
		t.Fatalf("prepare large export: count=%d error=%v", export.Count(), err)
	}
	exportStarted := time.Now()
	if err := export.Write(ctx, io.Discard); err != nil {
		t.Fatalf("stream large CSV export: %v", err)
	}
	exportDuration := time.Since(exportStarted)

	var explain []byte
	if err := pool.QueryRow(ctx, `
		EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
		SELECT a.value_text, count(*)
		FROM submission_answers a
		JOIN submissions s ON s.id = a.submission_id
		WHERE s.project_id = $1 AND s.deleted_at IS NULL AND a.field_id = $2
		GROUP BY a.value_text ORDER BY count(*) DESC`, created.ID, ids.choice).Scan(&explain); err != nil {
		t.Fatalf("explain large aggregation: %v", err)
	}
	var plans []struct {
		Plan struct {
			NodeType        string `json:"Node Type"`
			SharedHitBlocks int64  `json:"Shared Hit Blocks"`
		} `json:"Plan"`
		ExecutionTime float64 `json:"Execution Time"`
	}
	if err := json.Unmarshal(explain, &plans); err != nil || len(plans) != 1 || plans[0].ExecutionTime <= 0 || plans[0].Plan.SharedHitBlocks <= 0 {
		t.Fatalf("EXPLAIN ANALYZE did not include execution/buffer evidence: %s; error=%v", explain, err)
	}
	if listDuration > 2*time.Second || reportDuration > 5*time.Second || prepareDuration > 2*time.Second || exportDuration > 30*time.Second {
		t.Fatalf("large dataset budget exceeded: list=%s report=%s prepare=%s export=%s", listDuration, reportDuration, prepareDuration, exportDuration)
	}
	t.Logf("stage3-100k seed=%s list=%s report=%s export_prepare=%s csv_stream=%s", seedDuration, listDuration, reportDuration, prepareDuration, exportDuration)
	t.Logf("stage3-100k choice EXPLAIN ANALYZE node=%s execution_ms=%.3f shared_hit_blocks=%d",
		plans[0].Plan.NodeType, plans[0].ExecutionTime, plans[0].Plan.SharedHitBlocks)
}
