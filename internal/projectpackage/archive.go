package projectpackage

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"forms/internal/form"
	"forms/internal/project"
)

var versionPathPattern = regexp.MustCompile(`^versions/([0-9]{6,})\.json$`)
var packageUUIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type archiveEntry struct {
	path string
	data []byte
}

func buildArchive(source sourceData, appVersion string, createdAt time.Time) ([]byte, decodedPackage, error) {
	entries := make([]archiveEntry, 0, len(source.Versions)+2)
	projectDocument := ProjectDocument{Title: source.Project.Title, Slug: source.Project.Slug}
	for _, item := range source.Versions {
		if source.Project.ActiveVersionID != nil && *source.Project.ActiveVersionID == item.sourceID {
			value := item.VersionNumber
			projectDocument.ActiveVersionNumber = &value
		}
	}
	projectBytes, err := marshalDocument(projectDocument)
	if err != nil {
		return nil, decodedPackage{}, err
	}
	entries = append(entries, archiveEntry{path: "project.json", data: projectBytes})
	draftBytes, err := marshalDocument(source.Draft)
	if err != nil {
		return nil, decodedPackage{}, err
	}
	entries = append(entries, archiveEntry{path: "draft.json", data: draftBytes})
	for _, item := range source.Versions {
		encoded, encodeErr := marshalDocument(item)
		if encodeErr != nil {
			return nil, decodedPackage{}, encodeErr
		}
		entries = append(entries, archiveEntry{path: fmt.Sprintf("versions/%06d.json", item.VersionNumber), data: encoded})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].path < entries[j].path })
	counts := schemaCounts(source.Draft.Schema)
	counts.Versions = len(source.Versions)
	manifest := Manifest{
		Format: Format, FormatVersion: FormatVersion, AppVersion: appVersion,
		CreatedAt: createdAt.UTC(), SourceProjectID: source.Project.ID, Counts: counts,
		Files: make([]ManifestFile, 0, len(entries)),
	}
	for _, entry := range entries {
		manifest.Files = append(manifest.Files, ManifestFile{Path: entry.path, Size: int64(len(entry.data)), SHA256: checksum(entry.data)})
	}
	manifestBytes, err := marshalDocument(manifest)
	if err != nil {
		return nil, decodedPackage{}, err
	}

	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	if err := writeZipEntry(writer, "manifest.json", manifestBytes, createdAt); err != nil {
		return nil, decodedPackage{}, err
	}
	for _, entry := range entries {
		if err := writeZipEntry(writer, entry.path, entry.data, createdAt); err != nil {
			return nil, decodedPackage{}, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, decodedPackage{}, fmt.Errorf("close project package: %w", err)
	}
	if int64(output.Len()) > MaxArchiveBytes {
		return nil, decodedPackage{}, ErrPackageTooLarge
	}
	packageBytes := output.Bytes()
	decoded := decodedPackage{
		Manifest: manifest, Project: projectDocument, Draft: source.Draft,
		Versions: source.Versions, SHA256: checksum(packageBytes),
	}
	return packageBytes, decoded, nil
}

func parseArchive(raw []byte) (decodedPackage, error) {
	if len(raw) == 0 {
		return decodedPackage{}, ErrInvalidPackage
	}
	if int64(len(raw)) > MaxArchiveBytes {
		return decodedPackage{}, ErrPackageTooLarge
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return decodedPackage{}, fmt.Errorf("%w: zip قابل‌خواندن نیست", ErrInvalidPackage)
	}
	if len(reader.File) == 0 || len(reader.File) > maxFileCount {
		return decodedPackage{}, ErrInvalidPackage
	}
	entries := make(map[string][]byte, len(reader.File))
	var expanded uint64
	for _, file := range reader.File {
		if err := validateEntry(file); err != nil {
			return decodedPackage{}, err
		}
		if _, duplicate := entries[file.Name]; duplicate {
			return decodedPackage{}, fmt.Errorf("%w: مسیر تکراری %s", ErrInvalidPackage, file.Name)
		}
		expanded += file.UncompressedSize64
		if expanded > maxExpandedBytes {
			return decodedPackage{}, ErrPackageTooLarge
		}
		entryReader, openErr := file.Open()
		if openErr != nil {
			return decodedPackage{}, fmt.Errorf("%w: بازکردن %s", ErrInvalidPackage, file.Name)
		}
		data, readErr := io.ReadAll(io.LimitReader(entryReader, maxEntryBytes+1))
		closeErr := entryReader.Close()
		if readErr != nil || closeErr != nil || int64(len(data)) > maxEntryBytes || uint64(len(data)) != file.UncompressedSize64 {
			return decodedPackage{}, fmt.Errorf("%w: خواندن %s", ErrInvalidPackage, file.Name)
		}
		entries[file.Name] = data
	}
	manifestBytes, ok := entries["manifest.json"]
	if !ok {
		return decodedPackage{}, fmt.Errorf("%w: manifest.json وجود ندارد", ErrInvalidPackage)
	}
	var manifest Manifest
	if err := decodeDocument(manifestBytes, &manifest); err != nil {
		return decodedPackage{}, fmt.Errorf("%w: manifest.json: %v", ErrInvalidPackage, err)
	}
	if manifest.Format != Format || manifest.FormatVersion != FormatVersion {
		return decodedPackage{}, ErrUnsupportedFormat
	}
	if manifest.AppVersion == "" || len(manifest.AppVersion) > 128 || !packageUUIDPattern.MatchString(manifest.SourceProjectID) || manifest.CreatedAt.IsZero() {
		return decodedPackage{}, ErrInvalidPackage
	}
	if manifest.Counts.Versions < 0 || manifest.Counts.Versions > maxFileCount-3 ||
		manifest.Counts.Assets < 0 || manifest.Counts.Pages < 1 || manifest.Counts.Pages > 200 ||
		manifest.Counts.Fields < 0 || manifest.Counts.Fields > 1000 || manifest.Counts.Endings < 1 || manifest.Counts.Endings > 20 {
		return decodedPackage{}, ErrInvalidPackage
	}
	if len(manifest.Files) != len(entries)-1 || len(manifest.Files) > maxFileCount-1 {
		return decodedPackage{}, ErrInvalidPackage
	}
	declared := make(map[string]struct{}, len(manifest.Files))
	for _, item := range manifest.Files {
		if item.Path == "manifest.json" || !safeArchivePath(item.Path) {
			return decodedPackage{}, ErrInvalidPackage
		}
		if _, duplicate := declared[item.Path]; duplicate {
			return decodedPackage{}, ErrInvalidPackage
		}
		declared[item.Path] = struct{}{}
		data, exists := entries[item.Path]
		if !exists || item.Size != int64(len(data)) || !validChecksum(item.SHA256) || !strings.EqualFold(item.SHA256, checksum(data)) {
			return decodedPackage{}, ErrChecksumMismatch
		}
	}
	for name := range entries {
		if name == "manifest.json" {
			continue
		}
		if _, exists := declared[name]; !exists {
			return decodedPackage{}, ErrInvalidPackage
		}
		if strings.HasPrefix(name, "responses/") || strings.HasPrefix(name, "submissions/") || strings.HasPrefix(name, "respondent-files/") {
			return decodedPackage{}, ErrResponsesForbidden
		}
	}

	var projectDocument ProjectDocument
	if err := decodeExpected(entries, "project.json", &projectDocument); err != nil {
		return decodedPackage{}, err
	}
	var draft DraftDocument
	if err := decodeExpected(entries, "draft.json", &draft); err != nil {
		return decodedPackage{}, err
	}
	normalizedProject, normalizeErr := project.NormalizeCreateInput(project.CreateInput{Title: projectDocument.Title, Slug: projectDocument.Slug})
	if normalizeErr != nil || normalizedProject.Title != projectDocument.Title || normalizedProject.Slug != projectDocument.Slug || draft.Revision < 1 {
		return decodedPackage{}, ErrInvalidPackage
	}
	if err := verifySchema(draft.Schema, draft.Checksum, false); err != nil {
		return decodedPackage{}, err
	}

	versions := make([]VersionDocument, 0, manifest.Counts.Versions)
	seenNumbers := make(map[int64]struct{})
	for name, data := range entries {
		if name == "manifest.json" || name == "project.json" || name == "draft.json" {
			continue
		}
		match := versionPathPattern.FindStringSubmatch(name)
		if match == nil {
			return decodedPackage{}, fmt.Errorf("%w: مسیر ناشناخته %s", ErrInvalidPackage, name)
		}
		pathVersion, parseErr := strconv.ParseInt(match[1], 10, 64)
		if parseErr != nil || pathVersion < 1 {
			return decodedPackage{}, ErrInvalidPackage
		}
		var version VersionDocument
		if err := decodeDocument(data, &version); err != nil {
			return decodedPackage{}, fmt.Errorf("%w: %s: %v", ErrInvalidPackage, name, err)
		}
		if version.VersionNumber != pathVersion || version.PublishedAt.IsZero() {
			return decodedPackage{}, ErrInvalidPackage
		}
		if _, duplicate := seenNumbers[version.VersionNumber]; duplicate {
			return decodedPackage{}, ErrInvalidPackage
		}
		seenNumbers[version.VersionNumber] = struct{}{}
		if err := verifySchema(version.Schema, version.Checksum, true); err != nil {
			return decodedPackage{}, err
		}
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].VersionNumber < versions[j].VersionNumber })
	counts := schemaCounts(draft.Schema)
	counts.Versions = len(versions)
	if manifest.Counts != counts || manifest.Counts.Assets != 0 {
		return decodedPackage{}, ErrInvalidPackage
	}
	if projectDocument.ActiveVersionNumber != nil {
		if _, exists := seenNumbers[*projectDocument.ActiveVersionNumber]; !exists {
			return decodedPackage{}, ErrInvalidPackage
		}
	}
	return decodedPackage{
		Manifest: manifest, Project: projectDocument, Draft: draft,
		Versions: versions, SHA256: checksum(raw),
	}, nil
}

func validateEntry(file *zip.File) error {
	if !safeArchivePath(file.Name) || strings.HasSuffix(file.Name, "/") || !file.FileInfo().Mode().IsRegular() {
		return fmt.Errorf("%w: مسیر ناامن %q", ErrInvalidPackage, file.Name)
	}
	if file.UncompressedSize64 > maxEntryBytes {
		return ErrPackageTooLarge
	}
	if file.UncompressedSize64 > 4096 {
		if file.CompressedSize64 == 0 || file.UncompressedSize64/file.CompressedSize64 > maxCompressionRatio {
			return ErrPackageTooLarge
		}
	}
	return nil
}

func safeArchivePath(name string) bool {
	return name != "" && !strings.ContainsRune(name, 0) && !strings.Contains(name, "\\") &&
		!strings.HasPrefix(name, "/") && path.Clean(name) == name && name != "." &&
		!strings.HasPrefix(name, "../") && !strings.Contains(name, "/../")
}

func verifySchema(schema form.Schema, expectedChecksum string, published bool) error {
	encoded, actualChecksum, err := form.EncodeAndChecksum(schema)
	_ = encoded
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidPackage, err)
	}
	if published {
		if err := schema.ValidateForPublish(); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidPackage, err)
		}
	}
	if !validChecksum(expectedChecksum) || !strings.EqualFold(actualChecksum, expectedChecksum) {
		return ErrChecksumMismatch
	}
	return nil
}

func decodeExpected(entries map[string][]byte, name string, destination any) error {
	data, exists := entries[name]
	if !exists {
		return fmt.Errorf("%w: %s وجود ندارد", ErrInvalidPackage, name)
	}
	if err := decodeDocument(data, destination); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrInvalidPackage, name, err)
	}
	return nil
}

func decodeDocument(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("فایل باید دقیقاً یک سند JSON داشته باشد")
	}
	return nil
}

func marshalDocument(value any) ([]byte, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode project package document: %w", err)
	}
	return append(encoded, '\n'), nil
}

func writeZipEntry(writer *zip.Writer, name string, data []byte, modified time.Time) error {
	header := &zip.FileHeader{Name: name, Method: zip.Deflate, Flags: 0x800}
	header.SetMode(0o600)
	header.SetModTime(modified.UTC())
	entry, err := writer.CreateHeader(header)
	if err != nil {
		return fmt.Errorf("create package entry %s: %w", name, err)
	}
	if _, err := entry.Write(data); err != nil {
		return fmt.Errorf("write package entry %s: %w", name, err)
	}
	return nil
}

func schemaCounts(schema form.Schema) Counts {
	count := Counts{Pages: len(schema.Pages), Endings: len(schema.Endings)}
	for _, page := range schema.Pages {
		count.Fields += len(page.Fields)
	}
	return count
}

func checksum(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func validChecksum(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
