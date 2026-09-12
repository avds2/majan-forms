package persiandate

import (
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"
)

type dateFixture struct {
	Persian string `json:"persian"`
	ISO     string `json:"iso"`
}

func TestParseAndGregorianRoundTrip(t *testing.T) {
	t.Parallel()
	contents, err := os.ReadFile("../../testdata/persian-date-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var tests []dateFixture
	if err := json.Unmarshal(contents, &tests); err != nil {
		t.Fatal(err)
	}
	for _, test := range tests {
		date, err := Parse(test.Persian)
		if err != nil {
			t.Fatalf("Parse(%q): %v", test.Persian, err)
		}
		gregorian, err := date.Gregorian()
		if err != nil {
			t.Fatalf("Gregorian(%q): %v", test.Persian, err)
		}
		if got := gregorian.Format(time.DateOnly); got != test.ISO {
			t.Fatalf("Gregorian(%q) = %q, want %q", test.Persian, got, test.ISO)
		}
		roundTrip, err := FromGregorian(gregorian)
		if err != nil || roundTrip != date {
			t.Fatalf("round trip %q = %#v, %v", test.Persian, roundTrip, err)
		}
	}
}

func TestInvalidNonLeapEsfand(t *testing.T) {
	t.Parallel()
	_, err := Parse("۱۴۰۰/۱۲/۳۰")
	if !errors.Is(err, ErrInvalidDate) {
		t.Fatalf("Parse() error = %v, want ErrInvalidDate", err)
	}
}

func TestTehranNearMidnight(t *testing.T) {
	t.Parallel()
	location, err := time.LoadLocation("Asia/Tehran")
	if err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, 3, 20, 21, 30, 0, 0, time.UTC)
	got, err := FormatTime(instant, location)
	if err != nil {
		t.Fatal(err)
	}
	if got != "۱۴۰۵/۰۱/۰۱، ۰۱:۰۰" {
		t.Fatalf("FormatTime() = %q", got)
	}
}
