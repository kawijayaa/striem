package database

import (
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
