// Package app composes the process and owns its lifecycle.
package app

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"forms/internal/ai"
	"forms/internal/auth"
	formsbackup "forms/internal/backup"
	"forms/internal/config"
	"forms/internal/database"
	"forms/internal/form"
	"forms/internal/operations"
	"forms/internal/project"
	"forms/internal/projectpackage"
	"forms/internal/publicform"
	"forms/internal/response"
	formsrestore "forms/internal/restore"
	"forms/internal/web"
)

var ErrRestoreHandoff = errors.New("restore handoff requested")

type Runtime struct {
	Pool        *pgxpool.Pool
	Migrator    *database.Migrator
	Auth        *auth.Service
	AI          *ai.Service
	Projects    *project.Service
	Packages    *projectpackage.Service
	Forms       *form.Service
	PublicForms *publicform.Service
	Responses   *response.Service
}

func OpenRuntime(ctx context.Context, cfg config.Config) (*Runtime, error) {
	pool, err := database.Open(ctx, cfg.DatabaseURL, cfg.DBMinConns, cfg.DBMaxConns)
	if err != nil {
		return nil, err
	}
	migrator := database.NewMigrator(pool)
	authService, err := auth.NewService(auth.NewStore(pool), cfg.SessionIdle, cfg.SessionAbsolute)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("create authentication service: %w", err)
	}
	location, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("load display timezone: %w", err)
	}
	signingKey := make([]byte, 32)
	if _, err := rand.Read(signingKey); err != nil {
		pool.Close()
		return nil, fmt.Errorf("create public form signing key: %w", err)
	}
	publicForms, err := publicform.NewService(publicform.NewStore(pool), signingKey, location, cfg.DataDir, cfg.MaxUploadBytes)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("create public form service: %w", err)
	}
	if cfg.TurnstileSiteKey != "" {
		verifier, verifierErr := publicform.NewCloudflareTurnstileVerifier(cfg.TurnstileSecret)
		if verifierErr != nil {
			pool.Close()
			return nil, fmt.Errorf("create turnstile verifier: %w", verifierErr)
		}
		if configureErr := publicForms.ConfigureTurnstile(cfg.TurnstileSiteKey, verifier); configureErr != nil {
			pool.Close()
			return nil, fmt.Errorf("configure turnstile: %w", configureErr)
		}
	}
	responseService, err := response.NewService(response.NewStore(pool), location)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("create response service: %w", err)
	}
	return &Runtime{
		Pool:        pool,
		Migrator:    migrator,
		Auth:        authService,
		AI:          ai.NewService(ai.NewStore(pool), cfg.DataDir, responseService),
		Projects:    project.NewService(project.NewStore(pool)),
		Packages:    projectpackage.NewService(projectpackage.NewStore(pool)),
		Forms:       form.NewService(form.NewStore(pool)),
		PublicForms: publicForms,
		Responses:   responseService,
	}, nil
}

func (r *Runtime) Close() { r.Pool.Close() }

func Serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if err := PrepareDataDirectories(cfg.DataDir); err != nil {
		return err
	}
	processLock, err := operations.AcquireProcessLock(cfg.DataDir, false)
	if err != nil {
		return err
	}
	defer processLock.Close()
	runtime, err := OpenRuntime(ctx, cfg)
	if err != nil {
		return err
	}
	defer runtime.Close()
	if err := runtime.Migrator.CheckCurrent(ctx); err != nil {
		return fmt.Errorf("database is not ready; run 'forms migrate up': %w", err)
	}
	benchmarkDuration := auth.BenchmarkPasswordHash(auth.DefaultPasswordParams())
	if benchmarkDuration > cfg.PasswordHashLimit {
		return fmt.Errorf("argon2id startup benchmark exceeded limit: measured=%s limit=%s", benchmarkDuration, cfg.PasswordHashLimit)
	}
	// Argon2id deliberately allocates a large working area. The Go runtime may
	// otherwise retain those startup-only pages and make the idle RSS look like
	// active password hashing is still in progress.
	debug.FreeOSMemory()
	logger.Info("argon2id benchmark completed", slog.Duration("duration", benchmarkDuration))
	backupManager, err := formsbackup.New(formsbackup.Options{
		Pool: runtime.Pool, Migrator: runtime.Migrator, DatabaseURL: cfg.DatabaseURL,
		DataDir: cfg.DataDir, BackupDir: cfg.BackupDir, PGDumpPath: "pg_dump", PGRestorePath: "pg_restore",
	})
	if err != nil {
		return fmt.Errorf("create backup manager: %w", err)
	}
	restoreManager, err := formsrestore.New(formsrestore.Options{
		DatabaseURL: cfg.DatabaseURL, DataDir: cfg.DataDir, BackupDir: cfg.BackupDir,
		DBMinConns: cfg.DBMinConns, DBMaxConns: cfg.DBMaxConns,
		PGDumpPath: "pg_dump", PGRestorePath: "pg_restore",
	})
	if err != nil {
		return fmt.Errorf("create restore manager: %w", err)
	}
	restartRequests := make(chan struct{}, 1)
	requestRestart := func() {
		select {
		case restartRequests <- struct{}{}:
		default:
		}
	}

	handler := web.New(web.Dependencies{
		Logger: logger, Pool: runtime.Pool, Migrator: runtime.Migrator,
		Auth: runtime.Auth, AI: runtime.AI, Projects: runtime.Projects, Packages: runtime.Packages, Forms: runtime.Forms,
		PublicForms: runtime.PublicForms, Responses: runtime.Responses,
		DataDir: cfg.DataDir, BackupDir: cfg.BackupDir, Backups: backupManager, Restores: restoreManager,
		Lifecycle: ctx, RequestRestart: requestRestart,
		BackupSettings: map[string]any{
			"timezone": cfg.Timezone, "max_upload_bytes": cfg.MaxUploadBytes,
			"session_idle": cfg.SessionIdleText, "session_absolute": cfg.SessionAbsText,
			"db_max_conns": cfg.DBMaxConns, "db_min_conns": cfg.DBMinConns,
		},
		Timezone: cfg.Timezone, CookieSecure: cfg.CookieSecure,
		SessionAbsolute: cfg.SessionAbsolute, TrustedProxyCIDRs: cfg.TrustedProxyCIDRs,
	}).Handler()
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("forms server started", slog.String("address", cfg.HTTPAddr))
		serverErrors <- httpServer.ListenAndServe()
	}()

	shutdown := func(message string) error {
		shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		logger.Info(message)
		if err := httpServer.Shutdown(shutdownContext); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
		return nil
	}

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-ctx.Done():
		return shutdown("shutting down forms server")
	case <-restartRequests:
		if err := shutdown("shutting down forms server for pending restore"); err != nil {
			return err
		}
		return ErrRestoreHandoff
	}
}

func PrepareDataDirectories(dataDir string) error {
	for _, directory := range []string{
		dataDir,
		filepath.Join(dataDir, "tmp"),
		filepath.Join(dataDir, "secrets"),
		filepath.Join(dataDir, "uploads"),
		filepath.Join(dataDir, "uploads", "staging"),
		filepath.Join(dataDir, "uploads", "files"),
	} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create data directory %q: %w", directory, err)
		}
	}
	return nil
}
