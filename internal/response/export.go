package response

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"strconv"
	"strings"
	"time"
	"unicode"

	"forms/internal/auth"
	"forms/internal/form"
	"forms/internal/persiandate"
)

const MaxExportRows int64 = 100_000

type ExportFormat string

const (
	ExportCSV  ExportFormat = "csv"
	ExportJSON ExportFormat = "json"
	ExportXLSX ExportFormat = "xlsx"
)

type ExportOptions struct {
	Format ExportFormat
	BOM    bool
}

type PreparedExport struct {
	service      *Service
	projectID    string
	projectTitle string
	filter       Filter
	options      ExportOptions
	count        int64
	versions     []versionSchema
	versionMap   map[string]versionSchema
	fields       []form.Field
	generatedAt  time.Time
}

func (s *Service) PrepareExport(ctx context.Context, _ auth.User, projectID string, filter Filter, options ExportOptions) (*PreparedExport, error) {
	filter, err := normalizeFilter(projectID, filter)
	if err != nil {
		return nil, err
	}
	filter.From, filter.To = localDayBounds(filter.From, filter.To, s.location)
	if options.Format != ExportCSV && options.Format != ExportJSON && options.Format != ExportXLSX {
		return nil, ErrExportFormat
	}
	title, versions, count, err := s.store.ExportContext(ctx, projectID, filter)
	if err != nil {
		return nil, err
	}
	if count > MaxExportRows {
		return nil, ErrExportTooLarge
	}
	versionMap := make(map[string]versionSchema, len(versions))
	fieldsByID := make(map[string]form.Field)
	orderedIDs := make([]string, 0)
	for _, version := range versions {
		if filter.VersionNumber != nil && version.VersionNumber != *filter.VersionNumber {
			continue
		}
		versionMap[version.ID] = version
		for _, page := range version.Schema.Pages {
			for _, field := range page.Fields {
				if !isResponseField(field.Type) {
					continue
				}
				if _, exists := fieldsByID[field.ID]; !exists {
					fieldsByID[field.ID] = field
					orderedIDs = append(orderedIDs, field.ID)
				}
			}
		}
	}
	fields := make([]form.Field, 0, len(orderedIDs))
	for _, fieldID := range orderedIDs {
		fields = append(fields, fieldsByID[fieldID])
	}
	return &PreparedExport{
		service: s, projectID: projectID, projectTitle: title, filter: filter, options: options,
		count: count, versions: versions, versionMap: versionMap, fields: fields, generatedAt: s.now(),
	}, nil
}

func (e *PreparedExport) ContentType() string {
	switch e.options.Format {
	case ExportCSV:
		return "text/csv; charset=utf-8"
	case ExportJSON:
		return "application/json; charset=utf-8"
	case ExportXLSX:
		return "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	default:
		return "application/octet-stream"
	}
}

func (e *PreparedExport) Filename() string {
	extension := string(e.options.Format)
	return "responses-" + e.generatedAt.Format("20060102-150405") + "." + extension
}

func (e *PreparedExport) ContentDisposition() string {
	return mime.FormatMediaType("attachment", map[string]string{"filename": e.Filename()})
}

func (e *PreparedExport) Count() int64 { return e.count }

func (e *PreparedExport) Write(ctx context.Context, destination io.Writer) error {
	switch e.options.Format {
	case ExportCSV:
		return e.writeCSV(ctx, destination)
	case ExportJSON:
		return e.writeJSON(ctx, destination)
	case ExportXLSX:
		return e.writeXLSX(ctx, destination)
	default:
		return ErrExportFormat
	}
}

func (e *PreparedExport) writeCSV(ctx context.Context, destination io.Writer) error {
	if e.options.BOM {
		if _, err := io.WriteString(destination, "\ufeff"); err != nil {
			return fmt.Errorf("write CSV BOM: %w", err)
		}
	}
	writer := csv.NewWriter(destination)
	headers := []string{"شناسهٔ پاسخ", "نسخه", "زمان ثبت (شمسی)", "زمان ثبت (ISO)", "پایان", "مدت تکمیل (ثانیه)", "وضعیت"}
	for _, field := range e.fields {
		headers = append(headers, uniqueFieldHeader(field, e.fields))
	}
	for index := range headers {
		headers[index] = formulaSafe(headers[index])
	}
	if err := writer.Write(headers); err != nil {
		return fmt.Errorf("write CSV header: %w", err)
	}
	err := e.service.store.StreamExport(ctx, e.projectID, e.filter, func(record exportRecord) error {
		jalali, formatErr := persiandate.FormatTime(record.SubmittedAt, e.service.location)
		if formatErr != nil {
			return formatErr
		}
		version := e.versionMap[record.VersionID]
		row := []string{
			record.PublicID,
			strconv.FormatInt(record.VersionNumber, 10),
			jalali,
			record.SubmittedAt.UTC().Format(time.RFC3339),
			endingLabel(version.Schema, record.EndingID),
			strconv.FormatInt(record.DurationSeconds, 10),
			map[bool]string{true: "حذف‌شده", false: "فعال"}[record.DeletedAt != nil],
		}
		for _, field := range e.fields {
			text, sourceText := e.csvField(record, field.ID)
			if sourceText {
				text = formulaSafe(text)
			}
			row = append(row, text)
		}
		if err := writer.Write(row); err != nil {
			return fmt.Errorf("write CSV response: %w", err)
		}
		return nil
	})
	writer.Flush()
	if err != nil {
		return err
	}
	if err := writer.Error(); err != nil {
		return fmt.Errorf("flush CSV export: %w", err)
	}
	return nil
}

func (e *PreparedExport) csvField(record exportRecord, fieldID string) (string, bool) {
	version := e.versionMap[record.VersionID]
	field, exists := schemaField(version.Schema, fieldID)
	if !exists {
		return "", true
	}
	if files := record.Files[fieldID]; len(files) > 0 {
		names := make([]string, 0, len(files))
		for _, file := range files {
			names = append(names, file.Name)
		}
		return strings.Join(names, " | "), true
	}
	answer, exists := record.Answers[fieldID]
	if !exists {
		return "", true
	}
	textual := !isNumericType(answer.Type) && answer.Type != form.FieldYesNo && answer.Type != form.FieldConsent
	return displayValue(field, answer.Value, e.service.location), textual
}

type jsonExportAnswer struct {
	FieldID      string         `json:"field_id"`
	Label        string         `json:"label"`
	Type         form.FieldType `json:"type"`
	Value        any            `json:"value,omitempty"`
	DisplayValue string         `json:"display_value"`
	Files        []File         `json:"files,omitempty"`
}

type jsonExportRecord struct {
	ID                string             `json:"id"`
	PublicID          string             `json:"public_id"`
	VersionID         string             `json:"version_id"`
	VersionNumber     int64              `json:"version_number"`
	SubmittedAt       time.Time          `json:"submitted_at"`
	SubmittedAtJalali string             `json:"submitted_at_jalali"`
	EndingID          string             `json:"ending_id"`
	EndingTitle       string             `json:"ending_title"`
	DurationSeconds   int64              `json:"duration_seconds"`
	DeletedAt         *time.Time         `json:"deleted_at,omitempty"`
	Answers           []jsonExportAnswer `json:"answers"`
}

func (e *PreparedExport) writeJSON(ctx context.Context, destination io.Writer) error {
	header := struct {
		ExportVersion int       `json:"export_version"`
		GeneratedAt   time.Time `json:"generated_at"`
		ProjectID     string    `json:"project_id"`
		ProjectTitle  string    `json:"project_title"`
		ResponseCount int64     `json:"response_count"`
	}{1, e.generatedAt, e.projectID, e.projectTitle, e.count}
	encodedHeader, err := json.Marshal(header)
	if err != nil {
		return fmt.Errorf("encode JSON export header: %w", err)
	}
	if _, err := destination.Write(encodedHeader[:len(encodedHeader)-1]); err != nil {
		return fmt.Errorf("write JSON export header: %w", err)
	}
	if _, err := io.WriteString(destination, `,"responses":[`); err != nil {
		return fmt.Errorf("write JSON response opening: %w", err)
	}
	first := true
	err = e.service.store.StreamExport(ctx, e.projectID, e.filter, func(record exportRecord) error {
		if !first {
			if _, writeErr := io.WriteString(destination, ","); writeErr != nil {
				return writeErr
			}
		}
		first = false
		converted, conversionErr := e.jsonRecord(record)
		if conversionErr != nil {
			return conversionErr
		}
		if encodeErr := json.NewEncoder(destination).Encode(converted); encodeErr != nil {
			return fmt.Errorf("encode JSON response: %w", encodeErr)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if _, err := io.WriteString(destination, "]}"); err != nil {
		return fmt.Errorf("close JSON export: %w", err)
	}
	return nil
}

func (e *PreparedExport) jsonRecord(record exportRecord) (jsonExportRecord, error) {
	version := e.versionMap[record.VersionID]
	jalali, err := persiandate.FormatTime(record.SubmittedAt, e.service.location)
	if err != nil {
		return jsonExportRecord{}, err
	}
	result := jsonExportRecord{
		ID: record.ID, PublicID: record.PublicID, VersionID: record.VersionID, VersionNumber: record.VersionNumber,
		SubmittedAt: record.SubmittedAt, SubmittedAtJalali: jalali, EndingID: record.EndingID,
		EndingTitle: endingLabel(version.Schema, record.EndingID), DurationSeconds: record.DurationSeconds,
		DeletedAt: record.DeletedAt, Answers: []jsonExportAnswer{},
	}
	for _, page := range version.Schema.Pages {
		for _, field := range page.Fields {
			if !isResponseField(field.Type) {
				continue
			}
			answer, present := record.Answers[field.ID]
			files := record.Files[field.ID]
			if !present && len(files) == 0 {
				continue
			}
			value := any(nil)
			display := ""
			if present {
				value = answer.Value
				display = displayValue(field, answer.Value, e.service.location)
			} else if len(files) == 1 {
				display = files[0].Name
			} else {
				display = persiandate.PersianDigits(strconv.Itoa(len(files))) + " فایل"
			}
			result.Answers = append(result.Answers, jsonExportAnswer{
				FieldID: field.ID, Label: field.Label, Type: field.Type, Value: value, DisplayValue: display, Files: files,
			})
		}
	}
	return result, nil
}

func uniqueFieldHeader(field form.Field, fields []form.Field) string {
	duplicates := 0
	for _, candidate := range fields {
		if candidate.Label == field.Label {
			duplicates++
		}
	}
	if duplicates < 2 {
		return field.Label
	}
	suffix := field.ID
	if len(suffix) > 8 {
		suffix = suffix[len(suffix)-8:]
	}
	return field.Label + " (" + suffix + ")"
}

func formulaSafe(value string) string {
	for _, character := range value {
		if unicode.IsSpace(character) {
			if character == '\t' || character == '\r' || character == '\n' {
				return "'" + value
			}
			continue
		}
		if character == '=' || character == '+' || character == '-' || character == '@' {
			return "'" + value
		}
		break
	}
	return value
}

func isNumericType(fieldType form.FieldType) bool {
	return fieldType == form.FieldNumber || fieldType == form.FieldMoney || fieldType == form.FieldPercent ||
		fieldType == form.FieldRating || fieldType == form.FieldScale || fieldType == form.FieldNPS || fieldType == form.FieldCalculated
}
