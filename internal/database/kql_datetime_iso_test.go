package database

import (
	"strings"
	"testing"
	"time"

	"github.com/kawijayaa/striem/internal/eventtime"
)

func TestISODatetimeMinutePrecisionAndBounds(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"2014-05-25T08:20", "2014-05-25T08:20:00.000000000Z"},
		{"2014-11-08 15:55", "2014-11-08T15:55:00.000000000Z"},
		{"2014-05-25T08:20Z", "2014-05-25T08:20:00.000000000Z"},
		{"2014-05-25T08:20+02:30", "2014-05-25T05:50:00.000000000Z"},
		{"2014-05-25 08:20-02:30", "2014-05-25T10:50:00.000000000Z"},
		{"0001-01-01T01:00+01:00", "0001-01-01T00:00:00.000000000Z"},
		{"9999-12-31T22:59-01:00", "9999-12-31T23:59:00.000000000Z"},
		{"9999-12-31T23:59:59.9999999Z", "9999-12-31T23:59:59.999999900Z"},
	} {
		if got := kqlToDatetime(tc.input); got != tc.want {
			t.Fatalf("%s got %v want %s", tc.input, got, tc.want)
		}
	}
	for _, input := range []string{
		"0000-01-01", "0000-12-31T23:00-01:00", "0001-01-01T00:00+01:00", "9999-12-31T23:59-01:00",
		"2024-01-01T24:00", "2024-01-01T12:60", "2024-01-01T12:00.5", "2024-01-01T12:00+24:00", "2024-01-01T12:00+01:60",
		"2024-01-01T12:00+2:00", "2024-01-01T12:00z", "2024-01-01T12:00Zjunk", strings.Repeat("x", 1000000),
	} {
		if kqlToDatetime(input) != nil {
			t.Fatalf("invalid ISO input accepted: %s", input[:min(100, len(input))])
		}
	}
}

func FuzzISODatetimeNormalization(f *testing.F) {
	for _, input := range []string{"2024-02-29T12:34", "0001-01-01T00:00", "9999-12-31T23:59:59.9999999Z", "2024-01-01T01:00+01:00"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		output := kqlToDatetime(input)
		if output == nil {
			return
		}
		text, ok := output.(string)
		if !ok || len(text) != len(eventtime.Layout) {
			t.Fatal("unexpected datetime representation")
		}
		date, err := time.Parse(eventtime.Layout, text)
		if err != nil || date.Year() < 1 || date.Year() > 9999 {
			t.Fatal("invalid UTC datetime produced")
		}
		if kqlToDatetime(text) != text {
			t.Fatal("datetime normalization is not idempotent")
		}
	})
}
