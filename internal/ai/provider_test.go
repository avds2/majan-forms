package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompatibilityTestSupportsBothModes(t *testing.T) {
	for _, mode := range []APIMode{ModeResponses, ModeChatCompletions} {
		t.Run(string(mode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != providerPath(mode) || r.Header.Get("Authorization") != "Bearer test-provider-secret" {
					t.Errorf("unexpected request path/auth: %s %q", r.URL.Path, r.Header.Get("Authorization"))
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode provider request: %v", err)
				}
				if payload["stream"] != true {
					t.Errorf("compatibility request is not streaming: %+v", payload)
				}
				if mode == ModeResponses && payload["store"] != false {
					t.Errorf("Responses request must disable provider storage: %+v", payload)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if mode == ModeResponses {
					fmt.Fprint(w, "data: {\"type\":\"response.created\"}\n\n")
					fmt.Fprint(w, "data: {\"type\":\"response.output_item.added\",\"item\":{\"type\":\"function_call\",\"name\":\"health_check\"}}\n\n")
				} else {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"function\":{\"name\":\"health_check\"}}]}}]}\n\n")
					fmt.Fprint(w, "data: [DONE]\n\n")
				}
			}))
			defer server.Close()
			provider := NewHTTPProvider()
			result, err := provider.CompatibilityTest(context.Background(), providerConfig{
				BaseURL: server.URL, Mode: mode, Model: "test-model", Timeout: 2 * time.Second,
				MaxOutputTokens: 512, AllowPrivateEndpoint: true,
			}, "test-provider-secret")
			if err != nil {
				t.Fatalf("compatibility test: %v", err)
			}
			if !result.Streaming || !result.Tools || result.Mode != mode {
				t.Fatalf("unexpected compatibility result: %+v", result)
			}
		})
	}
}

func TestCompatibilityTestDoesNotFollowRedirects(t *testing.T) {
	var destinationHits atomic.Int64
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		destinationHits.Add(1)
	}))
	defer destination.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer redirector.Close()
	provider := NewHTTPProvider()
	_, err := provider.CompatibilityTest(context.Background(), providerConfig{
		BaseURL: redirector.URL, Mode: ModeResponses, Model: "test-model", Timeout: time.Second,
		MaxOutputTokens: 256, AllowPrivateEndpoint: true,
	}, "test-provider-secret")
	if !errors.Is(err, ErrProviderUnavailable) || destinationHits.Load() != 0 {
		t.Fatalf("redirect was not blocked: error=%v hits=%d", err, destinationHits.Load())
	}
}

func TestStreamRoundSupportsResponsesAndChatCompletions(t *testing.T) {
	for _, mode := range []APIMode{ModeResponses, ModeChatCompletions} {
		t.Run(string(mode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode stream request: %v", err)
				}
				if payload["stream"] != true || payload["store"] != false || payload["parallel_tool_calls"] != false {
					t.Errorf("stream privacy/tool constraints missing: %+v", payload)
				}
				if r.URL.Path != providerPath(mode) {
					t.Errorf("unexpected provider path: %s", r.URL.Path)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				if mode == ModeResponses {
					fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"پاسخ \"}\n\n")
					fmt.Fprint(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"آزمایشی\"}\n\n")
				} else {
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"پاسخ \"}}]}\n\n")
					fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"آزمایشی\"}}]}\n\n")
					fmt.Fprint(w, "data: [DONE]\n\n")
				}
			}))
			defer server.Close()
			provider := NewHTTPProvider()
			var streamed strings.Builder
			round, err := provider.StreamRound(context.Background(), providerConfig{
				BaseURL: server.URL, Mode: mode, Model: "test-model", Timeout: 2 * time.Second,
				MaxOutputTokens: 512, AllowPrivateEndpoint: true,
			}, "test-provider-secret", modelPrompt{Messages: []modelMessage{{Role: "user", Content: "آزمون"}}}, []toolDefinition{{
				Name: "count_submissions", Description: "test", Parameters: objectSchema(map[string]any{}, nil),
			}}, func(delta string) error {
				streamed.WriteString(delta)
				return nil
			})
			if err != nil {
				t.Fatalf("stream round: %v", err)
			}
			if round.Text != "پاسخ آزمایشی" || streamed.String() != round.Text || len(round.Deltas) != 2 || len(round.ToolCalls) != 0 {
				t.Fatalf("unexpected stream result: %+v", round)
			}
		})
	}
}

func TestParseProviderStreamReassemblesToolCalls(t *testing.T) {
	tests := []struct {
		name   string
		mode   APIMode
		stream string
	}{
		{
			name: "responses", mode: ModeResponses,
			stream: "data: {\"type\":\"response.output_item.added\",\"item\":{\"id\":\"item-1\",\"type\":\"function_call\",\"call_id\":\"call-1\",\"name\":\"count_submissions\",\"arguments\":\"\"}}\n\n" +
				"data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"item-1\",\"delta\":\"{\\\"version\\\":\"}\n\n" +
				"data: {\"type\":\"response.function_call_arguments.delta\",\"item_id\":\"item-1\",\"delta\":\"null}\"}\n\n",
		},
		{
			name: "chat", mode: ModeChatCompletions,
			stream: "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call-1\",\"function\":{\"name\":\"count_\",\"arguments\":\"{\\\"version\\\":\"}}]}}]}\n\n" +
				"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"name\":\"submissions\",\"arguments\":\"null}\"}}]}}]}\n\n" +
				"data: [DONE]\n\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			round, err := parseProviderStream(strings.NewReader(test.stream), test.mode, nil)
			if err != nil {
				t.Fatalf("parse tool stream: %v", err)
			}
			if len(round.ToolCalls) != 1 || round.ToolCalls[0].ID != "call-1" ||
				round.ToolCalls[0].Name != "count_submissions" || round.ToolCalls[0].Arguments != `{"version":null}` {
				t.Fatalf("unexpected tool call: %+v", round.ToolCalls)
			}
		})
	}
}
