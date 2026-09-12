package form

import (
	"errors"
	"testing"

	"forms/internal/id"
)

func TestNewEmptySchemaValid(t *testing.T) {
	t.Parallel()
	schema, err := NewEmpty("فرم استخدام")
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Validate(); err != nil {
		t.Fatal(err)
	}
	a, checksumA, err := EncodeAndChecksum(schema)
	if err != nil {
		t.Fatal(err)
	}
	b, checksumB, err := EncodeAndChecksum(schema)
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) || checksumA != checksumB {
		t.Fatal("schema checksum is not deterministic")
	}
}

func TestSchemaRejectsDuplicateIDsAndBadChoice(t *testing.T) {
	t.Parallel()
	schema, err := NewEmpty("آزمون")
	if err != nil {
		t.Fatal(err)
	}
	fieldID, _ := id.New()
	schema.Pages[0].Fields = []Field{
		{ID: fieldID, Type: FieldSingleChoice, Label: "انتخاب", Options: []Option{}},
		{ID: fieldID, Type: FieldShortText, Label: "تکراری"},
	}
	if err := schema.Validate(); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestSchemaRejectsDateOutsidePersianCalendarRange(t *testing.T) {
	t.Parallel()
	schema, err := NewEmpty("آزمون تاریخ")
	if err != nil {
		t.Fatal(err)
	}
	fieldID, err := id.New()
	if err != nil {
		t.Fatal(err)
	}
	schema.Pages[0].Fields = append(schema.Pages[0].Fields, Field{
		ID: fieldID, Type: FieldPersianDate, Label: "تاریخ",
		Date: &DateRule{Minimum: "0001-01-01"},
	})
	if err := schema.Validate(); !errors.Is(err, ErrInvalidSchema) {
		t.Fatalf("Validate() error = %v, want ErrInvalidSchema", err)
	}
}
