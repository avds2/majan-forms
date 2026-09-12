// Package persiandate owns conversion only at presentation boundaries. Dates
// are persisted as Gregorian PostgreSQL date values and timestamps as UTC.
package persiandate

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	jalaali "github.com/jalaali/go-jalaali"

	"forms/internal/normalize"
)

var ErrInvalidDate = errors.New("تاریخ شمسی معتبر نیست")

type Date struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

func Parse(value string) (Date, error) {
	value = normalize.Digits(strings.TrimSpace(value))
	replacer := strings.NewReplacer("-", "/", ".", "/", "\\", "/")
	parts := strings.Split(replacer.Replace(value), "/")
	if len(parts) != 3 {
		return Date{}, ErrInvalidDate
	}
	values := make([]int, 3)
	for i, part := range parts {
		parsed, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return Date{}, ErrInvalidDate
		}
		values[i] = parsed
	}
	date := Date{Year: values[0], Month: values[1], Day: values[2]}
	if _, err := date.Gregorian(); err != nil {
		return Date{}, ErrInvalidDate
	}
	return date, nil
}

func (d Date) Gregorian() (time.Time, error) {
	if d.Month < 1 || d.Month > 12 || d.Day < 1 {
		return time.Time{}, ErrInvalidDate
	}
	year, month, day, err := jalaali.ToGregorian(d.Year, jalaali.Month(d.Month), d.Day)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %v", ErrInvalidDate, err)
	}
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC), nil
}

func FromGregorian(value time.Time) (Date, error) {
	year, month, day, err := jalaali.ToJalaali(value.Year(), value.Month(), value.Day())
	if err != nil {
		return Date{}, fmt.Errorf("%w: %v", ErrInvalidDate, err)
	}
	return Date{Year: year, Month: int(month), Day: day}, nil
}

func (d Date) String() string {
	return fmt.Sprintf("%04d/%02d/%02d", d.Year, d.Month, d.Day)
}

func (d Date) PersianString() string {
	return PersianDigits(d.String())
}

func FormatTime(value time.Time, location *time.Location) (string, error) {
	if location == nil {
		return "", errors.New("timezone is required")
	}
	local := value.In(location)
	date, err := FromGregorian(local)
	if err != nil {
		return "", err
	}
	return PersianDigits(fmt.Sprintf("%s، %02d:%02d", date.String(), local.Hour(), local.Minute())), nil
}

// PersianDigits converts ASCII digits for user-facing Persian text.
func PersianDigits(value string) string {
	return strings.NewReplacer(
		"0", "۰", "1", "۱", "2", "۲", "3", "۳", "4", "۴",
		"5", "۵", "6", "۶", "7", "۷", "8", "۸", "9", "۹",
	).Replace(value)
}
