// Package response provides the authenticated response browser, reporting and
// export read model. Published form schemas remain the source of labels so old
// submissions are always rendered with the version that collected them.
package response

import (
	"errors"
	"time"

	"forms/internal/form"
)

var (
	ErrNotFound       = errors.New("پاسخ پیدا نشد")
	ErrInvalidFilter  = errors.New("فیلتر پاسخ‌ها معتبر نیست")
	ErrEmptySelection = errors.New("دست‌کم یک پاسخ را انتخاب کنید")
	ErrExportTooLarge = errors.New("تعداد پاسخ‌ها از سقف خروجی بیشتر است؛ بازه یا فیلتر را محدود کنید")
	ErrExportFormat   = errors.New("قالب خروجی پشتیبانی نمی‌شود")
)

type DeletedState string

const (
	DeletedActive DeletedState = "active"
	DeletedOnly   DeletedState = "deleted"
	DeletedAll    DeletedState = "all"
)

type Sort string

const (
	SortNewest        Sort = "newest"
	SortOldest        Sort = "oldest"
	SortDurationLong  Sort = "duration_desc"
	SortDurationShort Sort = "duration_asc"
)

type Filter struct {
	Page          int
	PageSize      int
	Search        string
	VersionNumber *int64
	EndingID      string
	From          *time.Time
	To            *time.Time
	Deleted       DeletedState
	Sort          Sort
}

type Summary struct {
	ID                string     `json:"id"`
	PublicID          string     `json:"public_id"`
	VersionID         string     `json:"version_id"`
	VersionNumber     int64      `json:"version_number"`
	EndingID          string     `json:"ending_id"`
	EndingTitle       string     `json:"ending_title"`
	StartedAt         time.Time  `json:"started_at"`
	SubmittedAt       time.Time  `json:"submitted_at"`
	SubmittedAtJalali string     `json:"submitted_at_jalali"`
	DurationSeconds   int64      `json:"duration_seconds"`
	DeletedAt         *time.Time `json:"deleted_at,omitempty"`
	DeletedAtJalali   string     `json:"deleted_at_jalali,omitempty"`
}

type Pagination struct {
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
}

type ListResult struct {
	Items      []Summary  `json:"items"`
	Pagination Pagination `json:"pagination"`
}

type File struct {
	FieldID      string `json:"-"`
	ID           string `json:"id"`
	Name         string `json:"name"`
	SizeBytes    int64  `json:"size_bytes"`
	MIME         string `json:"mime"`
	Checksum     string `json:"checksum"`
	DownloadPath string `json:"download_path"`
}

type Answer struct {
	FieldID      string         `json:"field_id"`
	FieldType    form.FieldType `json:"field_type"`
	Label        string         `json:"label"`
	PageTitle    string         `json:"page_title"`
	Value        any            `json:"value,omitempty"`
	DisplayValue string         `json:"display_value"`
	Files        []File         `json:"files,omitempty"`
}

type Detail struct {
	Summary
	ProjectID    string   `json:"project_id"`
	ProjectTitle string   `json:"project_title"`
	Answers      []Answer `json:"answers"`
}

type VersionOption struct {
	ID            string    `json:"id"`
	VersionNumber int64     `json:"version_number"`
	PublishedAt   time.Time `json:"published_at"`
}

type ReportFilter struct {
	VersionNumber *int64
	EndingID      string
	FieldID       string
	FieldValue    string
	From          *time.Time
	To            *time.Time
	fromDate      string
	toDate        string
	timezone      string
}

type ReportKPI struct {
	Views                  int64   `json:"views"`
	Starts                 int64   `json:"starts"`
	Completes              int64   `json:"completes"`
	CompletionRate         float64 `json:"completion_rate"`
	AverageDurationSeconds float64 `json:"average_duration_seconds"`
}

type TrendPoint struct {
	Date       string `json:"date"`
	DateJalali string `json:"date_jalali"`
	Views      int64  `json:"views"`
	Starts     int64  `json:"starts"`
	Completes  int64  `json:"completes"`
}

type CountBreakdown struct {
	VersionNumber  int64  `json:"version_number"`
	AcrossVersions bool   `json:"across_versions,omitempty"`
	ID             string `json:"id"`
	Label          string `json:"label"`
	Count          int64  `json:"count"`
}

type ChoiceValue struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int64  `json:"count"`
}

type ChoiceBreakdown struct {
	VersionNumber  int64         `json:"version_number"`
	AcrossVersions bool          `json:"across_versions,omitempty"`
	FieldID        string        `json:"field_id"`
	FieldLabel     string        `json:"field_label"`
	Values         []ChoiceValue `json:"values"`
}

type NumericBreakdown struct {
	VersionNumber  int64    `json:"version_number"`
	AcrossVersions bool     `json:"across_versions,omitempty"`
	FieldID        string   `json:"field_id"`
	FieldLabel     string   `json:"field_label"`
	FieldType      string   `json:"field_type"`
	Count          int64    `json:"count"`
	Minimum        float64  `json:"minimum"`
	Maximum        float64  `json:"maximum"`
	Average        float64  `json:"average"`
	Median         float64  `json:"median"`
	NPS            *float64 `json:"nps,omitempty"`
}

type ReportFieldOption struct {
	ID      string         `json:"id"`
	Label   string         `json:"label"`
	Type    form.FieldType `json:"type"`
	Options []form.Option  `json:"options,omitempty"`
}

type ReportEndingOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Report struct {
	ProjectID       string               `json:"project_id"`
	ProjectTitle    string               `json:"project_title"`
	GeneratedAt     time.Time            `json:"generated_at"`
	GeneratedJalali string               `json:"generated_at_jalali"`
	KPI             ReportKPI            `json:"kpi"`
	Trend           []TrendPoint         `json:"trend"`
	Endings         []CountBreakdown     `json:"endings"`
	PageActivity    []CountBreakdown     `json:"page_activity"`
	Choices         []ChoiceBreakdown    `json:"choices"`
	Numbers         []NumericBreakdown   `json:"numbers"`
	Versions        []VersionOption      `json:"versions"`
	Fields          []ReportFieldOption  `json:"fields"`
	EndingOptions   []ReportEndingOption `json:"ending_options"`
	Segmented       bool                 `json:"segmented"`
}

type storedSubmission struct {
	Summary
	ProjectID    string
	ProjectTitle string
	Schema       form.Schema
}

type storedAnswer struct {
	FieldID   string
	FieldType form.FieldType
	Value     any
}

type versionSchema struct {
	ID            string
	VersionNumber int64
	PublishedAt   time.Time
	Schema        form.Schema
}

type rawTrend struct {
	Date                     time.Time
	Views, Starts, Completes int64
}

type rawCount struct {
	VersionID string
	ID        string
	Count     int64
}

type rawChoice struct {
	VersionID string
	FieldID   string
	Value     string
	Count     int64
}

type rawNumber struct {
	VersionID string
	FieldID   string
	Count     int64
	Minimum   float64
	Maximum   float64
	Average   float64
	Median    float64
	NPS       *float64
}

type storedReport struct {
	ProjectTitle string
	Versions     []versionSchema
	KPI          ReportKPI
	Trend        []rawTrend
	Endings      []rawCount
	Pages        []rawCount
	Choices      []rawChoice
	Numbers      []rawNumber
}

type exportAnswer struct {
	Type  form.FieldType `json:"type"`
	Value any            `json:"value"`
}

type exportRecord struct {
	ID              string
	PublicID        string
	VersionID       string
	VersionNumber   int64
	EndingID        string
	StartedAt       time.Time
	SubmittedAt     time.Time
	DurationSeconds int64
	DeletedAt       *time.Time
	Answers         map[string]exportAnswer
	Files           map[string][]File
}
