package projectpackage

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"forms/internal/form"
	"forms/internal/project"
)

func TestProjectPackageRoundTrip(t *testing.T) {
	schema, err := form.NewEmpty("فرم انتقال")
	if err != nil {
		t.Fatal(err)
	}
	_, schemaChecksum, err := form.EncodeAndChecksum(schema)
	if err != nil {
		t.Fatal(err)
	}
	activeID := "11111111-1111-4111-8111-111111111111"
	publishedAt := time.Date(2026, time.September, 7, 7, 30, 0, 0, time.UTC)
	source := sourceData{
		Project: project.Project{
			ID: "22222222-2222-4222-8222-222222222222", Title: "فرم انتقال", Slug: "portable-form",
			ActiveVersionID: &activeID,
		},
		Draft: DraftDocument{Revision: 4, Checksum: schemaChecksum, Schema: schema},
		Versions: []VersionDocument{{
			VersionNumber: 1, Checksum: schemaChecksum, PublishedAt: publishedAt, Schema: schema, sourceID: activeID,
		}},
	}
	raw, generated, err := buildArchive(source, "0.3.0-test", publishedAt)
	if err != nil {
		t.Fatalf("build package: %v", err)
	}
	parsed, err := parseArchive(raw)
	if err != nil {
		t.Fatalf("parse generated package: %v", err)
	}
	if parsed.SHA256 != generated.SHA256 || parsed.Project.ActiveVersionNumber == nil || *parsed.Project.ActiveVersionNumber != 1 {
		t.Fatalf("package metadata was not preserved: %+v", parsed)
	}
	if parsed.Manifest.Counts.Versions != 1 || parsed.Manifest.Counts.Pages != 1 || parsed.Manifest.Counts.Endings != 1 {
		t.Fatalf("package counts are incorrect: %+v", parsed.Manifest.Counts)
	}
	if len(parsed.Versions) != 1 || parsed.Versions[0].Checksum != schemaChecksum || parsed.Draft.Checksum != schemaChecksum {
		t.Fatalf("schemas/checksums were not preserved: %+v", parsed)
	}
	archive, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range archive.File {
		if strings.Contains(item.Name, "response") || strings.Contains(item.Name, "submission") {
			t.Fatalf("response data leaked into project package: %s", item.Name)
		}
	}
}

func TestProjectPackageRejectsUnsafeArchives(t *testing.T) {
	valid := testPackage(t)
	tests := []struct {
		name string
		raw  func(*testing.T) []byte
		want error
	}{
		{
			name: "zip slip",
			raw: func(t *testing.T) []byte {
				return makeZip(t, []archiveEntry{{path: "../escape.json", data: []byte(`{}`)}})
			},
			want: ErrInvalidPackage,
		},
		{
			name: "duplicate path",
			raw: func(t *testing.T) []byte {
				return makeZip(t, []archiveEntry{{path: "manifest.json", data: []byte(`{}`)}, {path: "manifest.json", data: []byte(`{}`)}})
			},
			want: ErrInvalidPackage,
		},
		{
			name: "compression bomb",
			raw: func(t *testing.T) []byte {
				return makeZip(t, []archiveEntry{{path: "manifest.json", data: bytes.Repeat([]byte("a"), 2<<20)}})
			},
			want: ErrPackageTooLarge,
		},
		{
			name: "checksum mismatch",
			raw: func(t *testing.T) []byte {
				entries := readZipEntries(t, valid)
				var manifest Manifest
				if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
					t.Fatal(err)
				}
				manifest.Files[0].SHA256 = strings.Repeat("0", 64)
				entries["manifest.json"], _ = marshalDocument(manifest)
				return makeZipMap(t, entries)
			},
			want: ErrChecksumMismatch,
		},
		{
			name: "respondent data",
			raw: func(t *testing.T) []byte {
				entries := readZipEntries(t, valid)
				responseData := []byte(`{"answers":{"secret":"raw text"}}`)
				entries["responses/data.json"] = responseData
				var manifest Manifest
				if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
					t.Fatal(err)
				}
				manifest.Files = append(manifest.Files, ManifestFile{Path: "responses/data.json", Size: int64(len(responseData)), SHA256: checksum(responseData)})
				entries["manifest.json"], _ = marshalDocument(manifest)
				return makeZipMap(t, entries)
			},
			want: ErrResponsesForbidden,
		},
		{
			name: "duplicate schema ID",
			raw: func(t *testing.T) []byte {
				entries := readZipEntries(t, valid)
				var draft DraftDocument
				if err := json.Unmarshal(entries["draft.json"], &draft); err != nil {
					t.Fatal(err)
				}
				draft.Schema.Endings[0].ID = draft.Schema.Pages[0].ID
				entries["draft.json"], _ = marshalDocument(draft)
				refreshManifestEntry(t, entries, "draft.json")
				return makeZipMap(t, entries)
			},
			want: ErrInvalidPackage,
		},
		{
			name: "incompatible schema",
			raw: func(t *testing.T) []byte {
				entries := readZipEntries(t, valid)
				var draft DraftDocument
				if err := json.Unmarshal(entries["draft.json"], &draft); err != nil {
					t.Fatal(err)
				}
				draft.Schema.SchemaVersion = 99
				entries["draft.json"], _ = marshalDocument(draft)
				refreshManifestEntry(t, entries, "draft.json")
				return makeZipMap(t, entries)
			},
			want: ErrInvalidPackage,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := parseArchive(test.raw(t))
			if !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}
}

func TestSelectVersionsIsExact(t *testing.T) {
	available := []VersionDocument{{VersionNumber: 1}, {VersionNumber: 2}, {VersionNumber: 3}}
	selected, err := selectVersions(available, []int64{3, 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(selected) != 2 || selected[0].VersionNumber != 1 || selected[1].VersionNumber != 3 {
		t.Fatalf("unexpected selection: %+v", selected)
	}
	for _, invalid := range [][]int64{{0}, {2, 2}, {4}} {
		if _, err := selectVersions(available, invalid); !errors.Is(err, ErrInvalidSelection) {
			t.Fatalf("selection %v should fail, got %v", invalid, err)
		}
	}
}

func FuzzParseArchive(f *testing.F) {
	f.Add([]byte("not-a-zip"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, raw []byte) {
		if int64(len(raw)) > MaxArchiveBytes {
			t.Skip()
		}
		_, _ = parseArchive(raw)
	})
}

func testPackage(t *testing.T) []byte {
	t.Helper()
	schema, err := form.NewEmpty("بستهٔ آزمون")
	if err != nil {
		t.Fatal(err)
	}
	_, schemaChecksum, err := form.EncodeAndChecksum(schema)
	if err != nil {
		t.Fatal(err)
	}
	raw, _, err := buildArchive(sourceData{
		Project: project.Project{ID: "33333333-3333-4333-8333-333333333333", Title: "بستهٔ آزمون", Slug: "test-package"},
		Draft:   DraftDocument{Revision: 1, Checksum: schemaChecksum, Schema: schema},
	}, "test", time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func makeZip(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()
	var output bytes.Buffer
	writer := zip.NewWriter(&output)
	for _, item := range entries {
		entry, err := writer.Create(item.path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(item.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}

func makeZipMap(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	items := make([]archiveEntry, 0, len(entries))
	for name, data := range entries {
		items = append(items, archiveEntry{path: name, data: data})
	}
	return makeZip(t, items)
}

func readZipEntries(t *testing.T, raw []byte) map[string][]byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatal(err)
	}
	entries := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		opened, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(opened)
		opened.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name] = data
	}
	return entries
}

func refreshManifestEntry(t *testing.T, entries map[string][]byte, name string) {
	t.Helper()
	var manifest Manifest
	if err := json.Unmarshal(entries["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	for index := range manifest.Files {
		if manifest.Files[index].Path == name {
			manifest.Files[index].Size = int64(len(entries[name]))
			manifest.Files[index].SHA256 = checksum(entries[name])
			entries["manifest.json"], _ = marshalDocument(manifest)
			return
		}
	}
	t.Fatalf("manifest entry %s was not found", name)
}
