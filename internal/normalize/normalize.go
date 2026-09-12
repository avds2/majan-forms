// Package normalize contains the shared server-side Persian normalization
// rules. Original answers are never changed; normalized values are for lookup.
package normalize

import (
	"strings"
	"unicode"
)

var digitReplacer = strings.NewReplacer(
	"۰", "0", "۱", "1", "۲", "2", "۳", "3", "۴", "4",
	"۵", "5", "۶", "6", "۷", "7", "۸", "8", "۹", "9",
	"٠", "0", "١", "1", "٢", "2", "٣", "3", "٤", "4",
	"٥", "5", "٦", "6", "٧", "7", "٨", "8", "٩", "9",
)

var letterReplacer = strings.NewReplacer(
	"ي", "ی",
	"ى", "ی",
	"ك", "ک",
	"ۀ", "ه",
	"ة", "ه",
)

// Digits converts Persian and Arabic-Indic digits to ASCII digits.
func Digits(value string) string { return digitReplacer.Replace(value) }

// PersianText normalizes lookup-significant Arabic/Persian variants and
// collapses whitespace. It intentionally does not mutate stored source text.
func PersianText(value string) string {
	value = Digits(letterReplacer.Replace(strings.TrimSpace(value)))
	return strings.Join(strings.Fields(value), " ")
}

// SearchText applies the canonical Persian lookup rules and folds Latin
// letters. The returned value is an index key; source values remain untouched.
func SearchText(value string) string {
	return strings.ToLower(PersianText(value))
}

// LoginIdentifier applies deterministic normalization for email/username lookup.
func LoginIdentifier(value string) string {
	return SearchText(value)
}

// Slug prepares an ASCII URL slug. Validation remains the caller's job.
func Slug(value string) string {
	value = strings.ToLower(Digits(strings.TrimSpace(value)))
	var out strings.Builder
	dash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
			dash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if out.Len() > 0 && !dash {
				out.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(out.String(), "-")
}
