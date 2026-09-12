package response

import (
	"testing"
	"time"

	"forms/internal/form"
)

func TestFormulaSafe(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		input, want string
	}{
		{"=2+3", "'=2+3"},
		{"  -10+CMD", "'  -10+CMD"},
		{"@SUM(A1:A2)", "'@SUM(A1:A2)"},
		{"\t=unsafe", "'\t=unsafe"},
		{"متن امن", "متن امن"},
		{"42", "42"},
	} {
		if got := formulaSafe(test.input); got != test.want {
			t.Errorf("formulaSafe(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

func TestPrefixSearchQueryUsesSafeLexemes(t *testing.T) {
	t.Parallel()
	got := prefixSearchQuery("کیان ۸.۵ | test@example")
	if got != "کیان:* & ۸:* & ۵:* & test:* & example:*" {
		t.Fatalf("prefixSearchQuery() = %q", got)
	}
}

func TestSpreadsheetReferencesAndDates(t *testing.T) {
	t.Parallel()
	if got := cellReference(28, 12); got != "AB12" {
		t.Fatalf("cellReference() = %q", got)
	}
	date := time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC)
	if got := excelSerial(date, false); got != "2.0000000000" {
		t.Fatalf("excelSerial() = %q", got)
	}
}

func TestDisplayValueUsesVersionLabelsAndJalali(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	choice := form.Field{Type: form.FieldSingleChoice, Options: []form.Option{{ID: "choice-1", Label: "انتخاب فارسی"}}}
	if got := displayValue(choice, "choice-1", location); got != "انتخاب فارسی" {
		t.Fatalf("choice display = %q", got)
	}
	date := form.Field{Type: form.FieldPersianDate}
	if got := displayValue(date, "2026-09-07", location); got != "۱۴۰۵/۰۶/۱۶" {
		t.Fatalf("date display = %q", got)
	}
}

func TestChoiceBreakdownsUseLatestLabelAcrossVersions(t *testing.T) {
	t.Parallel()
	fieldID := "11111111-1111-4111-8111-111111111111"
	optionID := "22222222-2222-4222-8222-222222222222"
	versions := []versionSchema{
		{ID: "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", VersionNumber: 2, Schema: form.Schema{Pages: []form.Page{{Fields: []form.Field{{ID: fieldID, Type: form.FieldSingleChoice, Label: "عنوان تازه", Options: []form.Option{{ID: optionID, Label: "گزینهٔ تازه"}}}}}}}},
		{ID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", VersionNumber: 1, Schema: form.Schema{Pages: []form.Page{{Fields: []form.Field{{ID: fieldID, Type: form.FieldSingleChoice, Label: "عنوان قدیمی", Options: []form.Option{{ID: optionID, Label: "گزینهٔ قدیمی"}}}}}}}},
	}
	byID := map[string]versionSchema{versions[0].ID: versions[0], versions[1].ID: versions[1]}
	got := buildChoiceBreakdowns([]rawChoice{{VersionID: zeroUUID, FieldID: fieldID, Value: optionID, Count: 4}}, versions, byID)
	if len(got) != 1 || !got[0].AcrossVersions || got[0].FieldLabel != "عنوان تازه" || got[0].Values[0].Label != "گزینهٔ تازه" {
		t.Fatalf("cross-version breakdown = %#v", got)
	}
}

func TestLocalDayBoundsUseConfiguredTimezoneAndExclusiveEnd(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	to := from
	fromBound, toBound := localDayBounds(&from, &to, location)
	if got, want := fromBound.Format(time.RFC3339), "2026-09-06T20:30:00Z"; got != want {
		t.Fatalf("from bound=%s want=%s", got, want)
	}
	if got, want := toBound.Format(time.RFC3339), "2026-09-07T20:30:00Z"; got != want {
		t.Fatalf("exclusive to bound=%s want=%s", got, want)
	}
}
