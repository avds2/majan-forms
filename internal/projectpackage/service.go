package projectpackage

import (
	"context"
	"fmt"
	"sort"
	"time"

	"forms/internal/auth"
	"forms/internal/project"
	"forms/internal/version"
)

type Service struct {
	store *Store
	now   func() time.Time
}

func NewService(store *Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (s *Service) Export(ctx context.Context, _ auth.User, projectID string, selectedVersions []int64) (Export, error) {
	source, err := s.store.Load(ctx, projectID)
	if err != nil {
		return Export{}, err
	}
	source.Versions, err = selectVersions(source.Versions, selectedVersions)
	if err != nil {
		return Export{}, err
	}
	createdAt := s.now().UTC()
	raw, _, err := buildArchive(source, version.Current().Version, createdAt)
	if err != nil {
		return Export{}, err
	}
	decoded, err := parseArchive(raw)
	if err != nil {
		return Export{}, fmt.Errorf("verify generated project package: %w", err)
	}
	return Export{
		Bytes: raw, Filename: source.Project.Slug + "-project-package-v1.zip",
		SHA256: decoded.SHA256, Preview: previewOf(decoded),
	}, nil
}

func (s *Service) Preview(_ context.Context, actor auth.User, raw []byte) (Preview, error) {
	if !actor.Role.CanEditProjects() {
		return Preview{}, auth.ErrForbidden
	}
	decoded, err := parseArchive(raw)
	if err != nil {
		return Preview{}, err
	}
	return previewOf(decoded), nil
}

func (s *Service) Import(ctx context.Context, actor auth.User, input ImportInput, raw []byte) (ImportResult, error) {
	if !actor.Role.CanEditProjects() {
		return ImportResult{}, auth.ErrForbidden
	}
	validated, err := project.NormalizeCreateInput(project.CreateInput{Title: input.Title, Slug: input.Slug})
	if err != nil {
		return ImportResult{}, err
	}
	decoded, err := parseArchive(raw)
	if err != nil {
		return ImportResult{}, err
	}
	imported, err := s.store.Import(ctx, actor.ID, validated, decoded)
	if err != nil {
		return ImportResult{}, err
	}
	return ImportResult{Project: imported, ImportedVersions: len(decoded.Versions), PackageSHA256: decoded.SHA256}, nil
}

func selectVersions(available []VersionDocument, selected []int64) ([]VersionDocument, error) {
	if len(selected) == 0 {
		return available, nil
	}
	wanted := make(map[int64]struct{}, len(selected))
	for _, number := range selected {
		if number < 1 {
			return nil, ErrInvalidSelection
		}
		if _, duplicate := wanted[number]; duplicate {
			return nil, ErrInvalidSelection
		}
		wanted[number] = struct{}{}
	}
	result := make([]VersionDocument, 0, len(wanted))
	for _, item := range available {
		if _, include := wanted[item.VersionNumber]; include {
			result = append(result, item)
			delete(wanted, item.VersionNumber)
		}
	}
	if len(wanted) != 0 {
		return nil, ErrInvalidSelection
	}
	sort.Slice(result, func(i, j int) bool { return result[i].VersionNumber < result[j].VersionNumber })
	return result, nil
}

func previewOf(decoded decodedPackage) Preview {
	preview := Preview{
		Format: decoded.Manifest.Format, FormatVersion: decoded.Manifest.FormatVersion,
		AppVersion: decoded.Manifest.AppVersion, CreatedAt: decoded.Manifest.CreatedAt,
		PackageSHA256: decoded.SHA256, SourceProjectID: decoded.Manifest.SourceProjectID,
		SourceTitle: decoded.Project.Title, SourceSlug: decoded.Project.Slug,
		DraftRevision: decoded.Draft.Revision, PageCount: decoded.Manifest.Counts.Pages,
		FieldCount: decoded.Manifest.Counts.Fields, EndingCount: decoded.Manifest.Counts.Endings,
		VersionCount: len(decoded.Versions), AssetCount: decoded.Manifest.Counts.Assets,
		Versions: make([]VersionPreview, 0, len(decoded.Versions)), IncludesResponses: false,
		ImportedProjectStatus: project.StatusDraft,
	}
	for _, item := range decoded.Versions {
		preview.Versions = append(preview.Versions, VersionPreview{
			VersionNumber: item.VersionNumber, PublishedAt: item.PublishedAt, Checksum: item.Checksum,
		})
	}
	return preview
}
