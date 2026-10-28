package database

import (
	"math"
	"testing"
	"time"
)

func TestDatetimeConstructionBoundaries(t *testing.T) {
	for _, parts := range [][]any{
		{int64(0), int64(1), int64(1)}, {int64(10000), int64(1), int64(1)},
		{int64(2024), int64(0), int64(1)}, {int64(2024), int64(2), int64(30)},
		{int64(2024), int64(1), int64(1), int64(24)}, {int64(2024), int64(1), int64(1), int64(0), int64(60)},
		{int64(2024), int64(1), int64(1), int64(0), int64(0), math.NaN()},
		{int64(2024), int64(1), int64(1), int64(0), int64(0), 60.0},
		{int64(2024), int64(1), nil},
	} {
		if kqlMakeDatetime(parts...) != nil {
			t.Fatalf("invalid components accepted: %v", parts)
		}
	}
	for _, tc := range []struct {
		period     string
		amount     int64
		date, want string
	}{
		{"day", 3652058, "0001-01-01", "9999-12-31T00:00:00.000000000Z"},
		{"day", -3652058, "9999-12-31", "0001-01-01T00:00:00.000000000Z"},
		{"day", 1, "9999-12-31", ""}, {"nanosecond", -100, "0001-01-01", ""},
		{"month", math.MaxInt64, "2024-01-01", ""}, {"week", math.MinInt64, "2024-01-01", ""},
		{"MONTH", -1, "2024-03-31", "2024-02-29T00:00:00.000000000Z"},
		{"nanosecond", -100, "1970-01-01", "1969-12-31T23:59:59.999999900Z"},
		{"microsecond", 1, "2024-01-01", "2024-01-01T00:00:00.000001000Z"},
		{"millisecond", 1, "2024-01-01", "2024-01-01T00:00:00.001000000Z"},
		{"nanosecond", 99, "2024-01-01", "2024-01-01T00:00:00.000000000Z"},
		{"bad", 1, "2024-01-01", ""},
	} {
		got := kqlDatetimeAdd(tc.period, tc.amount, tc.date)
		if tc.want == "" {
			if got != nil {
				t.Fatalf("invalid addition accepted: %+v", tc)
			}
		} else if got != tc.want {
			t.Fatalf("addition %+v got %v", tc, got)
		}
	}
	if got := kqlMakeDatetime(int64(9999), int64(12), int64(31), int64(23), int64(59), 59.9999999); got != "9999-12-31T23:59:59.999999900Z" {
		t.Fatalf("upper boundary %v", got)
	}
}

func FuzzDatetimeCalendarAddition(f *testing.F) {
	f.Add(2024, 1, 31, int64(1))
	f.Add(1, 1, 1, int64(-1))
	f.Fuzz(func(t *testing.T, year, month, day int, delta int64) {
		if year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 {
			return
		}
		source := kqlMakeDatetime(int64(year), int64(month), int64(day))
		if source == nil {
			return
		}
		result := kqlDatetimeAdd("month", delta, source)
		if delta < -119988 || delta > 119988 {
			if result != nil {
				t.Fatal("overflow accepted")
			}
			return
		}
		target := int64(year-1)*12 + int64(month-1) + delta
		if target < 0 || target >= 9999*12 {
			if result != nil {
				t.Fatal("out-of-range date accepted")
			}
			return
		}
		if result == nil {
			t.Fatal("valid month shift rejected")
		}
		date, ok := parsedDatetime(result)
		if !ok {
			t.Fatal("invalid datetime result")
		}
		if int64(date.Year()-1)*12+int64(date.Month()-1) != target || date.Day() > day {
			t.Fatal("calendar shift changed wrong component")
		}
		last := time.Date(date.Year(), date.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
		if date.Day() != min(day, last) {
			t.Fatal("month-end clamp failed")
		}
	})
}
