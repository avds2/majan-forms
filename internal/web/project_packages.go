package web

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"forms/internal/projectpackage"
)

func (s *Server) exportProjectPackage(w http.ResponseWriter, r *http.Request) {
	selected, err := selectedPackageVersions(r.URL.Query().Get("versions"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	exported, err := s.packages.Export(r.Context(), authenticatedUser(r), r.PathValue("projectID"), selected)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": exported.Filename}))
	w.Header().Set("Content-Length", strconv.Itoa(len(exported.Bytes)))
	w.Header().Set("X-Package-Format", projectpackage.Format)
	w.Header().Set("X-Package-SHA256", exported.SHA256)
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(exported.Bytes)
}

func (s *Server) previewProjectPackage(w http.ResponseWriter, r *http.Request) {
	raw, err := readProjectPackage(w, r)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	preview, err := s.packages.Preview(r.Context(), authenticatedUser(r), raw)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": preview})
}

func (s *Server) importProjectPackage(w http.ResponseWriter, r *http.Request) {
	raw, err := readProjectPackage(w, r)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	result, err := s.packages.Import(r.Context(), authenticatedUser(r), projectpackage.ImportInput{
		Title: r.URL.Query().Get("title"), Slug: r.URL.Query().Get("slug"),
	}, raw)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": result})
}

func readProjectPackage(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if r.ContentLength > projectpackage.MaxArchiveBytes {
		return nil, projectpackage.ErrPackageTooLarge
	}
	r.Body = http.MaxBytesReader(w, r.Body, projectpackage.MaxArchiveBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var maxBytesError *http.MaxBytesError
		if errors.As(err, &maxBytesError) {
			return nil, projectpackage.ErrPackageTooLarge
		}
		return nil, projectpackage.ErrInvalidPackage
	}
	if len(raw) == 0 {
		return nil, projectpackage.ErrInvalidPackage
	}
	return raw, nil
}

func selectedPackageVersions(value string) ([]int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "all" {
		return nil, nil
	}
	parts := strings.Split(value, ",")
	if len(parts) > 200 {
		return nil, projectpackage.ErrInvalidSelection
	}
	selected := make([]int64, 0, len(parts))
	for _, part := range parts {
		number, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || number < 1 {
			return nil, projectpackage.ErrInvalidSelection
		}
		selected = append(selected, number)
	}
	return selected, nil
}
