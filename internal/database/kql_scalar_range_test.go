package database

import (
	"encoding/json"
	"math"
	"testing"
)

func TestScalarRangeNumericBoundaries(t *testing.T) {
	for _, tc := range []struct {
		start, stop, step any
		want              string
	}{
		{int64(math.MaxInt64 - 1), int64(math.MaxInt64), int64(1), `[9223372036854775806,9223372036854775807]`},
		{int64(math.MaxInt64 - 1), int64(math.MaxInt64), int64(2), `[9223372036854775806]`},
		{int64(math.MinInt64 + 1), int64(math.MinInt64), int64(-1), `[-9223372036854775807,-9223372036854775808]`},
		{int64(math.MaxInt64), int64(math.MinInt64), int64(math.MinInt64), `[9223372036854775807,-1]`},
		{int64(1), int64(1), int64(-1), `[1]`},
		{1.0, 3.0, 0.5, `[1,1.5,2,2.5,3]`},
		{math.MaxFloat64, math.MaxFloat64, 1.0, `[1.7976931348623157e+308]`},
	} {
		if got, err := kqlScalarRange(tc.start, tc.stop, tc.step); err != nil || got != tc.want {
			t.Fatalf("%v,%v,%v got %v want %s (%v)", tc.start, tc.stop, tc.step, got, tc.want, err)
		}
	}
	for _, input := range []any{nil, []byte(nil), "1"} {
		if got, err := kqlScalarRange(input, int64(3), int64(1)); err != nil || got != nil {
			t.Fatal("invalid input accepted")
		}
	}
	for _, tc := range [][3]any{{math.NaN(), 1.0, 1.0}, {0.0, math.Inf(1), 1.0}, {0.0, 1.0, math.Inf(1)}, {1e20, 1e21, 1.0}} {
		if got, err := kqlScalarRange(tc[0], tc[1], tc[2]); err == nil || got != nil {
			t.Fatal("unsupported real input accepted")
		}
	}
	for _, step := range []any{int64(0), 0.0} {
		if got, err := kqlScalarRange(int64(1), int64(3), step); err != nil || got != nil {
			t.Fatal("zero step policy changed")
		}
	}
}

func FuzzScalarRangeIntegers(f *testing.F) {
	f.Add(int64(1), int64(8), int64(3))
	f.Add(int64(math.MaxInt64), int64(math.MinInt64), int64(math.MinInt64))
	f.Fuzz(func(t *testing.T, start, stop, step int64) {
		// Keep fuzz cases short; the fixed maximum-length case executes in SQLite tests.
		if step == 0 {
			return
		}
		if (step > 0 && start <= stop && (uint64(stop)-uint64(start))/uint64(step) > 128) || (step < 0 && start >= stop && (uint64(start)-uint64(stop))/(uint64(-(step+1))+1) > 128) {
			return
		}
		result, err := kqlScalarRange(start, stop, step)
		if err != nil {
			t.Fatal(err)
		}
		var values []json.Number
		if json.Unmarshal([]byte(result.(string)), &values) != nil {
			t.Fatal("invalid JSON")
		}
		for i, raw := range values {
			value, err := raw.Int64()
			if err != nil {
				t.Fatal(err)
			}
			if i == 0 && value != start {
				t.Fatal("wrong first element")
			}
			if (step > 0 && value > stop) || (step < 0 && value < stop) {
				t.Fatal("endpoint crossed")
			}
			if i > 0 {
				previous, _ := values[i-1].Int64()
				if uint64(value)-uint64(previous) != uint64(step) || (step > 0 && value <= previous) || (step < 0 && value >= previous) {
					t.Fatal("progression or overflow error")
				}
			}
		}
	})
}
