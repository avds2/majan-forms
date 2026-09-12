package web

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"

	"forms/internal/auth"
	formsbackup "forms/internal/backup"
	"forms/internal/persiandate"
	formsrestore "forms/internal/restore"
	"forms/internal/version"
)

type diskStatus struct {
	Path       string `json:"path"`
	TotalBytes int64  `json:"total_bytes"`
	FreeBytes  int64  `json:"free_bytes"`
	Writable   bool   `json:"writable"`
}

type latestBackupStatus struct {
	Name          string    `json:"name"`
	CreatedAt     time.Time `json:"created_at"`
	CreatedJalali string    `json:"created_at_jalali"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
}

type systemBackupItem struct {
	Name          string    `json:"name"`
	Valid         bool      `json:"valid"`
	Error         string    `json:"error,omitempty"`
	Size          int64     `json:"size"`
	SHA256        string    `json:"sha256"`
	CreatedAt     time.Time `json:"created_at"`
	CreatedJalali string    `json:"created_at_jalali,omitempty"`
	AppVersion    string    `json:"app_version"`
	SchemaVersion int64     `json:"schema_version"`
	Files         int       `json:"files"`
}

type systemBackupJob struct {
	Running         bool              `json:"running"`
	StartedAt       *time.Time        `json:"started_at,omitempty"`
	StartedJalali   string            `json:"started_at_jalali,omitempty"`
	CompletedAt     *time.Time        `json:"completed_at,omitempty"`
	CompletedJalali string            `json:"completed_at_jalali,omitempty"`
	Error           string            `json:"error,omitempty"`
	CompletedBackup *systemBackupItem `json:"backup,omitempty"`
}

type systemBackupList struct {
	Items []systemBackupItem `json:"items"`
	Job   systemBackupJob    `json:"job"`
}

func (s *Server) systemHealth(w http.ResponseWriter, r *http.Request) {
	if authenticatedUser(r).Role != auth.RoleAdmin {
		s.respondError(w, r, auth.ErrForbidden)
		return
	}
	ctx := r.Context()
	schema, err := s.migrator.Status(ctx)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	var maintenance struct {
		Locked     bool       `json:"write_locked"`
		Reason     string     `json:"reason"`
		LockedAt   *time.Time `json:"locked_at,omitempty"`
		LeaseUntil *time.Time `json:"lease_until,omitempty"`
		Active     int        `json:"active_writes"`
	}
	if err := s.pool.QueryRow(ctx, `
		SELECT write_locked AND lease_until > now(), reason, locked_at, lease_until,
		       (SELECT count(*) FROM maintenance_writes WHERE lease_until > now())
		FROM maintenance_state WHERE singleton = true`).Scan(
		&maintenance.Locked, &maintenance.Reason, &maintenance.LockedAt, &maintenance.LeaseUntil, &maintenance.Active,
	); err != nil {
		s.respondError(w, r, err)
		return
	}
	dataDisk := inspectDisk(s.dataDir)
	backupDisk := inspectDisk(s.backupDir)
	var latest *latestBackupStatus
	var backup latestBackupStatus
	var sizeText string
	err = s.pool.QueryRow(ctx, `
		SELECT target_id, created_at, coalesce(metadata->>'size', '0'), coalesce(metadata->>'sha256', '')
		FROM audit_log WHERE action = 'backup.created' ORDER BY created_at DESC LIMIT 1`).Scan(
		&backup.Name, &backup.CreatedAt, &sizeText, &backup.SHA256,
	)
	if err == nil {
		backup.Size, _ = strconv.ParseInt(sizeText, 10, 64)
		backup.CreatedJalali, _ = persiandate.FormatTime(backup.CreatedAt, mustTimezone(s.timezone))
		latest = &backup
	} else if !errors.Is(err, pgx.ErrNoRows) {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]any{
		"app": version.Current(), "schema": schema, "database": "ready",
		"maintenance": maintenance, "data_disk": dataDisk, "backup_disk": backupDisk, "latest_backup": latest,
	}})
}

func (s *Server) listSystemBackups(w http.ResponseWriter, r *http.Request) {
	if authenticatedUser(r).Role != auth.RoleAdmin {
		s.respondError(w, r, auth.ErrForbidden)
		return
	}
	// Snapshot the job first. If it is already complete, the archive was
	// published before this point and the following list must include it. If it
	// is still running, clients will poll again.
	s.backupJobMu.Lock()
	job := s.backupJob
	s.backupJobMu.Unlock()
	backups, err := s.backups.List(r.Context())
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	items := make([]systemBackupItem, 0, len(backups))
	for _, summary := range backups {
		items = append(items, s.systemBackupItem(summary))
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": systemBackupList{Items: items, Job: job}})
}

func (s *Server) createSystemBackup(w http.ResponseWriter, r *http.Request) {
	if authenticatedUser(r).Role != auth.RoleAdmin {
		s.respondError(w, r, auth.ErrForbidden)
		return
	}
	s.backupJobMu.Lock()
	if s.backupJob.Running {
		s.backupJobMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]any{"error": apiError{Code: "backup_running", Message: "یک بکاپ در حال ساخت است"}})
		return
	}
	started := time.Now().UTC()
	startedJalali, _ := persiandate.FormatTime(started, mustTimezone(s.timezone))
	s.backupJob = systemBackupJob{Running: true, StartedAt: &started, StartedJalali: startedJalali}
	job := s.backupJob
	s.backupJobMu.Unlock()

	go s.runSystemBackup()
	writeJSON(w, http.StatusAccepted, map[string]any{"data": job})
}

func (s *Server) runSystemBackup() {
	ctx, cancel := context.WithTimeout(s.lifecycle, 2*time.Hour)
	defer cancel()
	summary, err := s.backups.Create(ctx, "", s.backupSettings)
	completed := time.Now().UTC()
	completedJalali, _ := persiandate.FormatTime(completed, mustTimezone(s.timezone))
	s.backupJobMu.Lock()
	defer s.backupJobMu.Unlock()
	s.backupJob.Running = false
	s.backupJob.CompletedAt = &completed
	s.backupJob.CompletedJalali = completedJalali
	if err != nil {
		s.backupJob.Error = err.Error()
		s.logger.Error("system backup failed", "error", err)
		return
	}
	item := s.systemBackupItem(summary)
	s.backupJob.Error = ""
	s.backupJob.CompletedBackup = &item
}

func (s *Server) downloadSystemBackup(w http.ResponseWriter, r *http.Request) {
	if authenticatedUser(r).Role != auth.RoleAdmin {
		s.respondError(w, r, auth.ErrForbidden)
		return
	}
	name := r.PathValue("name")
	if name == "" || filepath.Base(name) != name || !strings.HasPrefix(name, "forms-") || !strings.HasSuffix(name, ".tar.gz") {
		http.NotFound(w, r)
		return
	}
	backups, err := s.backups.List(r.Context())
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	var selected *formsbackup.Summary
	for index := range backups {
		if backups[index].Valid && filepath.Base(backups[index].Path) == name {
			selected = &backups[index]
			break
		}
	}
	if selected == nil {
		http.NotFound(w, r)
		return
	}
	file, err := os.Open(selected.Path)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	defer file.Close()
	// The global API timeout protects ordinary handlers; a large authenticated
	// archive needs a separate bounded streaming window.
	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(30 * time.Minute))
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, name, selected.CreatedAt, file)
}

func (s *Server) prepareSystemRestore(w http.ResponseWriter, r *http.Request) {
	if authenticatedUser(r).Role != auth.RoleAdmin {
		s.respondError(w, r, auth.ErrForbidden)
		return
	}
	var input struct {
		Confirmation string `json:"confirmation"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	if input.Confirmation != "بازیابی" {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": apiError{
			Code: "restore_confirmation_required", Message: "برای تأیید، واژهٔ «بازیابی» را دقیق وارد کنید",
		}})
		return
	}
	s.backupJobMu.Lock()
	backupRunning := s.backupJob.Running
	s.backupJobMu.Unlock()
	if backupRunning {
		writeJSON(w, http.StatusConflict, map[string]any{"error": apiError{Code: "backup_running", Message: "تا پایان ساخت بکاپ صبر کنید"}})
		return
	}
	name := r.PathValue("name")
	if name == "" || filepath.Base(name) != name || !strings.HasPrefix(name, "forms-") || !strings.HasSuffix(name, ".tar.gz") {
		http.NotFound(w, r)
		return
	}
	backups, err := s.backups.List(r.Context())
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	var selected *formsbackup.Summary
	for index := range backups {
		if backups[index].Valid && filepath.Base(backups[index].Path) == name {
			selected = &backups[index]
			break
		}
	}
	if selected == nil {
		http.NotFound(w, r)
		return
	}
	schema, err := s.migrator.Status(r.Context())
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	if selected.SchemaVersion > schema.Latest {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": apiError{
			Code: "restore_schema_too_new", Message: "نسخهٔ این بکاپ از نسخهٔ فعلی سامانه جدیدتر است",
		}})
		return
	}
	if _, err := s.restores.Prepare(r.Context(), selected.Path); err != nil {
		if errors.Is(err, formsrestore.ErrPendingRequest) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": apiError{Code: "restore_pending", Message: err.Error()}})
			return
		}
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"data": map[string]any{
		"pending": true, "backup": s.systemBackupItem(*selected),
		"message": "درخواست ثبت شد؛ سرویس برای بازیابی امن راه‌اندازی مجدد می‌شود",
	}})
	// Give the HTTP response time to reach the browser before graceful shutdown.
	time.AfterFunc(500*time.Millisecond, s.requestRestart)
}

func (s *Server) systemBackupItem(summary formsbackup.Summary) systemBackupItem {
	item := systemBackupItem{
		Name: filepath.Base(summary.Path), Valid: summary.Valid, Error: summary.Error,
		Size: summary.Size, SHA256: summary.SHA256, CreatedAt: summary.CreatedAt,
		AppVersion: summary.AppVersion, SchemaVersion: summary.SchemaVersion, Files: summary.Files,
	}
	if !summary.CreatedAt.IsZero() {
		item.CreatedJalali, _ = persiandate.FormatTime(summary.CreatedAt, mustTimezone(s.timezone))
	}
	return item
}

func inspectDisk(directory string) diskStatus {
	result := diskStatus{Path: directory}
	if directory == "" {
		return result
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(directory, &stat); err == nil {
		result.TotalBytes = int64(stat.Blocks) * int64(stat.Bsize)
		result.FreeBytes = int64(stat.Bavail) * int64(stat.Bsize)
	}
	probe, err := os.CreateTemp(directory, ".system-health-*")
	if err == nil {
		name := probe.Name()
		result.Writable = probe.Close() == nil && os.Remove(name) == nil
	}
	return result
}

func mustTimezone(name string) *time.Location {
	location, err := time.LoadLocation(name)
	if err != nil {
		return time.UTC
	}
	return location
}
