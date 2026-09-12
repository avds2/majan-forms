package web

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"forms/internal/ai"
	"forms/internal/auth"
	"forms/internal/form"
	"forms/internal/project"
	"forms/internal/projectpackage"
	"forms/internal/publicform"
	responsedata "forms/internal/response"
)

type apiError struct {
	Code     string                   `json:"code"`
	Message  string                   `json:"message"`
	Details  []form.ValidationProblem `json:"details,omitempty"`
	Conflict *form.ConflictError      `json:"conflict,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if status == http.StatusNoContent {
		return
	}
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON value")
	}
	return nil
}

func (s *Server) respondError(w http.ResponseWriter, r *http.Request, err error) {
	response := apiError{Code: "internal_error", Message: "خطایی در پردازش درخواست رخ داد"}
	status := http.StatusInternalServerError

	var schemaErr *form.SchemaError
	var conflictErr *form.ConflictError
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrInvalidSession):
		status, response.Code, response.Message = http.StatusUnauthorized, "unauthorized", err.Error()
	case errors.Is(err, auth.ErrForbidden):
		status, response.Code, response.Message = http.StatusForbidden, "forbidden", err.Error()
	case errors.Is(err, auth.ErrDuplicateUser), errors.Is(err, auth.ErrLastAdmin), errors.Is(err, auth.ErrSelfLockout), errors.Is(err, auth.ErrUsersExist), errors.Is(err, project.ErrDuplicateSlug), errors.Is(err, project.ErrSlugImmutable):
		status, response.Code, response.Message = http.StatusConflict, "conflict", err.Error()
	case errors.As(err, &conflictErr):
		status, response.Code, response.Message = http.StatusConflict, "draft_conflict", conflictErr.Error()
		response.Conflict = conflictErr
	case errors.Is(err, auth.ErrNotFound), errors.Is(err, project.ErrNotFound), errors.Is(err, form.ErrDraftNotFound), errors.Is(err, responsedata.ErrNotFound), errors.Is(err, ai.ErrConversationNotFound):
		status, response.Code, response.Message = http.StatusNotFound, "not_found", err.Error()
	case errors.Is(err, publicform.ErrInvalidUpload):
		status, response.Code, response.Message = http.StatusNotFound, "file_not_found", "فایل پیدا نشد"
	case errors.As(err, &schemaErr):
		status, response.Code, response.Message = http.StatusUnprocessableEntity, "invalid_schema", schemaErr.Error()
		response.Details = schemaErr.Problems
	case errors.Is(err, auth.ErrInvalidUser), errors.Is(err, auth.ErrPasswordTooShort), errors.Is(err, auth.ErrPasswordTooLong), errors.Is(err, project.ErrInvalidProject), errors.Is(err, form.ErrInvalidSchema), errors.Is(err, form.ErrInvalidPublishKey), errors.Is(err, responsedata.ErrInvalidFilter), errors.Is(err, responsedata.ErrEmptySelection), errors.Is(err, responsedata.ErrExportFormat), errors.Is(err, responsedata.ErrExportTooLarge):
		status, response.Code, response.Message = http.StatusUnprocessableEntity, "validation_error", err.Error()
	case errors.Is(err, ai.ErrInvalidSettings), errors.Is(err, ai.ErrUnsafeEndpoint):
		status, response.Code, response.Message = http.StatusUnprocessableEntity, "invalid_ai_settings", err.Error()
	case errors.Is(err, ai.ErrNotConfigured), errors.Is(err, ai.ErrProjectDisabled):
		status, response.Code, response.Message = http.StatusConflict, "ai_not_ready", err.Error()
	case errors.Is(err, ai.ErrProviderUnauthorized), errors.Is(err, ai.ErrProviderUnavailable):
		status, response.Code, response.Message = http.StatusBadGateway, "ai_provider_error", err.Error()
	case errors.Is(err, ai.ErrProviderRateLimited):
		status, response.Code, response.Message = http.StatusServiceUnavailable, "ai_rate_limited", err.Error()
	case errors.Is(err, ai.ErrProviderTimeout):
		status, response.Code, response.Message = http.StatusGatewayTimeout, "ai_provider_timeout", err.Error()
	case errors.Is(err, ai.ErrProviderIncompatible), errors.Is(err, ai.ErrProviderToolArguments):
		status, response.Code, response.Message = http.StatusUnprocessableEntity, "ai_provider_incompatible", err.Error()
	case errors.Is(err, ai.ErrInvalidMessage):
		status, response.Code, response.Message = http.StatusUnprocessableEntity, "invalid_ai_message", err.Error()
	case errors.Is(err, ai.ErrConversationLimit), errors.Is(err, ai.ErrToolLimit):
		status, response.Code, response.Message = http.StatusConflict, "ai_limit", err.Error()
	case errors.Is(err, projectpackage.ErrPackageTooLarge):
		status, response.Code, response.Message = http.StatusRequestEntityTooLarge, "package_too_large", err.Error()
	case errors.Is(err, projectpackage.ErrUnsupportedFormat):
		status, response.Code, response.Message = http.StatusUnprocessableEntity, "unsupported_package", err.Error()
	case errors.Is(err, projectpackage.ErrInvalidPackage), errors.Is(err, projectpackage.ErrChecksumMismatch), errors.Is(err, projectpackage.ErrInvalidSelection), errors.Is(err, projectpackage.ErrResponsesForbidden):
		status, response.Code, response.Message = http.StatusUnprocessableEntity, "invalid_package", err.Error()
	case errors.Is(err, form.ErrProjectNotPublishable):
		status, response.Code, response.Message = http.StatusConflict, "project_not_publishable", err.Error()
	}
	if status == http.StatusInternalServerError {
		s.logger.ErrorContext(r.Context(), "request failed", slog.String("request_id", requestIDFromContext(r.Context())), slog.Any("error", err))
	}
	writeJSON(w, status, map[string]any{"error": response})
}
