package publicform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"unicode/utf8"

	"forms/internal/form"
	"forms/internal/id"
)

var (
	ErrInvalidUpload    = errors.New("فایل بارگذاری‌شده معتبر نیست")
	ErrUploadTooLarge   = errors.New("حجم فایل از محدودیت مجاز بیشتر است")
	ErrStorageLow       = errors.New("فضای ذخیره‌سازی برای بارگذاری فایل کافی نیست")
	storedFileIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

func (s *Service) StageUpload(ctx context.Context, slug, token, pageID, fieldID, originalName string, source io.Reader) (StoredFile, error) {
	session, err := s.loadSession(ctx, slug, token)
	if err != nil {
		return StoredFile{}, err
	}
	if session.CurrentPageID != pageID {
		return StoredFile{}, ErrInvalidPage
	}
	field := findFieldOnPage(session.Project.Version.Schema, pageID, fieldID)
	if field == nil || field.Type != form.FieldFile && field.Type != form.FieldSignature {
		return StoredFile{}, ErrInvalidUpload
	}
	maximumBytes, maximumFiles, allowedExtensions := int64(2<<20), 1, map[string]bool{"png": true}
	if field.Type == form.FieldFile {
		if field.File == nil {
			return StoredFile{}, ErrInvalidUpload
		}
		maximumBytes = field.File.MaximumBytes
		if maximumBytes > s.maxUploadBytes {
			maximumBytes = s.maxUploadBytes
		}
		maximumFiles = field.File.MaximumFiles
		allowedExtensions = make(map[string]bool, len(field.File.Extensions))
		for _, extension := range field.File.Extensions {
			allowedExtensions[strings.TrimPrefix(strings.ToLower(extension), ".")] = true
		}
	}
	cleanName, extension, err := safeOriginalName(originalName)
	if err != nil || !allowedExtensions[extension] {
		return StoredFile{}, ErrInvalidUpload
	}
	if !hasUploadCapacity(s.dataDir, maximumBytes) {
		return StoredFile{}, ErrStorageLow
	}
	storageKey, err := id.New()
	if err != nil {
		return StoredFile{}, fmt.Errorf("create file storage key: %w", err)
	}
	fileID, err := id.New()
	if err != nil {
		return StoredFile{}, fmt.Errorf("create stored file ID: %w", err)
	}
	stagingPath := filepath.Join(s.dataDir, "uploads", "staging", storageKey)
	file, err := os.OpenFile(stagingPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return StoredFile{}, fmt.Errorf("create staged upload: %w", err)
	}
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(stagingPath)
		}
	}()
	limited := io.LimitReader(source, maximumBytes+1)
	head := make([]byte, 512)
	headLength, readErr := io.ReadFull(limited, head)
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return StoredFile{}, fmt.Errorf("read upload header: %w", readErr)
	}
	head = head[:headLength]
	detected := strings.Split(http.DetectContentType(head), ";")[0]
	if !mimeAllowed(extension, detected) {
		return StoredFile{}, ErrInvalidUpload
	}
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(file, digest), io.MultiReader(bytes.NewReader(head), limited))
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return StoredFile{}, ErrUploadTooLarge
		}
		return StoredFile{}, fmt.Errorf("stream staged upload: %w", err)
	}
	if written > maximumBytes {
		return StoredFile{}, ErrUploadTooLarge
	}
	if err := file.Sync(); err != nil {
		return StoredFile{}, fmt.Errorf("sync staged upload: %w", err)
	}
	if err := file.Close(); err != nil {
		return StoredFile{}, fmt.Errorf("close staged upload: %w", err)
	}
	fileRecord := StoredFile{
		ID: fileID, PublicSessionID: &session.ID, FieldID: field.ID, StorageKey: storageKey,
		OriginalName: cleanName, SizeBytes: written, DetectedMIME: detected, Extension: extension,
		Checksum: hex.EncodeToString(digest.Sum(nil)), Status: "staging", CreatedAt: s.now(),
	}
	if err := s.store.CreateStagedFile(ctx, fileRecord, maximumFiles, session.Revision, pageID); err != nil {
		return StoredFile{}, err
	}
	remove = false
	return fileRecord, nil
}

func (s *Service) StageSignature(ctx context.Context, slug, token, pageID, fieldID, encoded string) (StoredFile, error) {
	const prefix = "data:image/png;base64,"
	if !strings.HasPrefix(encoded, prefix) || len(encoded) > 3<<20 {
		return StoredFile{}, ErrInvalidUpload
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(encoded, prefix))
	if err != nil {
		return StoredFile{}, ErrInvalidUpload
	}
	return s.StageUpload(ctx, slug, token, pageID, fieldID, "signature.png", bytes.NewReader(decoded))
}

func (s *Service) OpenFinalizedFile(ctx context.Context, fileID string) (StoredFile, *os.File, error) {
	if !storedFileIDPattern.MatchString(fileID) {
		return StoredFile{}, nil, ErrInvalidUpload
	}
	fileRecord, err := s.store.GetFinalizedFile(ctx, fileID)
	if err != nil {
		return StoredFile{}, nil, err
	}
	file, err := os.Open(filepath.Join(s.dataDir, "uploads", "files", fileRecord.StorageKey))
	if err != nil {
		return StoredFile{}, nil, fmt.Errorf("open finalized file: %w", err)
	}
	return fileRecord, file, nil
}

func (s *Service) validateRequiredFiles(ctx context.Context, session Session, page form.Page) []AnswerProblem {
	problems := make([]AnswerProblem, 0)
	for _, field := range page.Fields {
		if !field.Required || field.Type != form.FieldFile && field.Type != form.FieldSignature {
			continue
		}
		count, err := s.store.CountStagedFiles(ctx, session.ID, field.ID)
		if err != nil || count == 0 {
			problems = append(problems, AnswerProblem{FieldID: field.ID, Message: "بارگذاری «" + field.Label + "» الزامی است"})
		}
	}
	return problems
}

func (s *Service) finalizeSubmissionFiles(ctx context.Context, submissionID string) error {
	files, err := s.store.ListPendingSubmissionFiles(ctx, submissionID)
	if err != nil {
		return err
	}
	for _, file := range files {
		stagingPath := filepath.Join(s.dataDir, "uploads", "staging", file.StorageKey)
		finalPath := filepath.Join(s.dataDir, "uploads", "files", file.StorageKey)
		if err := os.Rename(stagingPath, finalPath); err != nil {
			if _, statErr := os.Stat(finalPath); statErr != nil {
				return fmt.Errorf("finalize stored file: %w", err)
			}
		}
	}
	return s.store.MarkFilesFinalized(ctx, submissionID, s.now())
}

func (s *Service) pruneStagedFiles(ctx context.Context, session Session, reachablePages []string) error {
	allowedFields := make(map[string]bool)
	reachable := make(map[string]bool, len(reachablePages))
	for _, pageID := range reachablePages {
		reachable[pageID] = true
	}
	for _, page := range session.Project.Version.Schema.Pages {
		if !reachable[page.ID] {
			continue
		}
		for _, field := range page.Fields {
			allowedFields[field.ID] = true
		}
	}
	files, err := s.store.ListStagedFiles(ctx, session.ID)
	if err != nil {
		return err
	}
	for _, file := range files {
		if allowedFields[file.FieldID] {
			continue
		}
		storageKey, err := s.store.DeleteStagedFile(ctx, session.ID, file.ID)
		if err != nil {
			return err
		}
		if storageKey != "" {
			if err := os.Remove(filepath.Join(s.dataDir, "uploads", "staging", storageKey)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("remove unreachable staged file: %w", err)
			}
		}
	}
	return nil
}

func findFieldOnPage(schema form.Schema, pageID, fieldID string) *form.Field {
	page := findPage(schema, pageID)
	if page == nil {
		return nil
	}
	for index := range page.Fields {
		if page.Fields[index].ID == fieldID {
			return &page.Fields[index]
		}
	}
	return nil
}

func safeOriginalName(value string) (string, string, error) {
	value = strings.ToValidUTF8(value, "�")
	value = filepath.Base(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimSpace(value)
	if value == "" || value == "." || value == ".." {
		return "", "", ErrInvalidUpload
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return "", "", ErrInvalidUpload
		}
	}
	for len(value) > 180 || !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(value)), ".")
	if extension == "" {
		return "", "", ErrInvalidUpload
	}
	return value, extension, nil
}

func mimeAllowed(extension, detected string) bool {
	allowed := map[string]map[string]bool{
		"pdf":  {"application/pdf": true},
		"jpg":  {"image/jpeg": true},
		"jpeg": {"image/jpeg": true},
		"png":  {"image/png": true},
		"webp": {"image/webp": true},
		"txt":  {"text/plain": true, "application/octet-stream": true},
		"csv":  {"text/plain": true, "text/csv": true, "application/octet-stream": true},
		"docx": {"application/zip": true},
		"xlsx": {"application/zip": true},
		"zip":  {"application/zip": true},
	}
	return allowed[extension][detected]
}

func hasUploadCapacity(dataDir string, incoming int64) bool {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dataDir, &stat); err != nil {
		return false
	}
	available := int64(stat.Bavail) * int64(stat.Bsize)
	return available > incoming+(256<<20)
}
