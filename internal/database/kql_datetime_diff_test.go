package database

import "testing"

func TestDatetimeDiffBoundaries(t *testing.T) {
	for _, tc := range []struct {
		period, left, right string
		want                int64
	}{
		{"SECOND", "1970-01-01", "1969-12-31T23:59:59.9999999Z", 1},
		{"minute", "1970-01-01", "1969-12-31T23:59:59.9999999Z", 1},
		{"day", "1970-01-01", "1969-12-31T23:59:59.9999999Z", 1},
		{"week", "1970-01-04", "1970-01-03T23:59:59.9999999Z", 1},
		{"week", "1970-01-05", "1970-01-04", 0},
		{"month", "2024-02-01", "2024-01-31T23:59:59.9999999Z", 1},
		{"microsecond", "9999-12-31T23:59:59.9999999Z", "0001-01-01", 315537897599999999},
		{"nanosecond", "2262-04-11T23:47:16.8547758Z", "1970-01-01", 9223372036854775800},
		{"nanosecond", "1970-01-01", "2262-04-11T23:47:16.8547758Z", -9223372036854775800},
	} {
		got, err := kqlDatetimeDiff(tc.period, tc.left, tc.right)
		if err != nil || got != tc.want {
			t.Fatalf("%+v got %v (%v)", tc, got, err)
		}
	}
	if got, err := kqlDatetimeDiff("nanosecond", "2262-04-11T23:47:16.8547759Z", "1970-01-01"); err == nil || got != nil {
		t.Fatal("overflow not rejected")
	}
	for _, period := range []any{nil, "bad", 1} {
		if got, err := kqlDatetimeDiff(period, "2024-01-01", "2024-01-01"); err != nil || got != nil {
			t.Fatal("invalid period accepted")
		}
	}
}

func FuzzDatetimeDiffSymmetry(f *testing.F) {
	f.Add(2024, 2, 29, 2017, 1, 1)
	f.Add(1, 1, 1, 9999, 12, 31)
	f.Fuzz(func(t *testing.T, y1, m1, d1, y2, m2, d2 int) {
		if y1 < 1 || y1 > 9999 || y2 < 1 || y2 > 9999 || m1 < 1 || m1 > 12 || m2 < 1 || m2 > 12 || d1 < 1 || d1 > 31 || d2 < 1 || d2 > 31 {
			return
		}
		a := kqlMakeDatetime(int64(y1), int64(m1), int64(d1))
		b := kqlMakeDatetime(int64(y2), int64(m2), int64(d2))
		if a == nil || b == nil {
			return
		}
		for _, period := range []string{"year", "quarter", "month", "week", "day", "hour", "minute", "second", "millisecond", "microsecond", "nanosecond"} {
			forward, err := kqlDatetimeDiff(period, a, b)
			backward, backErr := kqlDatetimeDiff(period, b, a)
			if err != nil || backErr != nil {
				if period != "nanosecond" || err == nil || backErr == nil {
					t.Fatal("asymmetric overflow")
				}
				continue
			}
			if forward == nil || backward == nil || forward.(int64) != -backward.(int64) {
				t.Fatal("subtraction symmetry failed")
			}
		}
	})
}
