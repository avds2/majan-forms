package normalize

import "testing"

func TestPersianText(t *testing.T) {
	t.Parallel()
	got := PersianText("  كيان  ۱۲٣  ")
	if got != "کیان 123" {
		t.Fatalf("PersianText() = %q", got)
	}
}

func TestSearchText(t *testing.T) {
	got := SearchText("  ALI\tكاظمي ۰۹۱۲ ")
	if got != "ali کاظمی 0912" {
		t.Fatalf("SearchText() = %q", got)
	}
}

func TestSlug(t *testing.T) {
	t.Parallel()
	if got := Slug("  Hiring_Form ۱۴۰۵ "); got != "hiring-form-1405" {
		t.Fatalf("Slug() = %q", got)
	}
}
