package publicform

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

type CleanupReport struct {
	RecoveredSubmissions int `json:"recovered_submissions"`
	ExpiredSessions      int `json:"expired_sessions"`
	RemovedStagedFiles   int `json:"removed_staged_files"`
	RemovedOrphanFiles   int `json:"removed_orphan_files"`
}

func (s *Service) CleanupTemporary(ctx context.Context) (CleanupReport, error) {
	var report CleanupReport
	pendingSubmissions, err := s.store.ListPendingSubmissionIDs(ctx)
	if err != nil {
		return report, err
	}
	for _, submissionID := range pendingSubmissions {
		if err := s.finalizeSubmissionFiles(ctx, submissionID); err != nil {
			return report, fmt.Errorf("recover submission files: %w", err)
		}
		report.RecoveredSubmissions++
	}

	now := s.now()
	storageKeys, removedSessions, err := s.store.DeleteExpiredSessions(ctx, now)
	if err != nil {
		return report, err
	}
	report.ExpiredSessions = int(removedSessions)
	for _, storageKey := range storageKeys {
		path := filepath.Join(s.dataDir, "uploads", "staging", storageKey)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return report, fmt.Errorf("remove expired staged file: %w", err)
		}
		report.RemovedStagedFiles++
	}

	knownKeys, err := s.store.StagingStorageKeys(ctx)
	if err != nil {
		return report, err
	}
	stagingDirectory := filepath.Join(s.dataDir, "uploads", "staging")
	entries, err := os.ReadDir(stagingDirectory)
	if err != nil {
		return report, fmt.Errorf("read upload staging directory: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || knownKeys[entry.Name()] {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return report, fmt.Errorf("inspect orphan staged file: %w", err)
		}
		if info.ModTime().After(now.Add(-publicSessionLifetime)) {
			continue
		}
		if err := os.Remove(filepath.Join(stagingDirectory, entry.Name())); err != nil && !os.IsNotExist(err) {
			return report, fmt.Errorf("remove orphan staged file: %w", err)
		}
		report.RemovedOrphanFiles++
	}
	return report, nil
}
