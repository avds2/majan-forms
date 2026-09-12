package web

import (
	"context"
	"log/slog"
	"mime"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"forms/internal/ai"
	"forms/internal/auth"
	formsbackup "forms/internal/backup"
	"forms/internal/database"
	"forms/internal/form"
	"forms/internal/project"
	"forms/internal/projectpackage"
	"forms/internal/publicform"
	"forms/internal/response"
	formsrestore "forms/internal/restore"
)

type Server struct {
	logger          *slog.Logger
	pool            *pgxpool.Pool
	migrator        *database.Migrator
	auth            *auth.Service
	ai              *ai.Service
	projects        *project.Service
	packages        *projectpackage.Service
	forms           *form.Service
	publicForms     *publicform.Service
	responses       *response.Service
	dataDir         string
	backupDir       string
	backups         *formsbackup.Manager
	restores        *formsrestore.Manager
	backupSettings  map[string]any
	lifecycle       context.Context
	requestRestart  func()
	backupJobMu     sync.Mutex
	backupJob       systemBackupJob
	timezone        string
	cookieSecure    bool
	sessionAbsolute time.Duration
	loginLimiter    *loginLimiter
	publicLimiter   *publicRequestLimiter
	trustedProxies  []netip.Prefix
}

type Dependencies struct {
	Logger            *slog.Logger
	Pool              *pgxpool.Pool
	Migrator          *database.Migrator
	Auth              *auth.Service
	AI                *ai.Service
	Projects          *project.Service
	Packages          *projectpackage.Service
	Forms             *form.Service
	PublicForms       *publicform.Service
	Responses         *response.Service
	DataDir           string
	BackupDir         string
	Backups           *formsbackup.Manager
	Restores          *formsrestore.Manager
	BackupSettings    map[string]any
	Lifecycle         context.Context
	RequestRestart    func()
	Timezone          string
	CookieSecure      bool
	SessionAbsolute   time.Duration
	TrustedProxyCIDRs []string
}

func New(dependencies Dependencies) *Server {
	trustedProxies := make([]netip.Prefix, 0, len(dependencies.TrustedProxyCIDRs))
	for _, value := range dependencies.TrustedProxyCIDRs {
		if prefix, err := netip.ParsePrefix(value); err == nil {
			trustedProxies = append(trustedProxies, prefix.Masked())
		}
	}
	lifecycle := dependencies.Lifecycle
	if lifecycle == nil {
		lifecycle = context.Background()
	}
	return &Server{
		logger: dependencies.Logger, pool: dependencies.Pool, migrator: dependencies.Migrator,
		auth: dependencies.Auth, ai: dependencies.AI, projects: dependencies.Projects, packages: dependencies.Packages, forms: dependencies.Forms, publicForms: dependencies.PublicForms,
		responses: dependencies.Responses,
		dataDir:   dependencies.DataDir, backupDir: dependencies.BackupDir, backups: dependencies.Backups, restores: dependencies.Restores,
		backupSettings: dependencies.BackupSettings, lifecycle: lifecycle, requestRestart: dependencies.RequestRestart,
		timezone: dependencies.Timezone, cookieSecure: dependencies.CookieSecure,
		sessionAbsolute: dependencies.SessionAbsolute, loginLimiter: newLoginLimiter(), publicLimiter: newPublicRequestLimiter(), trustedProxies: trustedProxies,
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /report-assets/{asset...}", reportAssetHandler())
	mux.HandleFunc("GET /health/live", s.healthLive)
	mux.HandleFunc("GET /health/ready", s.healthReady)
	mux.HandleFunc("GET /api/runtime-config", s.runtimeConfig)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	if s.publicForms != nil {
		mux.Handle("GET /public-assets/{asset...}", publicAssetHandler())
		mux.HandleFunc("GET /f/{slug}", s.publicLanding)
		mux.HandleFunc("POST /f/{slug}/start", s.publicStart)
		mux.HandleFunc("POST /f/{slug}/step", s.publicStep)
		mux.HandleFunc("POST /f/{slug}/submit", s.publicSubmit)
	}

	mux.Handle("GET /api/auth/me", s.requireAuth(http.HandlerFunc(s.me)))
	if s.ai != nil {
		mux.Handle("GET /api/ai/provider", s.requireAuth(http.HandlerFunc(s.getAIProvider)))
		mux.Handle("PUT /api/ai/provider", s.requireAuth(http.HandlerFunc(s.saveAIProvider)))
		mux.Handle("POST /api/ai/provider/test", s.requireAuth(http.HandlerFunc(s.testAIProvider)))
		mux.Handle("GET /api/projects/{projectID}/ai/settings", s.requireAuth(http.HandlerFunc(s.getProjectAISettings)))
		mux.Handle("PUT /api/projects/{projectID}/ai/settings", s.requireAuth(http.HandlerFunc(s.saveProjectAISettings)))
		mux.Handle("GET /api/projects/{projectID}/ai/conversations", s.requireAuth(http.HandlerFunc(s.listAIConversations)))
		mux.Handle("POST /api/projects/{projectID}/ai/conversations", s.requireAuth(http.HandlerFunc(s.createAIConversation)))
		mux.Handle("GET /api/projects/{projectID}/ai/conversations/{conversationID}", s.requireAuth(http.HandlerFunc(s.getAIConversation)))
		mux.Handle("DELETE /api/projects/{projectID}/ai/conversations/{conversationID}", s.requireAuth(http.HandlerFunc(s.deleteAIConversation)))
		mux.Handle("GET /api/projects/{projectID}/ai/conversations/{conversationID}/messages", s.requireAuth(http.HandlerFunc(s.listAIMessages)))
		mux.Handle("POST /api/projects/{projectID}/ai/conversations/{conversationID}/messages/stream", s.requireAuth(http.HandlerFunc(s.streamAIMessage)))
	}
	mux.Handle("GET /api/users", s.requireAuth(http.HandlerFunc(s.listUsers)))
	mux.Handle("GET /api/system/health", s.requireAuth(http.HandlerFunc(s.systemHealth)))
	if s.backups != nil {
		mux.Handle("GET /api/system/backups", s.requireAuth(http.HandlerFunc(s.listSystemBackups)))
		mux.Handle("POST /api/system/backups", s.requireAuth(http.HandlerFunc(s.createSystemBackup)))
		mux.Handle("GET /api/system/backups/{name}", s.requireAuth(http.HandlerFunc(s.downloadSystemBackup)))
		if s.restores != nil && s.requestRestart != nil {
			mux.Handle("POST /api/system/backups/{name}/restore", s.requireAuth(http.HandlerFunc(s.prepareSystemRestore)))
		}
	}
	mux.Handle("POST /api/users", s.requireAuth(http.HandlerFunc(s.createUser)))
	mux.Handle("PATCH /api/users/{userID}", s.requireAuth(http.HandlerFunc(s.updateUser)))
	mux.Handle("GET /api/projects", s.requireAuth(http.HandlerFunc(s.listProjects)))
	mux.Handle("POST /api/projects", s.requireAuth(http.HandlerFunc(s.createProject)))
	mux.Handle("GET /api/projects/{projectID}", s.requireAuth(http.HandlerFunc(s.getProject)))
	mux.Handle("PATCH /api/projects/{projectID}", s.requireAuth(http.HandlerFunc(s.updateProject)))
	mux.Handle("DELETE /api/projects/{projectID}", s.requireAuth(http.HandlerFunc(s.trashProject)))
	mux.Handle("POST /api/projects/{projectID}/restore", s.requireAuth(http.HandlerFunc(s.restoreProject)))
	mux.Handle("POST /api/projects/{projectID}/close", s.requireAuth(http.HandlerFunc(s.closeProject)))
	mux.Handle("POST /api/projects/{projectID}/reopen", s.requireAuth(http.HandlerFunc(s.reopenProject)))
	mux.Handle("GET /api/projects/{projectID}/draft", s.requireAuth(http.HandlerFunc(s.getDraft)))
	mux.Handle("PUT /api/projects/{projectID}/draft", s.requireAuth(http.HandlerFunc(s.saveDraft)))
	mux.Handle("POST /api/projects/{projectID}/publish/validate", s.requireAuth(http.HandlerFunc(s.validatePublish)))
	mux.Handle("POST /api/projects/{projectID}/publish", s.requireAuth(http.HandlerFunc(s.publish)))
	mux.Handle("GET /api/projects/{projectID}/versions", s.requireAuth(http.HandlerFunc(s.listVersions)))
	if s.packages != nil {
		mux.Handle("GET /api/projects/{projectID}/package", s.requireAuth(http.HandlerFunc(s.exportProjectPackage)))
		mux.Handle("POST /api/project-packages/preview", s.requireAuth(http.HandlerFunc(s.previewProjectPackage)))
		mux.Handle("POST /api/project-packages/import", s.requireAuth(http.HandlerFunc(s.importProjectPackage)))
	}
	if s.responses != nil {
		mux.Handle("GET /api/projects/{projectID}/responses", s.requireAuth(http.HandlerFunc(s.listResponses)))
		mux.Handle("GET /api/projects/{projectID}/responses/{responseID}", s.requireAuth(http.HandlerFunc(s.getResponse)))
		mux.Handle("POST /api/projects/{projectID}/responses/delete", s.requireAuth(http.HandlerFunc(s.deleteResponses)))
		mux.Handle("POST /api/projects/{projectID}/responses/restore", s.requireAuth(http.HandlerFunc(s.restoreResponses)))
		mux.Handle("GET /api/projects/{projectID}/reports/summary", s.requireAuth(http.HandlerFunc(s.reportSummary)))
		mux.Handle("GET /api/projects/{projectID}/reports/print", s.requireAuth(http.HandlerFunc(s.printReport)))
		mux.Handle("GET /api/projects/{projectID}/exports/responses", s.requireAuth(http.HandlerFunc(s.exportResponses)))
	}
	if s.publicForms != nil {
		mux.Handle("GET /api/files/{fileID}", s.requireAuth(http.HandlerFunc(s.downloadStoredFile)))
	}
	mux.Handle("/", adminHandler())
	return s.middleware(mux)
}

func (s *Server) downloadStoredFile(w http.ResponseWriter, r *http.Request) {
	fileRecord, file, err := s.publicForms.OpenFinalizedFile(r.Context(), r.PathValue("fileID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", fileRecord.DetectedMIME)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": fileRecord.OriginalName}))
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, fileRecord.OriginalName, fileRecord.CreatedAt, file)
}

func (s *Server) runtimeConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]string{"timezone": s.timezone}})
}

func (s *Server) cookieName() string {
	if s.cookieSecure {
		return "__Host-forms_session"
	}
	return "forms_session"
}

func (s *Server) setSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(), Value: token, Path: "/", HttpOnly: true,
		Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: time.Now().Add(s.sessionAbsolute), MaxAge: int(s.sessionAbsolute.Seconds()),
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: s.cookieName(), Value: "", Path: "/", HttpOnly: true,
		Secure: s.cookieSecure, SameSite: http.SameSiteLaxMode,
		Expires: time.Unix(1, 0), MaxAge: -1,
	})
}

func (s *Server) healthLive(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "live"})
}

func (s *Server) healthReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "check": "database"})
		return
	}
	if err := s.migrator.CheckCurrent(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "check": "migrations"})
		return
	}
	var maintenance bool
	if err := s.pool.QueryRow(ctx, `SELECT write_locked AND lease_until > now() FROM maintenance_state WHERE singleton = true`).Scan(&maintenance); err != nil || maintenance {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "check": "maintenance"})
		return
	}
	probe, err := os.CreateTemp(filepath.Join(s.dataDir, "tmp"), ".health-*")
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "check": "storage"})
		return
	}
	probePath := probe.Name()
	closeErr := probe.Close()
	removeErr := os.Remove(probePath)
	if closeErr != nil || removeErr != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready", "check": "storage"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	if !s.loginLimiter.Allow(r.RemoteAddr, input.Identifier) {
		tooManyRequests(w)
		return
	}
	user, token, err := s.auth.Login(r.Context(), input.Identifier, input.Password, r.UserAgent())
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	s.setSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]any{"data": user})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(s.cookieName()); err == nil {
		if err := s.auth.Logout(r.Context(), cookie.Value); err != nil {
			s.respondError(w, r, err)
			return
		}
	}
	s.clearSessionCookie(w)
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"data": authenticatedUser(r)})
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.auth.ListUsers(r.Context(), authenticatedUser(r))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": users})
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var input auth.CreateUserInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	user, err := s.auth.CreateUser(r.Context(), authenticatedUser(r), input)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": user})
}

func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	var input auth.UpdateUserInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	user, err := s.auth.UpdateUser(r.Context(), authenticatedUser(r), r.PathValue("userID"), input)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": user})
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	trash, _ := strconv.ParseBool(r.URL.Query().Get("trash"))
	projects, err := s.projects.List(r.Context(), authenticatedUser(r), trash)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": projects})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var input project.CreateInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	created, err := s.projects.Create(r.Context(), authenticatedUser(r), input)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": created})
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request) {
	found, err := s.projects.Get(r.Context(), authenticatedUser(r), r.PathValue("projectID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": found})
}

func (s *Server) updateProject(w http.ResponseWriter, r *http.Request) {
	var input project.UpdateInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	updated, err := s.projects.Update(r.Context(), authenticatedUser(r), r.PathValue("projectID"), input)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": updated})
}

func (s *Server) trashProject(w http.ResponseWriter, r *http.Request) {
	trashed, err := s.projects.Trash(r.Context(), authenticatedUser(r), r.PathValue("projectID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": trashed})
}

func (s *Server) restoreProject(w http.ResponseWriter, r *http.Request) {
	restored, err := s.projects.Restore(r.Context(), authenticatedUser(r), r.PathValue("projectID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": restored})
}

func (s *Server) closeProject(w http.ResponseWriter, r *http.Request) {
	closed, err := s.projects.Close(r.Context(), authenticatedUser(r), r.PathValue("projectID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": closed})
}

func (s *Server) reopenProject(w http.ResponseWriter, r *http.Request) {
	reopened, err := s.projects.Reopen(r.Context(), authenticatedUser(r), r.PathValue("projectID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": reopened})
}

func (s *Server) getDraft(w http.ResponseWriter, r *http.Request) {
	draft, err := s.forms.GetDraft(r.Context(), authenticatedUser(r), r.PathValue("projectID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	w.Header().Set("ETag", `"`+strconv.FormatInt(draft.Revision, 10)+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"data": draft})
}

func (s *Server) saveDraft(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedRevision int64       `json:"expected_revision"`
		Schema           form.Schema `json:"schema"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	draft, err := s.forms.SaveDraft(r.Context(), authenticatedUser(r), r.PathValue("projectID"), input.ExpectedRevision, input.Schema)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	w.Header().Set("ETag", `"`+strconv.FormatInt(draft.Revision, 10)+`"`)
	writeJSON(w, http.StatusOK, map[string]any{"data": draft})
}

func (s *Server) validatePublish(w http.ResponseWriter, r *http.Request) {
	draft, err := s.forms.ValidateForPublish(r.Context(), authenticatedUser(r), r.PathValue("projectID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"valid": true, "revision": draft.Revision, "checksum": draft.Checksum}})
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ExpectedRevision int64 `json:"expected_revision"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	version, err := s.forms.Publish(r.Context(), authenticatedUser(r), r.PathValue("projectID"), input.ExpectedRevision, r.Header.Get("Idempotency-Key"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": version})
}

func (s *Server) listVersions(w http.ResponseWriter, r *http.Request) {
	versions, err := s.forms.ListVersions(r.Context(), authenticatedUser(r), r.PathValue("projectID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": versions})
}
