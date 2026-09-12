package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"forms/internal/response"
)

func (s *Server) listResponses(w http.ResponseWriter, r *http.Request) {
	filter, err := responseFilterFromRequest(r)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	result, err := s.responses.List(r.Context(), authenticatedUser(r), r.PathValue("projectID"), filter)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) getResponse(w http.ResponseWriter, r *http.Request) {
	result, err := s.responses.Get(r.Context(), authenticatedUser(r), r.PathValue("projectID"), r.PathValue("responseID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) deleteResponses(w http.ResponseWriter, r *http.Request) {
	s.setResponsesDeleted(w, r, true)
}

func (s *Server) restoreResponses(w http.ResponseWriter, r *http.Request) {
	s.setResponsesDeleted(w, r, false)
}

func (s *Server) setResponsesDeleted(w http.ResponseWriter, r *http.Request, deleted bool) {
	var input struct {
		IDs []string `json:"ids"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	var (
		updated int64
		err     error
	)
	if deleted {
		updated, err = s.responses.Delete(r.Context(), authenticatedUser(r), r.PathValue("projectID"), input.IDs)
	} else {
		updated, err = s.responses.Restore(r.Context(), authenticatedUser(r), r.PathValue("projectID"), input.IDs)
	}
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": map[string]int64{"updated": updated}})
}

func (s *Server) reportSummary(w http.ResponseWriter, r *http.Request) {
	filter, err := reportFilterFromRequest(r)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	result, err := s.responses.Report(r.Context(), authenticatedUser(r), r.PathValue("projectID"), filter)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) exportResponses(w http.ResponseWriter, r *http.Request) {
	filter, err := responseFilterFromRequest(r)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	format := response.ExportFormat(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format"))))
	if format == "" {
		format = response.ExportCSV
	}
	bom := r.URL.Query().Get("bom") != "0"
	export, err := s.responses.PrepareExport(r.Context(), authenticatedUser(r), r.PathValue("projectID"), filter, response.ExportOptions{
		Format: format, BOM: bom,
	})
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", export.ContentType())
	w.Header().Set("Content-Disposition", export.ContentDisposition())
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("X-Export-Row-Count", strconv.FormatInt(export.Count(), 10))
	if err := export.Write(r.Context(), w); err != nil {
		s.logger.ErrorContext(r.Context(), "response export failed",
			slog.String("request_id", requestIDFromContext(r.Context())),
			slog.String("project_id", r.PathValue("projectID")),
			slog.Any("error", err),
		)
	}
}

func responseFilterFromRequest(r *http.Request) (response.Filter, error) {
	query := r.URL.Query()
	page, err := optionalPositiveInteger(query.Get("page"))
	if err != nil {
		return response.Filter{}, response.ErrInvalidFilter
	}
	pageSize, err := optionalPositiveInteger(query.Get("page_size"))
	if err != nil {
		return response.Filter{}, response.ErrInvalidFilter
	}
	version, err := optionalPositiveInt64(query.Get("version"))
	if err != nil {
		return response.Filter{}, response.ErrInvalidFilter
	}
	from, err := optionalDate(query.Get("from"))
	if err != nil {
		return response.Filter{}, response.ErrInvalidFilter
	}
	to, err := optionalDate(query.Get("to"))
	if err != nil {
		return response.Filter{}, response.ErrInvalidFilter
	}
	return response.Filter{
		Page: page, PageSize: pageSize, Search: query.Get("search"), VersionNumber: version,
		EndingID: strings.TrimSpace(query.Get("ending")), From: from, To: to,
		Deleted: response.DeletedState(query.Get("deleted")), Sort: response.Sort(query.Get("sort")),
	}, nil
}

func reportFilterFromRequest(r *http.Request) (response.ReportFilter, error) {
	query := r.URL.Query()
	version, err := optionalPositiveInt64(query.Get("version"))
	if err != nil {
		return response.ReportFilter{}, response.ErrInvalidFilter
	}
	from, err := optionalDate(query.Get("from"))
	if err != nil {
		return response.ReportFilter{}, response.ErrInvalidFilter
	}
	to, err := optionalDate(query.Get("to"))
	if err != nil {
		return response.ReportFilter{}, response.ErrInvalidFilter
	}
	if from != nil && to != nil && from.After(*to) {
		return response.ReportFilter{}, response.ErrInvalidFilter
	}
	return response.ReportFilter{
		VersionNumber: version, EndingID: strings.TrimSpace(query.Get("ending")),
		FieldID: strings.TrimSpace(query.Get("field")), FieldValue: strings.TrimSpace(query.Get("field_value")),
		From: from, To: to,
	}, nil
}

func optionalPositiveInteger(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return 0, errors.New("positive integer required")
	}
	return parsed, nil
}

func optionalPositiveInt64(value string) (*int64, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 1 {
		return nil, errors.New("positive integer required")
	}
	return &parsed, nil
}

func optionalDate(value string) (*time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return nil, err
	}
	return &parsed, nil
}
