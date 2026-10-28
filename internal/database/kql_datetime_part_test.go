package database

import "testing"

func TestDatetimePartCalendarBoundaries(t *testing.T) {
	for _, tc := range []struct {
		part, date string
		want       int64
	}{
		{"week_of_year", "2021-01-01", 53}, {"week_of_year", "2021-01-04", 1},
		{"week_of_year", "2016-01-01", 53}, {"week_of_year", "0001-01-01", 1},
		{"dayofyear", "2024-12-31", 366}, {"dayofyear", "1900-12-31", 365},
		{"year", "2024-01-01T00:30:00+01:00", 2023},
		{"month", "2024-01-01T00:30:00+01:00", 12},
		{"quarter", "2024-04-01", 2}, {"nanosecond", "2024-01-01T00:00:00.999999999Z", 999999900},
		{"YEAR", "9999-12-31", 9999},
	} {
		if got := kqlDatetimePart(tc.part, tc.date); got != tc.want {
			t.Fatalf("%+v got %v", tc, got)
		}
	}
	for _, value := range []any{nil, 1, "bad", "0000-01-01"} {
		if kqlDatetimePart("year", value) != nil {
			t.Fatal("invalid date accepted")
		}
	}
	for _, part := range []any{nil, 1, "bad", "weekofyear"} {
		if kqlDatetimePart(part, "2024-01-01") != nil {
			t.Fatal("invalid or unsupported part accepted")
		}
	}
}

func FuzzDatetimePartReconstruction(f *testing.F) {
	f.Add(2024, 2, 29, int64(1234567))
	f.Add(1, 1, 1, int64(0))
	f.Fuzz(func(t *testing.T, year, month, day int, ticks int64) {
		if year < 1 || year > 9999 || month < 1 || month > 12 || day < 1 || day > 31 || ticks < 0 || ticks >= 600000000 {
			return
		}
		source := kqlMakeDatetime(int64(year), int64(month), int64(day), int64(12), int64(34), float64(ticks)/10000000)
		if source == nil {
			return
		}
		parts := make([]any, 0, 6)
		for _, part := range []string{"year", "month", "day", "hour", "minute"} {
			value := kqlDatetimePart(part, source)
			if value == nil {
				t.Fatal("valid date rejected")
			}
			parts = append(parts, value)
		}
		seconds := kqlDatetimePart("second", source).(int64)
		nano := kqlDatetimePart("nanosecond", source).(int64)
		parts = append(parts, float64(seconds)+float64(nano)/1e9)
		if got := kqlMakeDatetime(parts...); got != source {
			t.Fatal("extracted components did not reconstruct date")
		}
		week := kqlDatetimePart("week_of_year", source).(int64)
		if week < 1 || week > 53 {
			t.Fatal("invalid ISO week")
		}
	})
}
