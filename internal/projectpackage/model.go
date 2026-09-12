// Package projectpackage implements the bounded, portable project-package-v1
// format. Response rows and respondent uploads are intentionally never part of
// this format.
package projectpackage

import (
	"errors"
	"time"

	"forms/internal/form"
	"forms/internal/project"
)

const (
	Format                    = "project-package-v1"
	FormatVersion             = 1
	MaxArchiveBytes     int64 = 32 << 20
	maxExpandedBytes          = 64 << 20
	maxEntryBytes             = 8 << 20
	maxFileCount              = 256
	maxCompressionRatio       = 200
)

var (
	ErrInvalidPackage     = errors.New("بستهٔ پروژه معتبر نیست")
	ErrPackageTooLarge    = errors.New("حجم بستهٔ پروژه بیش از حد مجاز است")
	ErrUnsupportedFormat  = errors.New("نسخهٔ بستهٔ پروژه پشتیبانی نمی‌شود")
	ErrChecksumMismatch   = errors.New("صحت یکی از فایل‌های بسته تأیید نشد")
	ErrInvalidSelection   = errors.New("نسخه‌های انتخاب‌شده برای خروجی معتبر نیستند")
	ErrResponsesForbidden = errors.New("بستهٔ پروژه نباید شامل پاسخ‌ها یا فایل پاسخ‌دهندگان باشد")
)

type Manifest struct {
	Format          string         `json:"format"`
	FormatVersion   int            `json:"format_version"`
	AppVersion      string         `json:"app_version"`
	CreatedAt       time.Time      `json:"created_at"`
	SourceProjectID string         `json:"source_project_id"`
	Files           []ManifestFile `json:"files"`
	Counts          Counts         `json:"counts"`
}

type ManifestFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type Counts struct {
	Versions int `json:"versions"`
	Assets   int `json:"assets"`
	Pages    int `json:"pages"`
	Fields   int `json:"fields"`
	Endings  int `json:"endings"`
}

type ProjectDocument struct {
	Title               string `json:"title"`
	Slug                string `json:"slug"`
	ActiveVersionNumber *int64 `json:"active_version_number,omitempty"`
}

type DraftDocument struct {
	Revision int64       `json:"revision"`
	Checksum string      `json:"checksum"`
	Schema   form.Schema `json:"schema"`
}

type VersionDocument struct {
	VersionNumber int64       `json:"version_number"`
	Checksum      string      `json:"checksum"`
	PublishedAt   time.Time   `json:"published_at"`
	Schema        form.Schema `json:"schema"`
	sourceID      string
}

type VersionPreview struct {
	VersionNumber int64     `json:"version_number"`
	PublishedAt   time.Time `json:"published_at"`
	Checksum      string    `json:"checksum"`
}

type Preview struct {
	Format                string           `json:"format"`
	FormatVersion         int              `json:"format_version"`
	AppVersion            string           `json:"app_version"`
	CreatedAt             time.Time        `json:"created_at"`
	PackageSHA256         string           `json:"package_sha256"`
	SourceProjectID       string           `json:"source_project_id"`
	SourceTitle           string           `json:"source_title"`
	SourceSlug            string           `json:"source_slug"`
	DraftRevision         int64            `json:"draft_revision"`
	PageCount             int              `json:"page_count"`
	FieldCount            int              `json:"field_count"`
	EndingCount           int              `json:"ending_count"`
	VersionCount          int              `json:"version_count"`
	AssetCount            int              `json:"asset_count"`
	Versions              []VersionPreview `json:"versions"`
	IncludesResponses     bool             `json:"includes_responses"`
	ImportedProjectStatus project.Status   `json:"imported_project_status"`
}

type Export struct {
	Bytes    []byte
	Filename string
	SHA256   string
	Preview  Preview
}

type ImportInput struct {
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

type ImportResult struct {
	Project          project.Project `json:"project"`
	ImportedVersions int             `json:"imported_versions"`
	PackageSHA256    string          `json:"package_sha256"`
}

type sourceData struct {
	Project  project.Project
	Draft    DraftDocument
	Versions []VersionDocument
}

type decodedPackage struct {
	Manifest Manifest
	Project  ProjectDocument
	Draft    DraftDocument
	Versions []VersionDocument
	SHA256   string
}
