package form

import (
	"context"
	"errors"
	"strings"

	"forms/internal/auth"
)

type Service struct{ store *Store }

var ErrInvalidPublishKey = errors.New("کلید یکتایی انتشار معتبر نیست")

func NewService(store *Store) *Service { return &Service{store: store} }

func (s *Service) GetDraft(ctx context.Context, _ auth.User, projectID string) (Draft, error) {
	return s.store.Get(ctx, projectID)
}

func (s *Service) SaveDraft(ctx context.Context, actor auth.User, projectID string, expectedRevision int64, schema Schema) (Draft, error) {
	if !actor.Role.CanEditProjects() {
		return Draft{}, auth.ErrForbidden
	}
	if expectedRevision < 1 {
		return Draft{}, ErrInvalidSchema
	}
	return s.store.Save(ctx, projectID, actor.ID, expectedRevision, schema)
}

func (s *Service) ValidateForPublish(ctx context.Context, actor auth.User, projectID string) (Draft, error) {
	if !actor.Role.CanEditProjects() {
		return Draft{}, auth.ErrForbidden
	}
	draft, err := s.store.Get(ctx, projectID)
	if err != nil {
		return Draft{}, err
	}
	if err := draft.Schema.ValidateForPublish(); err != nil {
		return Draft{}, err
	}
	return draft, nil
}

func (s *Service) Publish(ctx context.Context, actor auth.User, projectID string, expectedRevision int64, idempotencyKey string) (Version, error) {
	if !actor.Role.CanEditProjects() {
		return Version{}, auth.ErrForbidden
	}
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if len(idempotencyKey) < 16 || len(idempotencyKey) > 200 {
		return Version{}, ErrInvalidPublishKey
	}
	return s.store.Publish(ctx, projectID, actor.ID, expectedRevision, idempotencyKey)
}

func (s *Service) ListVersions(ctx context.Context, _ auth.User, projectID string) ([]Version, error) {
	return s.store.ListVersions(ctx, projectID)
}
