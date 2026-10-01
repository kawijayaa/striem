package database

import (
	"math"
	"strings"
	"testing"
)

func TestArrayOperationsResourceBounds(t *testing.T) {
	oversized := strings.Repeat("x", maxReplacementBytes+1)
	many := `[` + strings.Repeat("0,", maxReplacementParts) + `0]`
	half := `["` + strings.Repeat("x", maxReplacementBytes/2) + `"]`
	for _, tc := range []struct {
		name string
		run  func() (any, error)
	}{
		{"reverse input", func() (any, error) { return kqlArrayReverse(oversized) }},
		{"concat input", func() (any, error) { return kqlArrayConcat(oversized) }},
		{"reverse elements", func() (any, error) { return kqlArrayReverse(many) }},
		{"concat elements", func() (any, error) { return kqlArrayConcat(many) }},
		{"total elements", func() (any, error) {
			a := `[` + strings.TrimSuffix(strings.Repeat("0,", maxReplacementParts/2+1), ",") + `]`
			return kqlArrayConcat(a, a)
		}},
		{"output", func() (any, error) { return kqlArrayConcat(half, half) }},
		{"arity", func() (any, error) { return kqlArrayConcat() }},
		{"work", func() (any, error) {
			v := make([]any, 64)
			for i := range v {
				v[i] = "[]" + strings.Repeat(" ", maxReplacementBytes-2)
			}
			return kqlArrayConcat(v...)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.run(); err == nil {
				t.Fatal("expected resource diagnostic")
			}
		})
	}
}

// Null/invalid array handling is characterized here; differential Kusto
// evidence is still required for those inputs.
func TestArrayOperationsInvalidInputs(t *testing.T) {
	for _, value := range []any{nil, int64(1), "null", "{}", "[1", "[1] false", "[1] ]", "[1,]"} {
		if got, err := kqlArrayReverse(value); err != nil || got != nil {
			t.Fatalf("reverse(%v): %v %v", value, got, err)
		}
		if got, err := kqlArrayConcat("[]", value); err != nil || got != nil {
			t.Fatalf("concat(%v): %v %v", value, got, err)
		}
	}
	got, err := kqlArrayReverse(` [ 9007199254740993, { "a" : [true, null] } ] `)
	if err != nil || got != `[{"a":[true,null]},9007199254740993]` {
		t.Fatalf("raw JSON: %v %v", got, err)
	}
}

func TestArraySliceResourceAndIntegerBounds(t *testing.T) {
	for _, value := range []string{strings.Repeat("x", maxReplacementBytes+1), `[` + strings.Repeat("0,", maxReplacementParts) + `0]`} {
		if _, err := kqlArraySlice(value, int64(0), int64(0)); err == nil {
			t.Fatal("expected resource diagnostic")
		}
	}
	for _, tc := range []struct {
		start, end int64
		want       string
	}{
		{math.MinInt64, math.MaxInt64, "[1,2,3]"},
		{math.MaxInt64, math.MaxInt64, "[]"},
		{math.MinInt64, math.MinInt64, "[]"},
		{-3, -3, "[1]"},
		{-4, 0, "[1]"},
	} {
		got, err := kqlArraySlice("[1,2,3]", tc.start, tc.end)
		if err != nil || got != tc.want {
			t.Fatalf("slice(%d,%d): %v %v", tc.start, tc.end, got, err)
		}
	}
	// These null/invalid-index cases remain characterization pending Kusto evidence.
	for _, index := range []any{nil, "1", float64(1)} {
		if got, err := kqlArraySlice("[1,2,3]", index, int64(2)); err != nil || got != nil {
			t.Fatalf("invalid start: %v %v", got, err)
		}
		if got, err := kqlArraySlice("[1,2,3]", int64(0), index); err != nil || got != nil {
			t.Fatalf("invalid end: %v %v", got, err)
		}
	}
	for _, value := range []any{nil, "null", "{}", "invalid"} {
		if got, err := kqlArraySlice(value, int64(0), int64(2)); err != nil || got != nil {
			t.Fatalf("invalid array: %v %v", got, err)
		}
	}
}

func TestZipBounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func() (any, error)
	}{
		{"arity", func() (any, error) { return kqlZip("[]") }},
		{"input", func() (any, error) { return kqlZip(strings.Repeat("x", maxReplacementBytes+1), "[]") }},
		{"cells", func() (any, error) {
			v := `[` + strings.TrimSuffix(strings.Repeat("0,", maxReplacementParts/2+1), ",") + `]`
			return kqlZip(v, "[]")
		}},
		{"output", func() (any, error) {
			v := `["` + strings.Repeat("x", maxReplacementBytes/2) + `"]`
			return kqlZip(v, v)
		}},
		{"work", func() (any, error) {
			v := make([]any, 64)
			for i := range v {
				v[i] = "[]" + strings.Repeat(" ", maxReplacementBytes-2)
			}
			return kqlZip(v...)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.run(); err == nil {
				t.Fatal("expected resource diagnostic")
			}
		})
	}
	// Invalid input behavior is characterized pending differential Kusto evidence.
	for _, value := range []any{nil, "null", "{}", "invalid", int64(1)} {
		if got, err := kqlZip("[]", value); err != nil || got != nil {
			t.Fatalf("invalid zip: %v %v", got, err)
		}
	}
}

func TestArraySplitBounds(t *testing.T) {
	for _, tc := range []struct {
		name           string
		value, indices any
	}{
		{"input", strings.Repeat("x", maxReplacementBytes+1), int64(0)},
		{"indices input", "[]", strings.Repeat("x", maxReplacementBytes+1)},
		{"segments", "[]", `[` + strings.TrimSuffix(strings.Repeat("0,", maxReplacementParts), ",") + `]`},
		{"output", `["` + strings.Repeat("x", maxReplacementBytes-4) + `"]`, int64(0)},
		{"negative bound", "[1]", math.MinInt64},
		{"positive bound", "[1]", math.MaxInt64},
		{"descending", "[1,2,3]", "[2,1]"},
		{"fractional", "[1,2,3]", "[1.5]"},
		{"overflow", "[1,2,3]", "[9223372036854775808]"},
		{"string index", "[1,2,3]", `["1"]`},
		{"null index", "[1,2,3]", "[null]"},
		{"invalid index", "[1,2,3]", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := kqlArraySplit(tc.value, tc.indices); err == nil {
				t.Fatal("expected diagnostic")
			}
		})
	}
	// Non-array input behavior remains characterization pending Kusto evidence.
	for _, value := range []any{nil, "null", "{}", "invalid"} {
		if got, err := kqlArraySplit(value, int64(0)); err != nil || got != nil {
			t.Fatalf("invalid input: %v %v", got, err)
		}
	}
}

func TestArraySumBoundsAndInvalidValues(t *testing.T) {
	for _, value := range []string{strings.Repeat("x", maxReplacementBytes+1), `[` + strings.Repeat("0,", maxReplacementParts) + `0]`, `[1e999]`, `[1e308,1e308]`} {
		if _, err := kqlArraySum(value); err == nil {
			t.Fatal("expected input/finite-real diagnostic")
		}
	}
	for _, value := range []any{nil, "null", "{}", "invalid", `[1e999,"x"]`, `[1e308,1e308,null]`} {
		if got, err := kqlArraySum(value); err != nil || got != nil {
			t.Fatalf("null/non-numeric result: %v %v", got, err)
		}
	}
	// Empty arrays and accumulation rounding remain characterization pending Kusto evidence.
	got, err := kqlArraySum("[]")
	if err != nil || got != float64(0) {
		t.Fatalf("empty array: %v %v", got, err)
	}
	got, err = kqlArraySum("[0.1,0.2]")
	if err != nil || math.Abs(got.(float64)-0.3) > 1e-15 {
		t.Fatalf("real sum: %v %v", got, err)
	}
}
