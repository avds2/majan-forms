package project

import (
	"context"

	"forms/internal/auth"
	"forms/internal/form"
)

type Service struct{ store *Store }

func NewService(store *Store) *Service { return &Service{store: store} }

func (s *Service) List(ctx context.Context, actor auth.User, trash bool) ([]Project, error) {
	if trash && actor.Role != auth.RoleAdmin {
		return nil, auth.ErrForbidden
	}
	return s.store.List(ctx, trash)
}

func (s *Service) Get(ctx context.Context, actor auth.User, projectID string) (Project, error) {
	return s.store.Get(ctx, projectID, actor.Role == auth.RoleAdmin)
}

func (s *Service) Create(ctx context.Context, actor auth.User, input CreateInput) (Project, error) {
	if !actor.Role.CanEditProjects() {
		return Project{}, auth.ErrForbidden
	}
	title, slug, err := normalizeInput(input.Title, input.Slug)
	if err != nil {
		return Project{}, err
	}
	schema, err := form.NewEmpty(title)
	if err != nil {
		return Project{}, err
	}
	encoded, checksum, err := form.EncodeAndChecksum(schema)
	if err != nil {
		return Project{}, err
	}
	return s.store.CreateWithDraft(ctx, actor.ID, title, slug, encoded, checksum)
}

func (s *Service) Update(ctx context.Context, actor auth.User, projectID string, input UpdateInput) (Project, error) {
	if !actor.Role.CanEditProjects() {
		return Project{}, auth.ErrForbidden
	}
	title, slug, err := normalizeInput(input.Title, input.Slug)
	if err != nil {
		return Project{}, err
	}
	return s.store.Update(ctx, actor.ID, projectID, title, slug)
}

func (s *Service) Trash(ctx context.Context, actor auth.User, projectID string) (Project, error) {
	if !actor.Role.CanEditProjects() {
		return Project{}, auth.ErrForbidden
	}
	return s.store.SetTrash(ctx, actor.ID, projectID, true)
}

func (s *Service) Restore(ctx context.Context, actor auth.User, projectID string) (Project, error) {
	if actor.Role != auth.RoleAdmin {
		return Project{}, auth.ErrForbidden
	}
	return s.store.SetTrash(ctx, actor.ID, projectID, false)
}

func (s *Service) Close(ctx context.Context, actor auth.User, projectID string) (Project, error) {
	if !actor.Role.CanEditProjects() {
		return Project{}, auth.ErrForbidden
	}
	return s.store.SetClosed(ctx, actor.ID, projectID, true)
}

func (s *Service) Reopen(ctx context.Context, actor auth.User, projectID string) (Project, error) {
	if !actor.Role.CanEditProjects() {
		return Project{}, auth.ErrForbidden
	}
	return s.store.SetClosed(ctx, actor.ID, projectID, false)
}
