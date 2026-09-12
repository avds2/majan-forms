package web

import (
	"net/http"

	"forms/internal/ai"
)

func (s *Server) getAIProvider(w http.ResponseWriter, r *http.Request) {
	settings, err := s.ai.ProviderSettings(r.Context(), authenticatedUser(r))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": settings})
}

func (s *Server) saveAIProvider(w http.ResponseWriter, r *http.Request) {
	var input ai.SaveProviderInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	settings, err := s.ai.SaveProviderSettings(r.Context(), authenticatedUser(r), input)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": settings})
}

func (s *Server) testAIProvider(w http.ResponseWriter, r *http.Request) {
	result, err := s.ai.TestProvider(r.Context(), authenticatedUser(r))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": result})
}

func (s *Server) getProjectAISettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.ai.ProjectSettings(r.Context(), authenticatedUser(r), r.PathValue("projectID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": settings})
}

func (s *Server) saveProjectAISettings(w http.ResponseWriter, r *http.Request) {
	var input ai.SaveProjectSettingsInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	settings, err := s.ai.SaveProjectSettings(r.Context(), authenticatedUser(r), r.PathValue("projectID"), input)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": settings})
}
