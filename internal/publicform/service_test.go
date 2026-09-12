package publicform

import (
	"math"
	"net/url"
	"strings"
	"testing"
	"time"

	"forms/internal/form"
)

func TestParseFieldCanonicalizesPersianInputAndEnforcesBounds(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	service := &Service{location: location}

	minimum, maximum := 0.0, 10.0
	numberField := form.Field{ID: "number", Type: form.FieldNumber, Label: "عدد", Number: &form.NumberRule{Minimum: &minimum, Maximum: &maximum}}
	value, present, message := service.parseField(numberField, url.Values{"number": {"۷.۵"}})
	if message != "" || !present || value != 7.5 {
		t.Fatalf("Persian number = %#v, present=%v, message=%q", value, present, message)
	}
	for _, invalid := range []string{"NaN", "+Inf", "۱۱"} {
		if _, _, message := service.parseField(numberField, url.Values{"number": {invalid}}); message == "" {
			t.Fatalf("invalid number %q was accepted", invalid)
		}
	}

	dateField := form.Field{ID: "date", Type: form.FieldPersianDate, Label: "تاریخ", Date: &form.DateRule{Minimum: "2026-09-06", Maximum: "2026-09-06"}}
	value, present, message = service.parseField(dateField, url.Values{"date": {"۱۴۰۵/۰۶/۱۵"}})
	if message != "" || !present || value != "2026-09-06" {
		t.Fatalf("Persian date = %#v, present=%v, message=%q", value, present, message)
	}
	if _, _, message := service.parseField(dateField, url.Values{"date": {"۱۴۰۵/۰۶/۱۶"}}); message == "" {
		t.Fatal("out-of-range Persian date was accepted")
	}

	datetimeField := form.Field{ID: "datetime", Type: form.FieldDateTime, Label: "زمان"}
	value, present, message = service.parseField(datetimeField, url.Values{"datetime": {"۱۴۰۵/۰۶/۱۵ ۱۴:۳۰"}})
	if message != "" || !present || value != "2026-09-06T11:00:00Z" {
		t.Fatalf("Persian datetime = %#v, present=%v, message=%q", value, present, message)
	}

	percentField := form.Field{ID: "percent", Type: form.FieldPercent, Label: "درصد"}
	if _, _, message := service.parseField(percentField, url.Values{"percent": {"۱۰۱"}}); message == "" {
		t.Fatal("percent above 100 was accepted")
	}
	scaleField := form.Field{ID: "scale", Type: form.FieldScale, Label: "امتیاز", Scale: &form.ScaleRule{Minimum: 1, Maximum: 5}}
	if _, _, message := service.parseField(scaleField, url.Values{"scale": {"۲.۵"}}); message == "" {
		t.Fatal("fractional scale answer was accepted")
	}
}

func TestParseFieldValidatesStructuredAndChoiceAnswers(t *testing.T) {
	t.Parallel()
	service := &Service{location: time.UTC}
	one, two := form.Option{ID: "option-one", Label: "یک"}, form.Option{ID: "option-two", Label: "دو"}
	choice := form.Field{ID: "choice", Type: form.FieldSingleChoice, Label: "گزینه", Options: []form.Option{one, two}}
	if value, present, message := service.parseField(choice, url.Values{"choice": {one.ID}}); message != "" || !present || value != one.ID {
		t.Fatalf("stable option ID was rejected: value=%#v present=%v message=%q", value, present, message)
	}
	if _, _, message := service.parseField(choice, url.Values{"choice": {one.Label}}); message == "" {
		t.Fatal("option label was accepted instead of its stable ID")
	}
	multiple := form.Field{ID: "multiple", Type: form.FieldMultiChoice, Label: "چندگزینه", Options: []form.Option{one, two}}
	if _, _, message := service.parseField(multiple, url.Values{"multiple": {one.ID, one.ID}}); message == "" {
		t.Fatal("duplicate multiple-choice value was accepted")
	}

	matrix := form.Field{ID: "matrix", Type: form.FieldMatrix, Label: "ماتریس", Required: true, Matrix: &form.MatrixRule{
		Rows:    []form.Option{{ID: "row-one", Label: "ردیف یک"}, {ID: "row-two", Label: "ردیف دو"}},
		Columns: []form.Option{{ID: "column-one", Label: "ستون یک"}, {ID: "column-two", Label: "ستون دو"}},
	}}
	if _, _, message := service.parseField(matrix, url.Values{"row-one": {"column-one"}}); message == "" {
		t.Fatal("required matrix with an unanswered row was accepted")
	}
	value, present, message := service.parseField(matrix, url.Values{"row-one": {"column-one"}, "row-two": {"column-two"}})
	if message != "" || !present || !canonicalAnswerValid(matrix, value) {
		t.Fatalf("complete matrix was rejected: value=%#v present=%v message=%q", value, present, message)
	}

	fullName := form.Field{ID: "name", Type: form.FieldFullName, Label: "نام"}
	if _, _, message := service.parseField(fullName, url.Values{"name_first": {"تنها"}}); message == "" {
		t.Fatal("partial full name was accepted")
	}
	reversedRange := form.Field{ID: "range", Type: form.FieldDateRange, Label: "بازه"}
	if _, _, message := service.parseField(reversedRange, url.Values{"range_start": {"۱۴۰۵/۰۶/۲۰"}, "range_end": {"۱۴۰۵/۰۶/۱۵"}}); message == "" {
		t.Fatal("reversed date range was accepted")
	}
}

func TestCanonicalDateTimeUsesDisplayTimezoneForDateRules(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	field := form.Field{Type: form.FieldDateTime, Date: &form.DateRule{Minimum: "2026-09-07", Maximum: "2026-09-07"}}
	if !canonicalAnswerValidInLocation(field, "2026-09-06T21:00:00Z", location) {
		t.Fatal("local September 7 datetime was rejected by local-day rule")
	}
	if canonicalAnswerValidInLocation(field, "2026-09-06T19:00:00Z", location) {
		t.Fatal("local September 6 datetime bypassed local-day rule")
	}
}

func TestTextAndCanonicalValidationRejectMalformedValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		field form.Field
		value string
	}{
		{name: "email", field: form.Field{Type: form.FieldEmail}, value: "نام <person@example.com>"},
		{name: "phone", field: form.Field{Type: form.FieldPhone}, value: "۱۲۳"},
		{name: "insecure URL", field: form.Field{Type: form.FieldURL}, value: "http://example.com"},
		{name: "URL credentials", field: form.Field{Type: form.FieldURL}, value: "https://user:secret@example.com"},
		{name: "national ID", field: form.Field{Type: form.FieldNationalID}, value: "۰۰۰۰۰۰۰۰۰۰"},
		{name: "hard text cap", field: form.Field{Type: form.FieldLongText}, value: strings.Repeat("آ", 100_001)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if message := validateTextAnswer(test.field, test.value); message == "" {
				t.Fatalf("malformed value was accepted")
			}
		})
	}
	if message := validateTextAnswer(form.Field{Type: form.FieldEmail}, "person@example.com"); message != "" {
		t.Fatalf("valid email was rejected: %s", message)
	}
	if message := validateTextAnswer(form.Field{Type: form.FieldPhone}, "۰۹۱۲ ۱۲۳ ۴۵۶۷"); message != "" {
		t.Fatalf("valid Persian-digit phone was rejected: %s", message)
	}
	if message := validateTextAnswer(form.Field{Type: form.FieldNationalID}, "۰۰۱۳۵۴۷۳۹۹"); message != "" {
		t.Fatalf("valid Persian-digit national ID was rejected: %s", message)
	}
	if canonicalAnswerValid(form.Field{Type: form.FieldNumber}, math.NaN()) {
		t.Fatal("non-finite canonical number was accepted")
	}
}
