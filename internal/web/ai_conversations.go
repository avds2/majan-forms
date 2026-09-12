package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"forms/internal/ai"
)

func (s *Server) listAIConversations(w http.ResponseWriter, r *http.Request) {
	items, err := s.ai.ListConversations(r.Context(), authenticatedUser(r), r.PathValue("projectID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (s *Server) createAIConversation(w http.ResponseWriter, r *http.Request) {
	var input ai.CreateConversationInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	item, err := s.ai.CreateConversation(r.Context(), authenticatedUser(r), r.PathValue("projectID"), input)
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"data": item})
}

func (s *Server) getAIConversation(w http.ResponseWriter, r *http.Request) {
	item, err := s.ai.Conversation(r.Context(), authenticatedUser(r), r.PathValue("projectID"), r.PathValue("conversationID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": item})
}

func (s *Server) deleteAIConversation(w http.ResponseWriter, r *http.Request) {
	err := s.ai.DeleteConversation(r.Context(), authenticatedUser(r), r.PathValue("projectID"), r.PathValue("conversationID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusNoContent, nil)
}

func (s *Server) listAIMessages(w http.ResponseWriter, r *http.Request) {
	items, err := s.ai.Messages(r.Context(), authenticatedUser(r), r.PathValue("projectID"), r.PathValue("conversationID"))
	if err != nil {
		s.respondError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": items})
}

func (s *Server) streamAIMessage(w http.ResponseWriter, r *http.Request) {
	var input ai.SendMessageInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": apiError{Code: "invalid_json", Message: "بدنهٔ درخواست معتبر نیست"}})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Connection", "keep-alive")
	controller := http.NewResponseController(w)
	_ = controller.SetWriteDeadline(time.Now().Add(190 * time.Second))
	if err := writeSSE(w, "meta", map[string]any{"conversation_id": r.PathValue("conversationID")}); err != nil {
		return
	}
	message, err := s.ai.StreamMessage(r.Context(), authenticatedUser(r), r.PathValue("projectID"), r.PathValue("conversationID"), input, func(delta string) error {
		return writeSSE(w, "delta", map[string]string{"text": delta})
	})
	if err != nil {
		streamError := publicAIStreamError(err)
		if streamError.Code == "internal_error" {
			s.logger.ErrorContext(r.Context(), "AI stream failed", "request_id", requestIDFromContext(r.Context()), "error", err)
		}
		_ = writeSSE(w, "error", streamError)
		return
	}
	_ = writeSSE(w, "done", map[string]any{"message": message})
}

func writeSSE(w http.ResponseWriter, event string, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func publicAIStreamError(err error) apiError {
	switch {
	case errors.Is(err, ai.ErrInvalidMessage):
		return apiError{Code: "invalid_ai_message", Message: err.Error()}
	case errors.Is(err, ai.ErrNotConfigured), errors.Is(err, ai.ErrProjectDisabled):
		return apiError{Code: "ai_not_ready", Message: err.Error()}
	case errors.Is(err, ai.ErrConversationNotFound):
		return apiError{Code: "not_found", Message: err.Error()}
	case errors.Is(err, ai.ErrProviderUnauthorized), errors.Is(err, ai.ErrProviderRateLimited),
		errors.Is(err, ai.ErrProviderUnavailable), errors.Is(err, ai.ErrProviderTimeout),
		errors.Is(err, ai.ErrProviderIncompatible):
		return apiError{Code: "ai_provider_error", Message: err.Error()}
	case errors.Is(err, ai.ErrToolLimit):
		return apiError{Code: "ai_limit", Message: err.Error()}
	default:
		return apiError{Code: "internal_error", Message: "خطایی در پردازش گفت‌وگو رخ داد"}
	}
}
