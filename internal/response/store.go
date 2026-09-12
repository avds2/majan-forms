package response

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"forms/internal/audit"
)

const zeroUUID = "00000000-0000-0000-0000-000000000000"

var searchTokenPattern = regexp.MustCompile(`[\p{L}\p{N}]+`)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

func (s *Store) List(ctx context.Context, projectID string, filter Filter) ([]Summary, int64, error) {
	where, args := submissionWhere(projectID, filter)
	var total int64
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM submissions s
		JOIN projects p ON p.id = s.project_id AND p.deleted_at IS NULL
		JOIN form_versions v ON v.id = s.version_id
		WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count responses: %w", err)
	}

	order := map[Sort]string{
		SortNewest:        "s.submitted_at DESC, s.id DESC",
		SortOldest:        "s.submitted_at ASC, s.id ASC",
		SortDurationLong:  "(s.submitted_at - s.started_at) DESC, s.submitted_at DESC",
		SortDurationShort: "(s.submitted_at - s.started_at) ASC, s.submitted_at DESC",
	}[filter.Sort]
	args = append(args, filter.PageSize, (filter.Page-1)*filter.PageSize)
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.public_id, s.version_id, v.version_number, s.ending_id,
		       coalesce((
		           SELECT ending->>'title'
		           FROM jsonb_array_elements(v.schema->'endings') ending
		           WHERE ending->>'id' = s.ending_id::text
		           LIMIT 1
		       ), 'پایان بدون عنوان'),
		       s.started_at, s.submitted_at,
		       floor(extract(epoch FROM s.submitted_at - s.started_at))::bigint,
		       s.deleted_at
		FROM submissions s
		JOIN projects p ON p.id = s.project_id AND p.deleted_at IS NULL
		JOIN form_versions v ON v.id = s.version_id
		WHERE `+where+`
		ORDER BY `+order+`
		LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list responses: %w", err)
	}
	defer rows.Close()
	items := make([]Summary, 0)
	for rows.Next() {
		var item Summary
		if err := rows.Scan(
			&item.ID, &item.PublicID, &item.VersionID, &item.VersionNumber, &item.EndingID,
			&item.EndingTitle, &item.StartedAt, &item.SubmittedAt, &item.DurationSeconds, &item.DeletedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan response summary: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list response rows: %w", err)
	}
	return items, total, nil
}

func submissionWhere(projectID string, filter Filter) (string, []any) {
	parts := []string{"s.project_id = $1"}
	args := []any{projectID}
	add := func(fragment string, value any) {
		args = append(args, value)
		parts = append(parts, fmt.Sprintf(fragment, len(args)))
	}
	switch filter.Deleted {
	case DeletedOnly:
		parts = append(parts, "s.deleted_at IS NOT NULL")
	case DeletedAll:
	default:
		parts = append(parts, "s.deleted_at IS NULL")
	}
	if filter.Search != "" {
		args = append(args, prefixSearchQuery(filter.Search))
		placeholder := fmt.Sprint(len(args))
		parts = append(parts, "to_tsvector('simple', s.search_text) @@ to_tsquery('simple', $"+placeholder+")")
	}
	if filter.VersionNumber != nil {
		add("v.version_number = $%d", *filter.VersionNumber)
	}
	if filter.EndingID != "" {
		add("s.ending_id::text = $%d", filter.EndingID)
	}
	if filter.From != nil {
		add("s.submitted_at >= $%d", *filter.From)
	}
	if filter.To != nil {
		add("s.submitted_at < $%d", *filter.To)
	}
	return strings.Join(parts, " AND "), args
}

func prefixSearchQuery(value string) string {
	tokens := searchTokenPattern.FindAllString(value, -1)
	parts := make([]string, 0, len(tokens))
	for _, token := range tokens {
		parts = append(parts, token+":*")
	}
	return strings.Join(parts, " & ")
}

func (s *Store) Get(ctx context.Context, projectID, responseID string) (storedSubmission, []storedAnswer, []File, error) {
	var result storedSubmission
	var encoded []byte
	err := s.pool.QueryRow(ctx, `
		SELECT s.id, s.public_id, s.version_id, v.version_number, s.ending_id,
		       coalesce((
		           SELECT ending->>'title'
		           FROM jsonb_array_elements(v.schema->'endings') ending
		           WHERE ending->>'id' = s.ending_id::text
		           LIMIT 1
		       ), 'پایان بدون عنوان'),
		       s.started_at, s.submitted_at,
		       floor(extract(epoch FROM s.submitted_at - s.started_at))::bigint,
		       s.deleted_at, s.project_id, p.title, v.schema
		FROM submissions s
		JOIN projects p ON p.id = s.project_id AND p.deleted_at IS NULL
		JOIN form_versions v ON v.id = s.version_id
		WHERE s.project_id = $1 AND s.id = $2`, projectID, responseID,
	).Scan(
		&result.ID, &result.PublicID, &result.VersionID, &result.VersionNumber, &result.EndingID,
		&result.EndingTitle, &result.StartedAt, &result.SubmittedAt, &result.DurationSeconds,
		&result.DeletedAt, &result.ProjectID, &result.ProjectTitle, &encoded,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return storedSubmission{}, nil, nil, ErrNotFound
	}
	if err != nil {
		return storedSubmission{}, nil, nil, fmt.Errorf("get response: %w", err)
	}
	if err := json.Unmarshal(encoded, &result.Schema); err != nil {
		return storedSubmission{}, nil, nil, fmt.Errorf("decode response schema: %w", err)
	}
	answers, err := s.listAnswers(ctx, result.ID)
	if err != nil {
		return storedSubmission{}, nil, nil, err
	}
	files, err := s.listFiles(ctx, result.ID)
	if err != nil {
		return storedSubmission{}, nil, nil, err
	}
	return result, answers, files, nil
}

func (s *Store) listAnswers(ctx context.Context, responseID string) ([]storedAnswer, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT field_id, field_type,
		       CASE
		           WHEN value_text IS NOT NULL THEN to_jsonb(value_text)
		           WHEN value_number IS NOT NULL THEN to_jsonb(value_number)
		           WHEN value_bool IS NOT NULL THEN to_jsonb(value_bool)
		           WHEN value_date IS NOT NULL THEN to_jsonb(value_date::text)
		           WHEN value_time IS NOT NULL THEN to_jsonb(value_time::text)
		           WHEN value_timestamp IS NOT NULL THEN to_jsonb(value_timestamp)
		           ELSE value_json
		       END
		FROM submission_answers
		WHERE submission_id = $1
		ORDER BY id`, responseID)
	if err != nil {
		return nil, fmt.Errorf("list response answers: %w", err)
	}
	defer rows.Close()
	answers := make([]storedAnswer, 0)
	for rows.Next() {
		var answer storedAnswer
		var encoded []byte
		if err := rows.Scan(&answer.FieldID, &answer.FieldType, &encoded); err != nil {
			return nil, fmt.Errorf("scan response answer: %w", err)
		}
		decoder := json.NewDecoder(strings.NewReader(string(encoded)))
		decoder.UseNumber()
		if err := decoder.Decode(&answer.Value); err != nil {
			return nil, fmt.Errorf("decode response answer: %w", err)
		}
		answers = append(answers, answer)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("response answer rows: %w", err)
	}
	return answers, nil
}

func (s *Store) listFiles(ctx context.Context, responseID string) ([]File, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, field_id, original_name, size_bytes, detected_mime, checksum
		FROM stored_files
		WHERE submission_id = $1 AND status = 'finalized'
		ORDER BY created_at, id`, responseID)
	if err != nil {
		return nil, fmt.Errorf("list response files: %w", err)
	}
	defer rows.Close()
	files := make([]File, 0)
	for rows.Next() {
		var file File
		if err := rows.Scan(&file.ID, &file.FieldID, &file.Name, &file.SizeBytes, &file.MIME, &file.Checksum); err != nil {
			return nil, fmt.Errorf("scan response file: %w", err)
		}
		file.DownloadPath = "/api/files/" + file.ID
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("response file rows: %w", err)
	}
	return files, nil
}

func (s *Store) SetDeleted(ctx context.Context, actorID, projectID string, responseIDs []string, deleted bool) (int64, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin response deletion: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	set := "now()"
	action := "responses.deleted"
	if !deleted {
		set = "NULL"
		action = "responses.restored"
	}
	command, err := tx.Exec(ctx, `UPDATE submissions s
		SET deleted_at = `+set+`
		FROM projects p
		WHERE s.project_id = $1 AND s.id = ANY($2::uuid[])
		  AND p.id = s.project_id AND p.deleted_at IS NULL`, projectID, responseIDs)
	if err != nil {
		return 0, fmt.Errorf("update response deletion: %w", err)
	}
	if err := audit.Insert(ctx, tx, &actorID, action, "project", projectID, map[string]any{
		"response_ids": responseIDs,
		"count":        command.RowsAffected(),
	}); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit response deletion: %w", err)
	}
	return command.RowsAffected(), nil
}

func (s *Store) Report(ctx context.Context, projectID string, filter ReportFilter) (storedReport, error) {
	var result storedReport
	if err := s.pool.QueryRow(ctx, `SELECT title FROM projects WHERE id = $1 AND deleted_at IS NULL`, projectID).Scan(&result.ProjectTitle); errors.Is(err, pgx.ErrNoRows) {
		return storedReport{}, ErrNotFound
	} else if err != nil {
		return storedReport{}, fmt.Errorf("get report project: %w", err)
	}
	versions, err := s.listVersionSchemas(ctx, projectID)
	if err != nil {
		return storedReport{}, err
	}
	result.Versions = versions

	analyticsWhere, analyticsArgs := analyticsWhere(projectID, filter)
	if err := s.pool.QueryRow(ctx, `
		SELECT coalesce(sum(ad.views), 0), coalesce(sum(ad.starts), 0)
		FROM analytics_daily ad
		JOIN form_versions v ON v.id = ad.version_id
		WHERE ad.page_id = '`+zeroUUID+`' AND `+analyticsWhere, analyticsArgs...).Scan(&result.KPI.Views, &result.KPI.Starts); err != nil {
		return storedReport{}, fmt.Errorf("summarize report events: %w", err)
	}
	responseWhere, responseArgs := reportResponseWhere(projectID, filter)
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*), coalesce(avg(extract(epoch FROM s.submitted_at - s.started_at)), 0)
		FROM submissions s
		JOIN form_versions v ON v.id = s.version_id
		WHERE `+responseWhere, responseArgs...).Scan(&result.KPI.Completes, &result.KPI.AverageDurationSeconds); err != nil {
		return storedReport{}, fmt.Errorf("summarize report responses: %w", err)
	}
	if result.KPI.Starts > 0 {
		result.KPI.CompletionRate = float64(result.KPI.Completes) * 100 / float64(result.KPI.Starts)
	}

	trend := make(map[string]*rawTrend)
	rows, err := s.pool.Query(ctx, `
		SELECT ad.event_date, sum(ad.views), sum(ad.starts)
		FROM analytics_daily ad
		JOIN form_versions v ON v.id = ad.version_id
		WHERE ad.page_id = '`+zeroUUID+`' AND `+analyticsWhere+`
		GROUP BY ad.event_date ORDER BY ad.event_date`, analyticsArgs...)
	if err != nil {
		return storedReport{}, fmt.Errorf("list report event trend: %w", err)
	}
	for rows.Next() {
		var point rawTrend
		if err := rows.Scan(&point.Date, &point.Views, &point.Starts); err != nil {
			rows.Close()
			return storedReport{}, fmt.Errorf("scan report event trend: %w", err)
		}
		trend[point.Date.Format(time.DateOnly)] = &point
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return storedReport{}, fmt.Errorf("report event trend rows: %w", err)
	}
	rows.Close()
	trendResponseArgs := append([]any{}, responseArgs...)
	trendResponseArgs = append(trendResponseArgs, filter.timezone)
	timezonePlaceholder := fmt.Sprintf("$%d", len(trendResponseArgs))
	rows, err = s.pool.Query(ctx, `
		SELECT (s.submitted_at AT TIME ZONE `+timezonePlaceholder+`)::date, count(*)
		FROM submissions s
		JOIN form_versions v ON v.id = s.version_id
		WHERE `+responseWhere+`
		GROUP BY 1 ORDER BY 1`, trendResponseArgs...)
	if err != nil {
		return storedReport{}, fmt.Errorf("list report response trend: %w", err)
	}
	for rows.Next() {
		var date time.Time
		var count int64
		if err := rows.Scan(&date, &count); err != nil {
			rows.Close()
			return storedReport{}, fmt.Errorf("scan report response trend: %w", err)
		}
		key := date.Format(time.DateOnly)
		point := trend[key]
		if point == nil {
			point = &rawTrend{Date: date}
			trend[key] = point
		}
		point.Completes = count
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return storedReport{}, fmt.Errorf("report response trend rows: %w", err)
	}
	rows.Close()
	keys := make([]string, 0, len(trend))
	for key := range trend {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result.Trend = append(result.Trend, *trend[key])
	}

	countVersionSelect, countVersionGroup := "s.version_id", "s.version_id, s.ending_id"
	pageVersionSelect, pageVersionGroup := "ad.version_id", "ad.version_id, ad.page_id"
	if filter.VersionNumber == nil {
		countVersionSelect, countVersionGroup = "'"+zeroUUID+"'::uuid", "s.ending_id"
		pageVersionSelect, pageVersionGroup = "'"+zeroUUID+"'::uuid", "ad.page_id"
	}
	result.Endings, err = s.reportCounts(ctx, `
		SELECT `+countVersionSelect+`, s.ending_id, count(*)
		FROM submissions s
		JOIN form_versions v ON v.id = s.version_id
		WHERE `+responseWhere+`
		GROUP BY `+countVersionGroup+`
		ORDER BY count(*) DESC`, responseArgs)
	if err != nil {
		return storedReport{}, fmt.Errorf("summarize report endings: %w", err)
	}
	result.Pages, err = s.reportCounts(ctx, `
		SELECT `+pageVersionSelect+`, ad.page_id, sum(ad.steps)
		FROM analytics_daily ad
		JOIN form_versions v ON v.id = ad.version_id
		WHERE ad.page_id <> '`+zeroUUID+`' AND `+analyticsWhere+`
		GROUP BY `+pageVersionGroup+`
		ORDER BY sum(ad.steps) DESC`, analyticsArgs)
	if err != nil {
		return storedReport{}, fmt.Errorf("summarize report pages: %w", err)
	}
	if err := s.loadChoiceReport(ctx, &result, responseWhere, responseArgs, filter.VersionNumber == nil); err != nil {
		return storedReport{}, err
	}
	if err := s.loadNumberReport(ctx, &result, responseWhere, responseArgs, filter.VersionNumber == nil); err != nil {
		return storedReport{}, err
	}
	return result, nil
}

func (s *Store) listVersionSchemas(ctx context.Context, projectID string) ([]versionSchema, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, version_number, published_at, schema FROM form_versions WHERE project_id = $1 ORDER BY version_number DESC`, projectID)
	if err != nil {
		return nil, fmt.Errorf("list report versions: %w", err)
	}
	defer rows.Close()
	versions := make([]versionSchema, 0)
	for rows.Next() {
		var version versionSchema
		var encoded []byte
		if err := rows.Scan(&version.ID, &version.VersionNumber, &version.PublishedAt, &encoded); err != nil {
			return nil, fmt.Errorf("scan report version: %w", err)
		}
		if err := json.Unmarshal(encoded, &version.Schema); err != nil {
			return nil, fmt.Errorf("decode report version: %w", err)
		}
		versions = append(versions, version)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("report version rows: %w", err)
	}
	return versions, nil
}

func (s *Store) ExportContext(ctx context.Context, projectID string, filter Filter) (string, []versionSchema, int64, error) {
	var title string
	if err := s.pool.QueryRow(ctx, `SELECT title FROM projects WHERE id = $1 AND deleted_at IS NULL`, projectID).Scan(&title); errors.Is(err, pgx.ErrNoRows) {
		return "", nil, 0, ErrNotFound
	} else if err != nil {
		return "", nil, 0, fmt.Errorf("get export project: %w", err)
	}
	versions, err := s.listVersionSchemas(ctx, projectID)
	if err != nil {
		return "", nil, 0, err
	}
	where, args := submissionWhere(projectID, filter)
	var count int64
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM submissions s
		JOIN projects p ON p.id = s.project_id AND p.deleted_at IS NULL
		JOIN form_versions v ON v.id = s.version_id
		WHERE `+where, args...).Scan(&count); err != nil {
		return "", nil, 0, fmt.Errorf("count export responses: %w", err)
	}
	return title, versions, count, nil
}

func (s *Store) StreamExport(ctx context.Context, projectID string, filter Filter, yield func(exportRecord) error) error {
	where, args := submissionWhere(projectID, filter)
	rows, err := s.pool.Query(ctx, `
		SELECT s.id, s.public_id, s.version_id, v.version_number, s.ending_id,
		       s.started_at, s.submitted_at,
		       floor(extract(epoch FROM s.submitted_at - s.started_at))::bigint,
		       s.deleted_at,
		       coalesce((
		           SELECT jsonb_object_agg(a.field_id::text, jsonb_build_object(
		               'type', a.field_type,
		               'value', CASE
		                   WHEN a.value_text IS NOT NULL THEN to_jsonb(a.value_text)
		                   WHEN a.value_number IS NOT NULL THEN to_jsonb(a.value_number)
		                   WHEN a.value_bool IS NOT NULL THEN to_jsonb(a.value_bool)
		                   WHEN a.value_date IS NOT NULL THEN to_jsonb(a.value_date::text)
		                   WHEN a.value_time IS NOT NULL THEN to_jsonb(a.value_time::text)
		                   WHEN a.value_timestamp IS NOT NULL THEN to_jsonb(a.value_timestamp)
		                   ELSE a.value_json
		               END
		           ))
		           FROM submission_answers a WHERE a.submission_id = s.id
		       ), '{}'::jsonb),
		       coalesce((
		           SELECT jsonb_object_agg(grouped.field_id::text, grouped.items)
		           FROM (
		               SELECT f.field_id, jsonb_agg(jsonb_build_object(
		                   'id', f.id, 'name', f.original_name, 'size_bytes', f.size_bytes,
		                   'mime', f.detected_mime, 'checksum', f.checksum,
		                   'download_path', '/api/files/' || f.id::text
		               ) ORDER BY f.created_at, f.id) AS items
		               FROM stored_files f
		               WHERE f.submission_id = s.id AND f.status = 'finalized'
		               GROUP BY f.field_id
		           ) grouped
		       ), '{}'::jsonb)
		FROM submissions s
		JOIN projects p ON p.id = s.project_id AND p.deleted_at IS NULL
		JOIN form_versions v ON v.id = s.version_id
		WHERE `+where+`
		ORDER BY s.submitted_at ASC, s.id ASC`, args...)
	if err != nil {
		return fmt.Errorf("stream export responses: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var record exportRecord
		var encodedAnswers, encodedFiles []byte
		if err := rows.Scan(
			&record.ID, &record.PublicID, &record.VersionID, &record.VersionNumber, &record.EndingID,
			&record.StartedAt, &record.SubmittedAt, &record.DurationSeconds, &record.DeletedAt,
			&encodedAnswers, &encodedFiles,
		); err != nil {
			return fmt.Errorf("scan export response: %w", err)
		}
		answerDecoder := json.NewDecoder(strings.NewReader(string(encodedAnswers)))
		answerDecoder.UseNumber()
		if err := answerDecoder.Decode(&record.Answers); err != nil {
			return fmt.Errorf("decode export answers: %w", err)
		}
		if err := json.Unmarshal(encodedFiles, &record.Files); err != nil {
			return fmt.Errorf("decode export files: %w", err)
		}
		for fieldID, files := range record.Files {
			for index := range files {
				files[index].FieldID = fieldID
			}
			record.Files[fieldID] = files
		}
		if err := yield(record); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("export response rows: %w", err)
	}
	return nil
}

func analyticsWhere(projectID string, filter ReportFilter) (string, []any) {
	parts := []string{"ad.project_id = $1"}
	args := []any{projectID}
	if filter.VersionNumber != nil {
		args = append(args, *filter.VersionNumber)
		parts = append(parts, fmt.Sprintf("v.version_number = $%d", len(args)))
	}
	if filter.fromDate != "" {
		args = append(args, filter.fromDate)
		parts = append(parts, fmt.Sprintf("ad.event_date >= $%d::date", len(args)))
	}
	if filter.toDate != "" {
		args = append(args, filter.toDate)
		parts = append(parts, fmt.Sprintf("ad.event_date <= $%d::date", len(args)))
	}
	return strings.Join(parts, " AND "), args
}

func reportResponseWhere(projectID string, filter ReportFilter) (string, []any) {
	parts := []string{"s.project_id = $1", "s.deleted_at IS NULL"}
	args := []any{projectID}
	if filter.VersionNumber != nil {
		args = append(args, *filter.VersionNumber)
		parts = append(parts, fmt.Sprintf("v.version_number = $%d", len(args)))
	}
	if filter.EndingID != "" {
		args = append(args, filter.EndingID)
		parts = append(parts, fmt.Sprintf("s.ending_id::text = $%d", len(args)))
	}
	if filter.FieldID != "" {
		args = append(args, filter.FieldID)
		fieldPlaceholder := len(args)
		valuePredicate := ""
		if filter.FieldValue != "" {
			args = append(args, filter.FieldValue)
			valuePlaceholder := len(args)
			valuePredicate = fmt.Sprintf(` AND (
				translate(translate(fa.value_text, '۰۱۲۳۴۵۶۷۸۹', '0123456789'), '٠١٢٣٤٥٦٧٨٩', '0123456789') = $%[1]d OR
				fa.value_number::text = $%[1]d OR fa.value_bool::text = lower($%[1]d) OR
				fa.value_date::text = $%[1]d OR to_char(fa.value_time, 'HH24:MI') = $%[1]d OR
				(jsonb_typeof(fa.value_json) = 'array' AND fa.value_json ? $%[1]d)
			)`, valuePlaceholder)
		}
		parts = append(parts, fmt.Sprintf("EXISTS (SELECT 1 FROM submission_answers fa WHERE fa.submission_id = s.id AND fa.field_id::text = $%d%s)", fieldPlaceholder, valuePredicate))
	}
	if filter.From != nil {
		args = append(args, *filter.From)
		parts = append(parts, fmt.Sprintf("s.submitted_at >= $%d", len(args)))
	}
	if filter.To != nil {
		args = append(args, *filter.To)
		parts = append(parts, fmt.Sprintf("s.submitted_at < $%d", len(args)))
	}
	return strings.Join(parts, " AND "), args
}

func (s *Store) reportCounts(ctx context.Context, query string, args []any) ([]rawCount, error) {
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]rawCount, 0)
	for rows.Next() {
		var value rawCount
		if err := rows.Scan(&value.VersionID, &value.ID, &value.Count); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (s *Store) loadChoiceReport(ctx context.Context, result *storedReport, where string, args []any, acrossVersions bool) error {
	versionSelect, versionGroup := "s.version_id", "s.version_id, a.field_id, 3"
	if acrossVersions {
		versionSelect, versionGroup = "'"+zeroUUID+"'::uuid", "a.field_id, 3"
	}
	query := `
		SELECT ` + versionSelect + `, a.field_id,
		       CASE WHEN a.value_bool IS NOT NULL THEN a.value_bool::text ELSE a.value_text END,
		       count(*)
		FROM submission_answers a
		JOIN submissions s ON s.id = a.submission_id
		JOIN form_versions v ON v.id = s.version_id
		WHERE ` + where + ` AND a.field_type IN ('yes_no', 'single_choice', 'dropdown')
		GROUP BY ` + versionGroup
	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("summarize scalar choices: %w", err)
	}
	for rows.Next() {
		var value rawChoice
		if err := rows.Scan(&value.VersionID, &value.FieldID, &value.Value, &value.Count); err != nil {
			rows.Close()
			return fmt.Errorf("scan scalar choice: %w", err)
		}
		result.Choices = append(result.Choices, value)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("scalar choice rows: %w", err)
	}
	rows.Close()
	versionGroup = "s.version_id, a.field_id, item.value"
	if acrossVersions {
		versionGroup = "a.field_id, item.value"
	}
	query = `
		SELECT ` + versionSelect + `, a.field_id, item.value, count(*)
		FROM submission_answers a
		JOIN submissions s ON s.id = a.submission_id
		JOIN form_versions v ON v.id = s.version_id
		CROSS JOIN LATERAL jsonb_array_elements_text(a.value_json) item(value)
		WHERE ` + where + ` AND a.field_type IN ('multi_choice', 'multi_dropdown')
		GROUP BY ` + versionGroup
	rows, err = s.pool.Query(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("summarize multiple choices: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var value rawChoice
		if err := rows.Scan(&value.VersionID, &value.FieldID, &value.Value, &value.Count); err != nil {
			return fmt.Errorf("scan multiple choice: %w", err)
		}
		result.Choices = append(result.Choices, value)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("multiple choice rows: %w", err)
	}
	return nil
}

func (s *Store) loadNumberReport(ctx context.Context, result *storedReport, where string, args []any, acrossVersions bool) error {
	versionSelect, versionGroup := "s.version_id", "s.version_id, a.field_id"
	if acrossVersions {
		versionSelect, versionGroup = "'"+zeroUUID+"'::uuid", "a.field_id"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT `+versionSelect+`, a.field_id, count(*), min(a.value_number)::float8,
		       max(a.value_number)::float8, avg(a.value_number)::float8,
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY a.value_number)::float8,
		       CASE WHEN bool_or(a.field_type = 'nps') THEN
		           100.0 * (count(*) FILTER (WHERE a.value_number >= 9) - count(*) FILTER (WHERE a.value_number <= 6)) / count(*)
		       END::float8
		FROM submission_answers a
		JOIN submissions s ON s.id = a.submission_id
		JOIN form_versions v ON v.id = s.version_id
		WHERE `+where+` AND a.value_number IS NOT NULL
		GROUP BY `+versionGroup, args...)
	if err != nil {
		return fmt.Errorf("summarize numeric answers: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var value rawNumber
		if err := rows.Scan(&value.VersionID, &value.FieldID, &value.Count, &value.Minimum, &value.Maximum, &value.Average, &value.Median, &value.NPS); err != nil {
			return fmt.Errorf("scan numeric summary: %w", err)
		}
		result.Numbers = append(result.Numbers, value)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("numeric summary rows: %w", err)
	}
	return nil
}
