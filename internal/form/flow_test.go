package form

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"reflect"
	"testing"
)

const (
	flowPageStart  = "11111111-1111-4111-8111-111111111111"
	flowPageWork   = "22222222-2222-4222-8222-222222222222"
	flowPageShort  = "33333333-3333-4333-8333-333333333333"
	flowExperience = "11111111-aaaa-4111-8111-111111111111"
	flowPortfolio  = "22222222-aaaa-4222-8222-222222222222"
	flowTrack      = "33333333-aaaa-4333-8333-333333333333"
	flowHired      = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	flowRejected   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

func employmentFlowSchema() Schema {
	return Schema{
		SchemaVersion: SchemaVersion,
		Title:         "فرم استخدام",
		Pages: []Page{
			{ID: flowPageStart, Title: "سابقه", Fields: []Field{
				{ID: flowExperience, Type: FieldNumber, Label: "سال سابقه"},
				{ID: flowTrack, Type: FieldHidden, Label: "مسیر ارزیابی"},
			}},
			{ID: flowPageWork, Title: "نمونه‌کار", Fields: []Field{
				{ID: flowPortfolio, Type: FieldSingleChoice, Label: "نمونه‌کار تأیید شد؟", Options: []Option{
					{ID: "44444444-aaaa-4444-8444-444444444444", Label: "بله"},
					{ID: "55555555-aaaa-4555-8555-555555555555", Label: "خیر"},
				}},
			}},
			{ID: flowPageShort, Title: "مسیر کوتاه", Fields: []Field{}},
		},
		Endings: []Ending{
			{ID: flowHired, Title: "دعوت به مصاحبه"},
			{ID: flowRejected, Title: "پایان بررسی"},
		},
		Flow: Flow{StartPageID: flowPageStart, Edges: []FlowEdge{
			{
				ID: "10000000-0000-4000-8000-000000000001", SourceID: flowPageStart, TargetID: flowPageWork, Priority: 10,
				Condition: &ConditionGroup{Match: "all", Conditions: []Condition{{FieldID: flowExperience, Operator: "greater_than", Value: 4}}},
				Actions:   []AssignmentAction{{ID: "10000000-0000-4000-8000-000000000011", TargetFieldID: flowTrack, Mode: "replace", Source: "constant", Value: "ارشد"}},
			},
			{ID: "10000000-0000-4000-8000-000000000002", SourceID: flowPageStart, TargetID: flowPageShort, Priority: 1000, Fallback: true},
			{
				ID: "10000000-0000-4000-8000-000000000003", SourceID: flowPageWork, TargetID: flowHired, Priority: 10,
				Condition: &ConditionGroup{Match: "all", Conditions: []Condition{{FieldID: flowPortfolio, Operator: "equals", Value: "44444444-aaaa-4444-8444-444444444444"}}},
			},
			{ID: "10000000-0000-4000-8000-000000000004", SourceID: flowPageWork, TargetID: flowRejected, Priority: 1000, Fallback: true},
			{ID: "10000000-0000-4000-8000-000000000005", SourceID: flowPageShort, TargetID: flowRejected, Priority: 1000, Fallback: true},
		}},
		Theme:          Theme{Preset: "minimal", Alignment: "start"},
		PublicSettings: PublicSettings{MinimumFillSeconds: 2},
		EditorMetadata: json.RawMessage(`{}`),
	}
}

func TestFlowGoldenCases(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile("../../testdata/flow-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name          string         `json:"name"`
		Answers       map[string]any `json:"answers"`
		Pages         []string       `json:"pages"`
		Ending        string         `json:"ending"`
		AssignedTrack any            `json:"assigned_track"`
	}
	if err := json.Unmarshal(contents, &cases); err != nil {
		t.Fatal(err)
	}
	schema := employmentFlowSchema()
	if err := schema.ValidateForPublish(); err != nil {
		t.Fatalf("fixture schema is invalid: %v", err)
	}
	for _, testCase := range cases {
		t.Run(testCase.Name, func(t *testing.T) {
			result, err := Evaluate(schema, testCase.Answers)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result.PageIDs, testCase.Pages) || result.EndingID != testCase.Ending {
				t.Fatalf("Evaluate() path=%v ending=%s", result.PageIDs, result.EndingID)
			}
			if got := result.Answers[flowTrack]; !reflect.DeepEqual(got, testCase.AssignedTrack) {
				t.Fatalf("assigned track = %#v, want %#v", got, testCase.AssignedTrack)
			}
		})
	}
}

func TestPublishValidationRejectsCycleReferenceAndMissingFallback(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*Schema){
		"cycle": func(schema *Schema) {
			schema.Flow.Edges[3].TargetID = flowPageStart
		},
		"broken reference": func(schema *Schema) {
			schema.Flow.Edges[0].Condition.Conditions[0].FieldID = "ffffffff-ffff-4fff-8fff-ffffffffffff"
		},
		"missing fallback": func(schema *Schema) {
			schema.Flow.Edges = schema.Flow.Edges[:4]
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			schema := employmentFlowSchema()
			mutate(&schema)
			if err := schema.ValidateForPublish(); !errors.Is(err, ErrInvalidSchema) {
				t.Fatalf("ValidateForPublish() error = %v", err)
			}
		})
	}
}

func TestPublishValidationEnforcesTypedChoiceAndFiniteValues(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*Schema){
		"choice label instead of ID": func(schema *Schema) {
			schema.Flow.Edges[2].Condition.Conditions[0].Value = "بله"
		},
		"unknown choice ID": func(schema *Schema) {
			schema.Flow.Edges[2].Condition.Conditions[0].Value = "ffffffff-ffff-4fff-8fff-ffffffffffff"
		},
		"non-finite number": func(schema *Schema) {
			schema.Flow.Edges[0].Condition.Conditions[0].Value = math.NaN()
		},
		"invalid assignment target value": func(schema *Schema) {
			schema.Flow.Edges[0].Actions[0].TargetFieldID = flowPortfolio
			schema.Flow.Edges[0].Actions[0].Value = "بله"
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			schema := employmentFlowSchema()
			mutate(&schema)
			if err := schema.ValidateForPublish(); !errors.Is(err, ErrInvalidSchema) {
				t.Fatalf("ValidateForPublish() error = %v", err)
			}
		})
	}
}

func TestMatrixConditionsFlattenSelectedColumns(t *testing.T) {
	t.Parallel()
	condition := Condition{
		FieldID: "matrix", Operator: "contains_all",
		Value: []any{"column-one", "column-two"},
	}
	answers := map[string]any{"matrix": map[string]any{
		"row-one": "column-one",
		"row-two": []any{"column-two"},
	}}
	if !evaluateCondition(condition, answers) {
		t.Fatal("matrix column selections did not satisfy contains_all")
	}
	condition.Value = []any{"column-three"}
	if evaluateCondition(condition, answers) {
		t.Fatal("unknown matrix column unexpectedly matched")
	}
}

func TestEmptyConditionMatchesMissingAnswer(t *testing.T) {
	t.Parallel()
	if !evaluateCondition(Condition{FieldID: flowExperience, Operator: "empty"}, map[string]any{}) {
		t.Fatal("missing answer must be considered empty")
	}
	if evaluateCondition(Condition{FieldID: flowExperience, Operator: "not_empty"}, map[string]any{}) {
		t.Fatal("missing answer must not satisfy not_empty")
	}
}

func TestPublishValidationRejectsFutureAndUnreachableReferences(t *testing.T) {
	t.Parallel()
	tests := map[string]func(*Schema){
		"future condition": func(schema *Schema) {
			schema.Flow.Edges[0].Condition.Conditions[0] = Condition{FieldID: flowPortfolio, Operator: "empty"}
		},
		"unreachable assignment target": func(schema *Schema) {
			schema.Flow.Edges[1].Actions = []AssignmentAction{{
				ID: "10000000-0000-4000-8000-000000000012", TargetFieldID: flowPortfolio,
				Mode: "replace", Source: "constant", Value: "44444444-aaaa-4444-8444-444444444444",
			}}
		},
		"future copy source": func(schema *Schema) {
			schema.Pages[1].Fields = append(schema.Pages[1].Fields,
				Field{ID: "66666666-aaaa-4666-8666-666666666666", Type: FieldNumber, Label: "منبع آینده"},
				Field{ID: "77777777-aaaa-4777-8777-777777777777", Type: FieldNumber, Label: "مقصد آینده"},
			)
			schema.Flow.Edges[0].Actions[0] = AssignmentAction{
				ID: "10000000-0000-4000-8000-000000000013", TargetFieldID: "77777777-aaaa-4777-8777-777777777777",
				Mode: "replace", Source: "copy", SourceFieldID: "66666666-aaaa-4666-8666-666666666666",
			}
		},
		"reversed between": func(schema *Schema) {
			schema.Flow.Edges[0].Condition.Conditions[0] = Condition{FieldID: flowExperience, Operator: "between", Value: []any{10, 2}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			schema := employmentFlowSchema()
			mutate(&schema)
			if err := schema.ValidateForPublish(); !errors.Is(err, ErrInvalidSchema) {
				t.Fatalf("ValidateForPublish() error = %v", err)
			}
		})
	}
}

func TestCopiedAssignmentHonorsTargetConstraints(t *testing.T) {
	t.Parallel()
	maximum := 10.0
	schema := employmentFlowSchema()
	schema.Pages[0].Fields = append(schema.Pages[0].Fields, Field{
		ID: "77777777-aaaa-4777-8777-777777777777", Type: FieldNumber, Label: "مقصد", Number: &NumberRule{Maximum: &maximum},
	})
	answers := map[string]any{flowExperience: float64(20)}
	applyAssignments(schema, []AssignmentAction{{
		TargetFieldID: "77777777-aaaa-4777-8777-777777777777", Mode: "replace", Source: "copy", SourceFieldID: flowExperience,
	}}, answers)
	if _, assigned := answers["77777777-aaaa-4777-8777-777777777777"]; assigned {
		t.Fatal("copy assignment bypassed target constraints")
	}
}
