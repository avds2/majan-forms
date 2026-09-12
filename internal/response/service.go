package response

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"forms/internal/auth"
	"forms/internal/form"
	"forms/internal/normalize"
	"forms/internal/persiandate"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Service struct {
	store    *Store
	location *time.Location
	now      func() time.Time
}

func NewService(store *Store, location *time.Location) (*Service, error) {
	if store == nil || location == nil {
		return nil, errors.New("response store and timezone are required")
	}
	return &Service{store: store, location: location, now: func() time.Time { return time.Now().UTC() }}, nil
}

func (s *Service) List(ctx context.Context, _ auth.User, projectID string, filter Filter) (ListResult, error) {
	filter, err := normalizeFilter(projectID, filter)
	if err != nil {
		return ListResult{}, err
	}
	filter.From, filter.To = localDayBounds(filter.From, filter.To, s.location)
	items, total, err := s.store.List(ctx, projectID, filter)
	if err != nil {
		return ListResult{}, err
	}
	for index := range items {
		if items[index].SubmittedAtJalali, err = persiandate.FormatTime(items[index].SubmittedAt, s.location); err != nil {
			return ListResult{}, fmt.Errorf("format response submitted time: %w", err)
		}
		if items[index].DeletedAt != nil {
			items[index].DeletedAtJalali, err = persiandate.FormatTime(*items[index].DeletedAt, s.location)
			if err != nil {
				return ListResult{}, fmt.Errorf("format response deletion time: %w", err)
			}
		}
	}
	totalPages := 0
	if total > 0 {
		totalPages = int((total + int64(filter.PageSize) - 1) / int64(filter.PageSize))
	}
	return ListResult{Items: items, Pagination: Pagination{
		Page: filter.Page, PageSize: filter.PageSize, Total: total, TotalPages: totalPages,
	}}, nil
}

func normalizeFilter(projectID string, filter Filter) (Filter, error) {
	if !uuidPattern.MatchString(projectID) {
		return Filter{}, ErrInvalidFilter
	}
	if filter.Page == 0 {
		filter.Page = 1
	}
	if filter.PageSize == 0 {
		filter.PageSize = 25
	}
	if filter.Deleted == "" {
		filter.Deleted = DeletedActive
	}
	if filter.Sort == "" {
		filter.Sort = SortNewest
	}
	if filter.Page < 1 || filter.PageSize < 1 || filter.PageSize > 100 ||
		(filter.Deleted != DeletedActive && filter.Deleted != DeletedOnly && filter.Deleted != DeletedAll) ||
		(filter.Sort != SortNewest && filter.Sort != SortOldest && filter.Sort != SortDurationLong && filter.Sort != SortDurationShort) ||
		(filter.VersionNumber != nil && *filter.VersionNumber < 1) ||
		(filter.EndingID != "" && !uuidPattern.MatchString(filter.EndingID)) ||
		(filter.From != nil && filter.To != nil && filter.From.After(*filter.To)) {
		return Filter{}, ErrInvalidFilter
	}
	filter.Search = normalize.SearchText(filter.Search)
	if utf8.RuneCountInString(filter.Search) > 200 {
		return Filter{}, ErrInvalidFilter
	}
	return filter, nil
}

func (s *Service) Get(ctx context.Context, _ auth.User, projectID, responseID string) (Detail, error) {
	if !uuidPattern.MatchString(projectID) || !uuidPattern.MatchString(responseID) {
		return Detail{}, ErrNotFound
	}
	stored, values, files, err := s.store.Get(ctx, projectID, responseID)
	if err != nil {
		return Detail{}, err
	}
	stored.SubmittedAtJalali, err = persiandate.FormatTime(stored.SubmittedAt, s.location)
	if err != nil {
		return Detail{}, fmt.Errorf("format response submitted time: %w", err)
	}
	if stored.DeletedAt != nil {
		stored.DeletedAtJalali, err = persiandate.FormatTime(*stored.DeletedAt, s.location)
		if err != nil {
			return Detail{}, fmt.Errorf("format response deletion time: %w", err)
		}
	}
	byField := make(map[string]storedAnswer, len(values))
	for _, answer := range values {
		byField[answer.FieldID] = answer
	}
	filesByField := make(map[string][]File)
	for _, file := range files {
		filesByField[file.FieldID] = append(filesByField[file.FieldID], file)
	}
	answers := make([]Answer, 0)
	for _, page := range stored.Schema.Pages {
		for _, field := range page.Fields {
			if !isResponseField(field.Type) {
				continue
			}
			value, present := byField[field.ID]
			fieldFiles := filesByField[field.ID]
			if !present && len(fieldFiles) == 0 {
				answers = append(answers, Answer{
					FieldID: field.ID, FieldType: field.Type, Label: field.Label,
					PageTitle: page.Title, DisplayValue: "بدون پاسخ",
				})
				continue
			}
			display := ""
			if present {
				display = displayValue(field, value.Value, s.location)
			} else if len(fieldFiles) == 1 {
				display = fieldFiles[0].Name
			} else {
				display = persiandate.PersianDigits(strconv.Itoa(len(fieldFiles))) + " فایل"
			}
			answers = append(answers, Answer{
				FieldID: field.ID, FieldType: field.Type, Label: field.Label, PageTitle: page.Title,
				Value: value.Value, DisplayValue: display, Files: fieldFiles,
			})
		}
	}
	return Detail{
		Summary: stored.Summary, ProjectID: stored.ProjectID, ProjectTitle: stored.ProjectTitle, Answers: answers,
	}, nil
}

func (s *Service) Delete(ctx context.Context, actor auth.User, projectID string, responseIDs []string) (int64, error) {
	return s.setDeleted(ctx, actor, projectID, responseIDs, true)
}

func (s *Service) Restore(ctx context.Context, actor auth.User, projectID string, responseIDs []string) (int64, error) {
	return s.setDeleted(ctx, actor, projectID, responseIDs, false)
}

func (s *Service) setDeleted(ctx context.Context, actor auth.User, projectID string, responseIDs []string, deleted bool) (int64, error) {
	if !actor.Role.CanEditProjects() {
		return 0, auth.ErrForbidden
	}
	if !uuidPattern.MatchString(projectID) || len(responseIDs) == 0 || len(responseIDs) > 500 {
		return 0, ErrEmptySelection
	}
	seen := make(map[string]bool, len(responseIDs))
	clean := make([]string, 0, len(responseIDs))
	for _, responseID := range responseIDs {
		if !uuidPattern.MatchString(responseID) {
			return 0, ErrInvalidFilter
		}
		if !seen[responseID] {
			seen[responseID] = true
			clean = append(clean, responseID)
		}
	}
	return s.store.SetDeleted(ctx, actor.ID, projectID, clean, deleted)
}

func (s *Service) Report(ctx context.Context, _ auth.User, projectID string, filter ReportFilter) (Report, error) {
	filter.FieldValue = normalize.Digits(strings.TrimSpace(filter.FieldValue))
	if !uuidPattern.MatchString(projectID) ||
		(filter.VersionNumber != nil && *filter.VersionNumber < 1) ||
		(filter.EndingID != "" && !uuidPattern.MatchString(filter.EndingID)) ||
		(filter.FieldID != "" && !uuidPattern.MatchString(filter.FieldID)) ||
		(filter.FieldValue != "" && filter.FieldID == "") || utf8.RuneCountInString(filter.FieldValue) > 500 ||
		(filter.From != nil && filter.To != nil && filter.From.After(*filter.To)) {
		return Report{}, ErrInvalidFilter
	}
	if filter.From != nil {
		filter.fromDate = filter.From.Format(time.DateOnly)
	}
	if filter.To != nil {
		filter.toDate = filter.To.Format(time.DateOnly)
	}
	filter.timezone = s.location.String()
	filter.From, filter.To = localDayBounds(filter.From, filter.To, s.location)
	stored, err := s.store.Report(ctx, projectID, filter)
	if err != nil {
		return Report{}, err
	}
	generated := s.now()
	stored.KPI.CompletionRate = rounded(stored.KPI.CompletionRate)
	stored.KPI.AverageDurationSeconds = rounded(stored.KPI.AverageDurationSeconds)
	generatedJalali, err := persiandate.FormatTime(generated, s.location)
	if err != nil {
		return Report{}, fmt.Errorf("format report generation time: %w", err)
	}
	report := Report{
		ProjectID: projectID, ProjectTitle: stored.ProjectTitle, GeneratedAt: generated,
		GeneratedJalali: generatedJalali, KPI: stored.KPI,
		Trend: make([]TrendPoint, 0, len(stored.Trend)), Endings: []CountBreakdown{},
		PageActivity: []CountBreakdown{}, Choices: []ChoiceBreakdown{}, Numbers: []NumericBreakdown{},
		Versions: make([]VersionOption, 0, len(stored.Versions)),
		Fields:   []ReportFieldOption{}, EndingOptions: []ReportEndingOption{},
		Segmented: filter.EndingID != "" || filter.FieldID != "",
	}
	versionMap := make(map[string]versionSchema, len(stored.Versions))
	for _, version := range stored.Versions {
		versionMap[version.ID] = version
		report.Versions = append(report.Versions, VersionOption{ID: version.ID, VersionNumber: version.VersionNumber, PublishedAt: version.PublishedAt})
	}
	report.Fields, report.EndingOptions = reportFilterOptions(stored.Versions, filter.VersionNumber)
	if !validReportSegment(filter, report.Fields, report.EndingOptions) {
		return Report{}, ErrInvalidFilter
	}
	if report.Segmented {
		report.KPI.CompletionRate = 0
	}
	for _, point := range stored.Trend {
		jalali, conversionErr := persiandate.FromGregorian(point.Date)
		if conversionErr != nil {
			return Report{}, fmt.Errorf("format report trend date: %w", conversionErr)
		}
		report.Trend = append(report.Trend, TrendPoint{
			Date: point.Date.Format(time.DateOnly), DateJalali: jalali.PersianString(),
			Views: point.Views, Starts: point.Starts, Completes: point.Completes,
		})
	}
	for _, value := range stored.Endings {
		version := reportVersionForNode(value.VersionID, value.ID, stored.Versions, versionMap, false)
		report.Endings = append(report.Endings, CountBreakdown{
			VersionNumber: version.VersionNumber, AcrossVersions: value.VersionID == zeroUUID, ID: value.ID, Label: endingLabel(version.Schema, value.ID), Count: value.Count,
		})
	}
	for _, value := range stored.Pages {
		version := reportVersionForNode(value.VersionID, value.ID, stored.Versions, versionMap, true)
		report.PageActivity = append(report.PageActivity, CountBreakdown{
			VersionNumber: version.VersionNumber, AcrossVersions: value.VersionID == zeroUUID, ID: value.ID, Label: pageLabel(version.Schema, value.ID), Count: value.Count,
		})
	}
	report.Choices = buildChoiceBreakdowns(stored.Choices, stored.Versions, versionMap)
	for _, value := range stored.Numbers {
		version := reportVersionForField(value.VersionID, value.FieldID, stored.Versions, versionMap)
		field, _ := schemaField(version.Schema, value.FieldID)
		value.Minimum, value.Maximum, value.Average, value.Median = rounded(value.Minimum), rounded(value.Maximum), rounded(value.Average), rounded(value.Median)
		if value.NPS != nil {
			roundedNPS := rounded(*value.NPS)
			value.NPS = &roundedNPS
		}
		report.Numbers = append(report.Numbers, NumericBreakdown{
			VersionNumber: version.VersionNumber, AcrossVersions: value.VersionID == zeroUUID, FieldID: value.FieldID, FieldLabel: field.Label, FieldType: string(field.Type),
			Count: value.Count, Minimum: value.Minimum, Maximum: value.Maximum, Average: value.Average, Median: value.Median, NPS: value.NPS,
		})
	}
	return report, nil
}

func reportFilterOptions(versions []versionSchema, versionNumber *int64) ([]ReportFieldOption, []ReportEndingOption) {
	fields := make([]ReportFieldOption, 0)
	endings := make([]ReportEndingOption, 0)
	seenFields, seenEndings := make(map[string]bool), make(map[string]bool)
	for _, version := range versions {
		if versionNumber != nil && version.VersionNumber != *versionNumber {
			continue
		}
		for _, page := range version.Schema.Pages {
			for _, field := range page.Fields {
				if seenFields[field.ID] || !reportFilterableField(field.Type) {
					continue
				}
				seenFields[field.ID] = true
				fields = append(fields, ReportFieldOption{ID: field.ID, Label: field.Label, Type: field.Type, Options: field.Options})
			}
		}
		for _, ending := range version.Schema.Endings {
			if !seenEndings[ending.ID] {
				seenEndings[ending.ID] = true
				endings = append(endings, ReportEndingOption{ID: ending.ID, Label: ending.Title})
			}
		}
	}
	return fields, endings
}

func reportFilterableField(fieldType form.FieldType) bool {
	return isResponseField(fieldType) && fieldType != form.FieldFile && fieldType != form.FieldSignature &&
		fieldType != form.FieldFullName && fieldType != form.FieldAddress && fieldType != form.FieldDateRange &&
		fieldType != form.FieldDateTime && fieldType != form.FieldMatrix
}

func validReportSegment(filter ReportFilter, fields []ReportFieldOption, endings []ReportEndingOption) bool {
	if filter.EndingID != "" {
		found := false
		for _, ending := range endings {
			found = found || ending.ID == filter.EndingID
		}
		if !found {
			return false
		}
	}
	if filter.FieldID == "" {
		return true
	}
	for _, field := range fields {
		if field.ID != filter.FieldID {
			continue
		}
		if filter.FieldValue == "" {
			return true
		}
		if field.Type == form.FieldYesNo || field.Type == form.FieldConsent {
			return filter.FieldValue == "true" || filter.FieldValue == "false"
		}
		if len(field.Options) == 0 {
			return true
		}
		for _, option := range field.Options {
			if option.ID == filter.FieldValue {
				return true
			}
		}
		return false
	}
	return false
}

func localDayBounds(from, to *time.Time, location *time.Location) (*time.Time, *time.Time) {
	var fromBound, toBound *time.Time
	if from != nil {
		value := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, location).UTC()
		fromBound = &value
	}
	if to != nil {
		value := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, location).AddDate(0, 0, 1).UTC()
		toBound = &value
	}
	return fromBound, toBound
}

func buildChoiceBreakdowns(values []rawChoice, orderedVersions []versionSchema, versions map[string]versionSchema) []ChoiceBreakdown {
	type grouped struct {
		breakdown ChoiceBreakdown
	}
	groups := make(map[string]*grouped)
	order := make([]string, 0)
	for _, value := range values {
		key := value.VersionID + ":" + value.FieldID
		group := groups[key]
		if group == nil {
			version := reportVersionForField(value.VersionID, value.FieldID, orderedVersions, versions)
			field, _ := schemaField(version.Schema, value.FieldID)
			group = &grouped{breakdown: ChoiceBreakdown{
				VersionNumber: version.VersionNumber, AcrossVersions: value.VersionID == zeroUUID, FieldID: value.FieldID, FieldLabel: field.Label,
				Values: []ChoiceValue{},
			}}
			groups[key] = group
			order = append(order, key)
		}
		version := reportVersionForField(value.VersionID, value.FieldID, orderedVersions, versions)
		field, _ := schemaField(version.Schema, value.FieldID)
		group.breakdown.Values = append(group.breakdown.Values, ChoiceValue{
			Value: value.Value, Label: choiceLabel(field, value.Value), Count: value.Count,
		})
	}
	result := make([]ChoiceBreakdown, 0, len(order))
	for _, key := range order {
		breakdown := groups[key].breakdown
		sort.SliceStable(breakdown.Values, func(i, j int) bool { return breakdown.Values[i].Count > breakdown.Values[j].Count })
		result = append(result, breakdown)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].VersionNumber == result[j].VersionNumber {
			return result[i].FieldLabel < result[j].FieldLabel
		}
		return result[i].VersionNumber > result[j].VersionNumber
	})
	return result
}

func reportVersionForField(versionID, fieldID string, ordered []versionSchema, versions map[string]versionSchema) versionSchema {
	if versionID != zeroUUID {
		return versions[versionID]
	}
	for _, version := range ordered {
		if _, found := schemaField(version.Schema, fieldID); found {
			return version
		}
	}
	return versionSchema{}
}

func reportVersionForNode(versionID, nodeID string, ordered []versionSchema, versions map[string]versionSchema, page bool) versionSchema {
	if versionID != zeroUUID {
		return versions[versionID]
	}
	for _, version := range ordered {
		if page {
			for _, item := range version.Schema.Pages {
				if item.ID == nodeID {
					return version
				}
			}
		} else {
			for _, item := range version.Schema.Endings {
				if item.ID == nodeID {
					return version
				}
			}
		}
	}
	return versionSchema{}
}

func displayValue(field form.Field, value any, location *time.Location) string {
	if value == nil {
		return "بدون پاسخ"
	}
	switch field.Type {
	case form.FieldYesNo:
		if boolean, ok := value.(bool); ok && boolean {
			return "بله"
		}
		return "خیر"
	case form.FieldConsent:
		if boolean, ok := value.(bool); ok && boolean {
			return "پذیرفته شد"
		}
		return "پذیرفته نشد"
	case form.FieldSingleChoice, form.FieldDropdown:
		return choiceLabel(field, fmt.Sprint(value))
	case form.FieldMultiChoice, form.FieldMultiDropdown:
		values := valueStrings(value)
		labels := make([]string, 0, len(values))
		for _, item := range values {
			labels = append(labels, choiceLabel(field, item))
		}
		return strings.Join(labels, "، ")
	case form.FieldPersianDate:
		if converted := jalaliDate(fmt.Sprint(value)); converted != "" {
			return converted
		}
	case form.FieldDateTime:
		if parsed, err := time.Parse(time.RFC3339, fmt.Sprint(value)); err == nil {
			if converted, formatErr := persiandate.FormatTime(parsed, location); formatErr == nil {
				return converted
			}
		}
	case form.FieldDateRange:
		if values, ok := value.(map[string]any); ok {
			from, to := jalaliDate(fmt.Sprint(values["from"])), jalaliDate(fmt.Sprint(values["to"]))
			if from != "" || to != "" {
				return strings.TrimSpace(from + " تا " + to)
			}
		}
	case form.FieldMatrix:
		return displayMatrix(field, value)
	}
	return displayScalar(value)
}

func displayScalar(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case json.Number:
		return persiandate.PersianDigits(typed.String())
	case float64:
		return persiandate.PersianDigits(strconv.FormatFloat(typed, 'f', -1, 64))
	case bool:
		if typed {
			return "بله"
		}
		return "خیر"
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			values = append(values, displayScalar(item))
		}
		return strings.Join(values, "، ")
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		values := make([]string, 0, len(keys))
		for _, key := range keys {
			if rendered := displayScalar(typed[key]); rendered != "" {
				values = append(values, rendered)
			}
		}
		return strings.Join(values, "، ")
	default:
		return fmt.Sprint(value)
	}
}

func displayMatrix(field form.Field, value any) string {
	mapping, ok := value.(map[string]any)
	if !ok || field.Matrix == nil {
		return displayScalar(value)
	}
	rowLabels, columnLabels := map[string]string{}, map[string]string{}
	for _, row := range field.Matrix.Rows {
		rowLabels[row.ID] = row.Label
	}
	for _, column := range field.Matrix.Columns {
		columnLabels[column.ID] = column.Label
	}
	keys := make([]string, 0, len(mapping))
	for key := range mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		selected := valueStrings(mapping[key])
		for index := range selected {
			if label := columnLabels[selected[index]]; label != "" {
				selected[index] = label
			}
		}
		label := rowLabels[key]
		if label == "" {
			label = key
		}
		values = append(values, label+": "+strings.Join(selected, "، "))
	}
	return strings.Join(values, "؛ ")
}

func valueStrings(value any) []string {
	switch typed := value.(type) {
	case []any:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			result = append(result, fmt.Sprint(item))
		}
		return result
	case []string:
		return typed
	case string:
		return []string{typed}
	default:
		return nil
	}
}

func jalaliDate(value string) string {
	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return ""
	}
	date, err := persiandate.FromGregorian(parsed)
	if err != nil {
		return ""
	}
	return date.PersianString()
}

func choiceLabel(field form.Field, value string) string {
	if field.Type == form.FieldYesNo {
		if value == "true" {
			return "بله"
		}
		return "خیر"
	}
	for _, option := range field.Options {
		if option.ID == value {
			return option.Label
		}
	}
	return value
}

func endingLabel(schema form.Schema, endingID string) string {
	for _, ending := range schema.Endings {
		if ending.ID == endingID {
			return ending.Title
		}
	}
	return "پایان بدون عنوان"
}

func pageLabel(schema form.Schema, pageID string) string {
	for _, page := range schema.Pages {
		if page.ID == pageID {
			return page.Title
		}
	}
	return "صفحهٔ حذف‌شده"
}

func schemaField(schema form.Schema, fieldID string) (form.Field, bool) {
	for _, page := range schema.Pages {
		for _, field := range page.Fields {
			if field.ID == fieldID {
				return field, true
			}
		}
	}
	return form.Field{ID: fieldID, Label: "پرسش حذف‌شده"}, false
}

func isResponseField(fieldType form.FieldType) bool {
	return fieldType != form.FieldHeading && fieldType != form.FieldDescription && fieldType != form.FieldDivider &&
		fieldType != form.FieldImage && fieldType != form.FieldSpacer
}

func rounded(value float64) float64 { return math.Round(value*100) / 100 }
