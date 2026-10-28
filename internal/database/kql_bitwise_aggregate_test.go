package database

import (
	"math"
	"testing"
)

func TestBitwiseAggregateStateAndHex(t *testing.T) {
	for _, op := range []string{"and", "or", "xor"} {
		state := &bitwiseAggregate{operation: op}
		if state.Done() != nil {
			t.Fatal("empty result is not null")
		}
		for _, value := range []any{nil, []byte(nil)} {
			if err := state.Step(value); err != nil {
				t.Fatal(err)
			}
		}
		if state.Done() != nil {
			t.Fatal("null was not skipped")
		}
		if err := state.Step(int64(math.MinInt64)); err != nil {
			t.Fatal(err)
		}
		if state.Done() != int64(math.MinInt64) {
			t.Fatal("first value changed")
		}
		if err := state.Step("1"); err == nil {
			t.Fatal("invalid runtime type skipped")
		}
	}
	for _, tc := range []struct {
		v     int64
		kind  string
		width int64
		want  string
	}{
		{math.MinInt64, "long", 1, "8000000000000000"}, {math.MaxInt64, "long", 1, "7fffffffffffffff"},
		{-1, "int", 1, "ffffffff"}, {-1, "int", 16, "00000000ffffffff"}, {-1, "long", 1, "ffffffffffffffff"},
		{0, "long", 1, "0"}, {math.MinInt32, "int", 1, "80000000"},
	} {
		if got := kqlToHex(tc.v, tc.kind, tc.width); got != tc.want {
			t.Fatalf("hex %+v got %v", tc, got)
		}
	}
	if kqlToHex(int64(math.MaxInt32+1), "int", int64(1)) != nil || kqlToHex(int64(1), "long", int64(-1)) != nil || kqlToHex(nil, "long", int64(1)) != nil {
		t.Fatal("invalid hex input accepted")
	}
}

func FuzzBitwiseAggregatePartitioning(f *testing.F) {
	f.Add(int64(7), int64(3), int64(-1))
	f.Add(int64(math.MinInt64), int64(math.MaxInt64), int64(0))
	f.Fuzz(func(t *testing.T, a, b, c int64) {
		for _, operation := range []string{"and", "or", "xor"} {
			total := &bitwiseAggregate{operation: operation}
			first := &bitwiseAggregate{operation: operation}
			second := &bitwiseAggregate{operation: operation}
			for _, value := range []int64{a, b, c} {
				if err := total.Step(value); err != nil {
					t.Fatal(err)
				}
			}
			first.Step(a)
			second.Step(b)
			second.Step(c)
			first.Step(second.Done())
			if total.Done() != first.Done() {
				t.Fatal("partitioned aggregation disagrees")
			}
			reverse := &bitwiseAggregate{operation: operation}
			reverse.Step(c)
			reverse.Step(nil)
			reverse.Step(b)
			reverse.Step(a)
			if total.Done() != reverse.Done() {
				t.Fatal("ordering or null altered aggregation")
			}
		}
	})
}
