package project

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"forms/internal/normalize"
)

type Status string

const (
	StatusDraft     Status = "draft"
	StatusPublished Status = "published"
	StatusClosed    Status = "closed"
	StatusTrash     Status = "trash"
)

var (
	ErrNotFound        = errors.New("پروژه پیدا نشد")
	ErrInvalidProject  = errors.New("اطلاعات پروژه معتبر نیست")
	ErrDuplicateSlug   = errors.New("این نشانی قبلاً استفاده شده است")
	ErrSlugImmutable   = errors.New("نشانی عمومی پروژه پس از نخستین انتشار قابل تغییر نیست")
	projectSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,61}[a-z0-9])?$`)
)

type Project struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Slug            string     `json:"slug"`
	Status          Status     `json:"status"`
	CreatedBy       string     `json:"created_by"`
	UpdatedBy       string     `json:"updated_by"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DeletedAt       *time.Time `json:"deleted_at,omitempty"`
	ActiveVersionID *string    `json:"active_version_id,omitempty"`
}

type CreateInput struct {
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

type UpdateInput struct {
	Title string `json:"title"`
	Slug  string `json:"slug"`
}

func normalizeInput(title, slug string) (string, string, error) {
	title = strings.TrimSpace(title)
	slug = normalize.Slug(slug)
	if title == "" || utf8.RuneCountInString(title) > 200 || !projectSlugPattern.MatchString(slug) {
		return "", "", ErrInvalidProject
	}
	return title, slug, nil
}

// NormalizeCreateInput applies the same title and slug contract used by normal
// project creation. Importers use it before opening a database transaction so
// package imports cannot bypass project naming rules.
func NormalizeCreateInput(input CreateInput) (CreateInput, error) {
	title, slug, err := normalizeInput(input.Title, input.Slug)
	if err != nil {
		return CreateInput{}, err
	}
	return CreateInput{Title: title, Slug: slug}, nil
}
