package web

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/jackc/pgx/v5"

	"forms/internal/auth"
	"forms/internal/id"
)

type contextKey string

const (
	userContextKey      contextKey = "authenticated-user"
	requestIDContextKey contextKey = "request-id"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) Flush() {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(contents []byte) (int, error) {
	if r.status == 0 {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(contents)
}

func (s *Server) middleware(next http.Handler) http.Handler {
	crossOrigin := http.NewCrossOriginProtection()
	crossOrigin.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": apiError{Code: "cross_origin_denied", Message: "مبدأ درخواست مجاز نیست"}})
	}))
	return requestIDMiddleware(
		s.recoverMiddleware(
			s.securityHeaders(
				s.loggingMiddleware(
					s.maintenanceMiddleware(crossOrigin.Handler(next)),
				),
			),
		),
	)
}

func (s *Server) maintenanceMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		// Creating a backup takes the maintenance lock itself. Registering this
		// request as an ordinary write would make the backup wait for its own
		// lease forever.
		if r.Method == http.MethodPost && r.URL.Path == "/api/system/backups" {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		requestID := requestIDFromContext(r.Context())
		var admitted bool
		err := s.pool.QueryRow(ctx, `
			INSERT INTO maintenance_writes(request_id, lease_until)
			SELECT $1, now() + interval '5 minutes'
			FROM maintenance_state
			WHERE singleton = true AND (NOT write_locked OR lease_until <= now())
			ON CONFLICT (request_id) DO UPDATE SET lease_until = EXCLUDED.lease_until
			RETURNING true`, requestID).Scan(&admitted)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			w.Header().Set("Retry-After", "30")
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": apiError{
				Code: "maintenance_check_failed", Message: "وضعیت نگهداری سامانه قابل بررسی نیست؛ کمی بعد دوباره تلاش کنید",
			}})
			return
		}
		if !admitted {
			w.Header().Set("Retry-After", "30")
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"error": apiError{
				Code: "maintenance", Message: "سامانه موقتاً در حالت نگهداری است؛ اطلاعات شما ثبت نشد و می‌توانید کمی بعد دوباره تلاش کنید",
			}})
			return
		}
		defer func() {
			releaseContext, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer releaseCancel()
			if _, err := s.pool.Exec(releaseContext, `DELETE FROM maintenance_writes WHERE request_id = $1`, requestID); err != nil {
				s.logger.ErrorContext(r.Context(), "release maintenance write lease", "error", err, "request_id", requestID)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID, err := id.Token(16)
		if err != nil {
			requestID = "unavailable"
		}
		w.Header().Set("X-Request-ID", requestID)
		ctx := context.WithValue(r.Context(), requestIDContextKey, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey).(string)
	return value
}

func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.ErrorContext(r.Context(), "panic recovered",
					slog.String("request_id", requestIDFromContext(r.Context())),
					slog.Any("panic", recovered),
					slog.String("stack", string(debug.Stack())),
				)
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": apiError{Code: "internal_error", Message: "خطایی در پردازش درخواست رخ داد"}})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self' https://challenges.cloudflare.com; style-src 'self'; style-src-elem 'self'; style-src-attr 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'; connect-src 'self' https://challenges.cloudflare.com; frame-src https://challenges.cloudflare.com")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.cookieSecure {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		s.logger.InfoContext(r.Context(), "http request",
			slog.String("request_id", requestIDFromContext(r.Context())),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Duration("duration", time.Since(started)),
		)
	})
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(s.cookieName())
		if err != nil {
			s.respondError(w, r, auth.ErrInvalidSession)
			return
		}
		user, err := s.auth.Authenticate(r.Context(), cookie.Value)
		if err != nil {
			s.clearSessionCookie(w)
			s.respondError(w, r, err)
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func authenticatedUser(r *http.Request) auth.User {
	user, ok := r.Context().Value(userContextKey).(auth.User)
	if !ok {
		panic(fmt.Sprintf("authenticated user missing for %s %s", r.Method, r.URL.Path))
	}
	return user
}
