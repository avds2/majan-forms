package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"forms/internal/audit"
	"forms/internal/id"
	"forms/internal/normalize"
)

type Store struct{ pool *pgxpool.Pool }

func NewStore(pool *pgxpool.Pool) *Store { return &Store{pool: pool} }

const userAdministrationLockID int64 = 684_667_230_042

type rowScanner interface{ Scan(...any) error }

func scanUser(row rowScanner) (User, error) {
	var user User
	err := row.Scan(
		&user.ID,
		&user.Email,
		&user.Username,
		&user.DisplayName,
		&user.Role,
		&user.Active,
		&user.LastLoginAt,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	return user, err
}

const userColumns = `id, email, username, display_name, role, active, last_login_at, created_at, updated_at`

func (s *Store) CreateUser(ctx context.Context, actorID *string, input CreateUserInput, emailNormalized, usernameNormalized, passwordHash string, action string) (User, error) {
	return s.createUser(ctx, actorID, input, emailNormalized, usernameNormalized, passwordHash, action, false)
}

func (s *Store) CreateInitialAdmin(ctx context.Context, input CreateUserInput, emailNormalized, usernameNormalized, passwordHash string, action string) (User, error) {
	return s.createUser(ctx, nil, input, emailNormalized, usernameNormalized, passwordHash, action, true)
}

func (s *Store) createUser(ctx context.Context, actorID *string, input CreateUserInput, emailNormalized, usernameNormalized, passwordHash string, action string, requireEmpty bool) (User, error) {
	userID, err := id.New()
	if err != nil {
		return User{}, fmt.Errorf("create user ID: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin create user: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if requireEmpty {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, userAdministrationLockID); err != nil {
			return User{}, fmt.Errorf("lock initial administrator creation: %w", err)
		}
		var count int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
			return User{}, fmt.Errorf("count users: %w", err)
		}
		if count != 0 {
			return User{}, ErrUsersExist
		}
	}
	user, err := scanUser(tx.QueryRow(ctx, `
        INSERT INTO users(id, email, email_normalized, username, username_normalized, display_name, role, password_hash)
        VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        RETURNING `+userColumns,
		userID, input.Email, emailNormalized, input.Username, usernameNormalized, input.DisplayName, input.Role, passwordHash,
	))
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return User{}, ErrDuplicateUser
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	if err := audit.Insert(ctx, tx, actorID, action, "user", user.ID, map[string]any{"role": user.Role}); err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit create user: %w", err)
	}
	return user, nil
}

func (s *Store) FindByLogin(ctx context.Context, identifier string) (User, string, error) {
	normalized := normalize.LoginIdentifier(identifier)
	row := s.pool.QueryRow(ctx, `
        SELECT `+userColumns+`, password_hash
        FROM users
        WHERE email_normalized = $1 OR username_normalized = $1
        LIMIT 1`, normalized)
	var user User
	var passwordHash string
	err := row.Scan(&user.ID, &user.Email, &user.Username, &user.DisplayName, &user.Role, &user.Active, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt, &passwordHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, "", ErrNotFound
	}
	if err != nil {
		return User{}, "", fmt.Errorf("find user by login: %w", err)
	}
	return user, passwordHash, nil
}

func (s *Store) FindByID(ctx context.Context, userID string) (User, error) {
	user, err := scanUser(s.pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("find user by ID: %w", err)
	}
	return user, nil
}

func (s *Store) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userColumns+` FROM users ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		users = append(users, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list users rows: %w", err)
	}
	return users, nil
}

func (s *Store) UpdateUser(ctx context.Context, actor User, userID string, input UpdateUserInput) (User, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return User{}, fmt.Errorf("begin update user: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, userAdministrationLockID); err != nil {
		return User{}, fmt.Errorf("lock user administration: %w", err)
	}
	current, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1 FOR UPDATE`, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrNotFound
	}
	if err != nil {
		return User{}, fmt.Errorf("lock user: %w", err)
	}
	if current.ID == actor.ID && (input.Role != RoleAdmin || !input.Active) {
		return User{}, ErrSelfLockout
	}
	if current.Role == RoleAdmin && current.Active && (input.Role != RoleAdmin || !input.Active) {
		var admins int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = 'admin' AND active = true`).Scan(&admins); err != nil {
			return User{}, fmt.Errorf("count active admins: %w", err)
		}
		if admins <= 1 {
			return User{}, ErrLastAdmin
		}
	}
	updated, err := scanUser(tx.QueryRow(ctx, `
        UPDATE users
        SET display_name = $2, role = $3, active = $4, updated_at = now()
        WHERE id = $1
        RETURNING `+userColumns, userID, input.DisplayName, input.Role, input.Active))
	if err != nil {
		return User{}, fmt.Errorf("update user: %w", err)
	}
	if !input.Active {
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
			return User{}, fmt.Errorf("revoke disabled user sessions: %w", err)
		}
	}
	if err := audit.Insert(ctx, tx, &actor.ID, "user.updated", "user", userID, map[string]any{"role": input.Role, "active": input.Active}); err != nil {
		return User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return User{}, fmt.Errorf("commit update user: %w", err)
	}
	return updated, nil
}

func (s *Store) CreateSession(ctx context.Context, user User, tokenHash []byte, idle, absolute time.Duration, userAgent string, now time.Time) error {
	sessionID, err := id.New()
	if err != nil {
		return fmt.Errorf("create session ID: %w", err)
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin login: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `
        INSERT INTO sessions(id, token_hash, user_id, created_at, last_seen_at, idle_expires_at, absolute_expires_at, user_agent)
        VALUES ($1, $2, $3, $4, $4, $5, $6, $7)`,
		sessionID, tokenHash, user.ID, now, now.Add(idle), now.Add(absolute), userAgent,
	); err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET last_login_at = $2, updated_at = updated_at WHERE id = $1`, user.ID, now); err != nil {
		return fmt.Errorf("record last login: %w", err)
	}
	if err := audit.Insert(ctx, tx, &user.ID, "auth.login", "session", sessionID, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit login: %w", err)
	}
	return nil
}

func (s *Store) AuthenticateSession(ctx context.Context, tokenHash []byte, idle time.Duration, now time.Time) (User, error) {
	row := s.pool.QueryRow(ctx, `
        SELECT u.id, u.email, u.username, u.display_name, u.role, u.active, u.last_login_at, u.created_at, u.updated_at,
               s.id, s.last_seen_at, s.idle_expires_at, s.absolute_expires_at
        FROM sessions s
        JOIN users u ON u.id = s.user_id
        WHERE s.token_hash = $1`, tokenHash)
	var user User
	var sessionID string
	var lastSeen, idleExpires, absoluteExpires time.Time
	err := row.Scan(&user.ID, &user.Email, &user.Username, &user.DisplayName, &user.Role, &user.Active, &user.LastLoginAt, &user.CreatedAt, &user.UpdatedAt, &sessionID, &lastSeen, &idleExpires, &absoluteExpires)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrInvalidSession
	}
	if err != nil {
		return User{}, fmt.Errorf("authenticate session: %w", err)
	}
	if !user.Active || !now.Before(idleExpires) || !now.Before(absoluteExpires) {
		_, _ = s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID)
		return User{}, ErrInvalidSession
	}
	if now.Sub(lastSeen) >= 5*time.Minute {
		newIdleExpiry := now.Add(idle)
		if newIdleExpiry.After(absoluteExpires) {
			newIdleExpiry = absoluteExpires
		}
		if _, err := s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = $2, idle_expires_at = $3 WHERE id = $1`, sessionID, now, newIdleExpiry); err != nil {
			return User{}, fmt.Errorf("refresh session: %w", err)
		}
	}
	return user, nil
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE token_hash = $1`, tokenHash)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) ResetPassword(ctx context.Context, userID, passwordHash string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin password reset: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	command, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, userID, passwordHash)
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}
	if command.RowsAffected() == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID); err != nil {
		return fmt.Errorf("revoke sessions after password reset: %w", err)
	}
	if err := audit.Insert(ctx, tx, nil, "user.password_reset_cli", "user", userID, nil); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit password reset: %w", err)
	}
	return nil
}

func (s *Store) RecordLoginFailure(ctx context.Context, identifier string) {
	hash := sha256.Sum256([]byte(normalize.LoginIdentifier(identifier)))
	metadata := map[string]any{"identifier_hash": hex.EncodeToString(hash[:])}
	_ = audit.Insert(ctx, s.pool, nil, "auth.login_failed", "authentication", "", metadata)
}
