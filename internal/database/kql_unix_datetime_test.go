package database

import (
	"math"
	"testing"
	"time"

	"github.com/kawijayaa/striem/internal/eventtime"
)

func TestUnixDatetimeBoundaries(t *testing.T) {
	for _, tc := range []struct {
		value any
		scale int64
		want  string
	}{
		{int64(-62135596800), 1, "0001-01-01T00:00:00.000000000Z"},
		{int64(253402300799), 1, "9999-12-31T23:59:59.000000000Z"},
		{int64(253402300799999999), 1000000, "9999-12-31T23:59:59.999999000Z"},
		{int64(math.MaxInt64), 1000000000, "2262-04-11T23:47:16.854775800Z"},
		{int64(math.MinInt64), 1000000000, "1677-09-21T00:12:43.145224200Z"},
		{int64(-99), 1000000000, "1970-01-01T00:00:00.000000000Z"},
		{int64(-199), 1000000000, "1969-12-31T23:59:59.999999900Z"},
		{0.3, 1, "1970-01-01T00:00:00.300000000Z"},
		{0.00000019, 1, "1970-01-01T00:00:00.000000100Z"},
	} {
		if got := unixKQLDatetime(tc.value, tc.scale); got != tc.want {
			t.Fatalf("%+v got %v", tc, got)
		}
	}
	for _, value := range []any{nil, []byte(nil), "1", math.NaN(), math.Inf(1), math.MaxFloat64, int64(-62135596801), int64(253402300800), int64(math.MaxInt64), int64(math.MinInt64)} {
		if kqlUnixSeconds(value) != nil {
			t.Fatal("invalid or out-of-range input accepted")
		}
	}
}

func FuzzUnixIntegerPrecision(f *testing.F) {
	f.Add(int64(1546300800123456700))
	f.Add(int64(math.MaxInt64))
	f.Add(int64(math.MinInt64))
	f.Fuzz(func(t *testing.T, nanoseconds int64) {
		for _, unit := range []int64{1, 1000, 1000000, 1000000000} {
			value := nanoseconds / (1000000000 / unit)
			var expected time.Time
			if unit == 1000000000 {
				expected = time.Unix(0, value/100*100)
			} else {
				expected = time.Unix(0, value*(1000000000/unit))
			}
			// All selected values lie within the signed-long nanosecond interval.
			if got := unixKQLDatetime(value, unit); got != eventtime.Format(expected) {
				t.Fatalf("unit=%d value=%d got %v want %s", unit, value, got, eventtime.Format(expected))
			}
		}
	})
}
