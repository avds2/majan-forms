package auth

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"forms/internal/id"
)

type Service struct {
	store           *Store
	passwordParams  PasswordParams
	idleTimeout     time.Duration
	absoluteTimeout time.Duration
	dummyHash       string
	now             func() time.Time
	verifySlots     chan struct{}
}

func NewService(store *Store, idleTimeout, absoluteTimeout time.Duration) (*Service, error) {
	params := DefaultPasswordParams()
	dummyHash, err := HashPassword("forms-dummy-password-value", params)
	if err != nil {
		return nil, err
	}
	return &Service{
		store:           store,
		passwordParams:  params,
		idleTimeout:     idleTimeout,
		absoluteTimeout: absoluteTimeout,
		dummyHash:       dummyHash,
		now:             func() time.Time { return time.Now().UTC() },
		verifySlots:     make(chan struct{}, 2),
	}, nil
}

func (s *Service) Login(ctx context.Context, identifier, password, userAgent string) (User, string, error) {
	if err := s.acquirePasswordSlot(ctx); err != nil {
		return User{}, "", err
	}
	defer s.releasePasswordSlot()
	identifier = strings.TrimSpace(identifier)
	user, encodedHash, err := s.store.FindByLogin(ctx, identifier)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			_, _, _ = VerifyPassword(password, s.dummyHash)
			s.store.RecordLoginFailure(ctx, identifier)
			return User{}, "", ErrInvalidCredentials
		}
		return User{}, "", err
	}
	valid, currentParams, err := VerifyPassword(password, encodedHash)
	if err != nil {
		return User{}, "", fmt.Errorf("verify stored password: %w", err)
	}
	if !valid || !user.Active {
		s.store.RecordLoginFailure(ctx, identifier)
		return User{}, "", ErrInvalidCredentials
	}
	token, err := id.Token(32)
	if err != nil {
		return User{}, "", fmt.Errorf("create session token: %w", err)
	}
	hash := sha256.Sum256([]byte(token))
	userAgent = truncateUTF8(userAgent, 255)
	if err := s.store.CreateSession(ctx, user, hash[:], s.idleTimeout, s.absoluteTimeout, userAgent, s.now()); err != nil {
		return User{}, "", err
	}
	if NeedsRehash(currentParams, s.passwordParams) {
		// Rehash is deliberately deferred to an explicit password change until a
		// transactional compare-and-swap update is added; authentication succeeds.
	}
	return user, token, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (User, error) {
	if token == "" {
		return User{}, ErrInvalidSession
	}
	hash := sha256.Sum256([]byte(token))
	return s.store.AuthenticateSession(ctx, hash[:], s.idleTimeout, s.now())
}

func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	hash := sha256.Sum256([]byte(token))
	return s.store.DeleteSession(ctx, hash[:])
}

func (s *Service) ListUsers(ctx context.Context, actor User) ([]User, error) {
	if !actor.Role.CanManageUsers() {
		return nil, ErrForbidden
	}
	return s.store.ListUsers(ctx)
}

func (s *Service) CreateUser(ctx context.Context, actor User, input CreateUserInput) (User, error) {
	if !actor.Role.CanManageUsers() {
		return User{}, ErrForbidden
	}
	return s.createUser(ctx, &actor.ID, input, "user.created", false)
}

func (s *Service) CreateInitialAdmin(ctx context.Context, input CreateUserInput) (User, error) {
	input.Role = RoleAdmin
	return s.createUser(ctx, nil, input, "user.initial_admin_created", true)
}

func (s *Service) createUser(ctx context.Context, actorID *string, input CreateUserInput, action string, initialAdmin bool) (User, error) {
	input, emailNormalized, usernameNormalized, err := normalizeCreateInput(input)
	if err != nil {
		return User{}, err
	}
	if err := s.acquirePasswordSlot(ctx); err != nil {
		return User{}, err
	}
	hash, err := HashPassword(input.Password, s.passwordParams)
	s.releasePasswordSlot()
	if err != nil {
		return User{}, err
	}
	if initialAdmin {
		return s.store.CreateInitialAdmin(ctx, input, emailNormalized, usernameNormalized, hash, action)
	}
	return s.store.CreateUser(ctx, actorID, input, emailNormalized, usernameNormalized, hash, action)
}

func (s *Service) UpdateUser(ctx context.Context, actor User, userID string, input UpdateUserInput) (User, error) {
	if !actor.Role.CanManageUsers() {
		return User{}, ErrForbidden
	}
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	if input.DisplayName == "" || !input.Role.Valid() {
		return User{}, ErrInvalidUser
	}
	return s.store.UpdateUser(ctx, actor, userID, input)
}

func (s *Service) ResetPasswordCLI(ctx context.Context, identifier, password string) error {
	if err := ValidatePassword(password); err != nil {
		return err
	}
	user, _, err := s.store.FindByLogin(ctx, identifier)
	if err != nil {
		return err
	}
	if err := s.acquirePasswordSlot(ctx); err != nil {
		return err
	}
	hash, err := HashPassword(password, s.passwordParams)
	s.releasePasswordSlot()
	if err != nil {
		return err
	}
	return s.store.ResetPassword(ctx, user.ID, hash)
}

func (s *Service) acquirePasswordSlot(ctx context.Context) error {
	select {
	case s.verifySlots <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) releasePasswordSlot() { <-s.verifySlots }

func truncateUTF8(value string, maxBytes int) string {
	value = strings.ToValidUTF8(value, "�")
	if maxBytes < 1 {
		return ""
	}
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
