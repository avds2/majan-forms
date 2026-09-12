package ai

import (
	"context"
	"errors"
	"testing"
	"time"

	"forms/internal/auth"
)

func TestToolDefinitionsKeepTextSamplingBehindConsent(t *testing.T) {
	service := &Service{}
	aggregateOnly := service.toolDefinitions(false)
	withSampling := service.toolDefinitions(true)
	if len(aggregateOnly) != 6 || len(withSampling) != 7 {
		t.Fatalf("unexpected tool counts: aggregate=%d sampling=%d", len(aggregateOnly), len(withSampling))
	}
	for _, definition := range aggregateOnly {
		if definition.Name == "sample_text_answers" {
			t.Fatal("text sampling tool leaked into aggregate-only definition")
		}
		if definition.Parameters["additionalProperties"] != false {
			t.Fatalf("tool %s accepts unknown arguments", definition.Name)
		}
	}
	if withSampling[len(withSampling)-1].Name != "sample_text_answers" {
		t.Fatalf("explicit consent did not expose the bounded sampling tool: %+v", withSampling)
	}
}

func TestMalformedProviderToolArgumentsAreNotBlamedOnUserMessage(t *testing.T) {
	t.Parallel()
	service := &Service{}
	_, err := service.reportForTool(context.Background(), auth.User{}, "00000000-0000-4000-8000-000000000001", `{"version":"wrong"}`)
	if !errors.Is(err, ErrProviderToolArguments) || errors.Is(err, ErrInvalidMessage) {
		t.Fatalf("malformed provider arguments error = %v", err)
	}
}

func TestDateBoundsAcceptsCompatibleNullString(t *testing.T) {
	from, to, err := dateBounds("null", " NULL ")
	if err != nil || from != nil || to != nil {
		t.Fatalf("provider null strings were not treated as omitted bounds: from=%v to=%v error=%v", from, to, err)
	}
	from, to, err = dateBounds("2026-09-01", "2026-09-08")
	if err != nil || from == nil || to == nil || from.Format(time.DateOnly) != "2026-09-01" || to.Format(time.DateOnly) != "2026-09-08" {
		t.Fatalf("valid date bounds changed: from=%v to=%v error=%v", from, to, err)
	}
}

func TestDecodeToolArgumentsIsStrict(t *testing.T) {
	var valid reportToolArgs
	if err := decodeToolArguments(`{"version":1,"from":null,"to":"2026-09-07"}`, &valid); err != nil || valid.Version == nil || *valid.Version != 1 {
		t.Fatalf("valid tool arguments rejected: args=%+v error=%v", valid, err)
	}
	for _, value := range []string{
		`{"version":1,"unknown":true}`,
		`{"version":1} {"version":2}`,
		`not-json`,
	} {
		var target reportToolArgs
		if err := decodeToolArguments(value, &target); err == nil {
			t.Fatalf("unsafe tool arguments accepted: %s", value)
		}
	}
}
