package database

import (
	"strings"
	"testing"
)

func TestBagOperationsBounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func() (any, error)
	}{
		{"merge arity", func() (any, error) { return kqlBagMerge("{}") }},
		{"merge input", func() (any, error) { return kqlBagMerge(strings.Repeat("x", maxBagBytes+1), "{}") }},
		{"merge output", func() (any, error) {
			v := strings.Repeat("x", maxBagBytes/2)
			return kqlBagMerge(`{"a":"`+v+`"}`, `{"b":"`+v+`"}`)
		}},
		{"nonarray keys", func() (any, error) { return kqlBagRemoveKeys("{}", "null") }},
		{"nonstring keys", func() (any, error) { return kqlBagRemoveKeys("{}", "[1]") }},
		{"array path", func() (any, error) { return kqlBagRemoveKeys("{}", `["$.a[0]"]`) }},
		{"root path", func() (any, error) { return kqlBagRemoveKeys("{}", `["$"]`) }},
		{"long path", func() (any, error) { return kqlBagRemoveKeys("{}", `["`+strings.Repeat("a", maxBagPathBytes+1)+`"]`) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.run(); err == nil {
				t.Fatal("expected diagnostic")
			}
		})
	}
	for _, value := range []any{nil, int64(1), "null", "[]", "invalid"} {
		if got, err := kqlBagMerge(value, "{}"); err != nil || got != nil {
			t.Fatalf("invalid object: %v %v", got, err)
		}
	}
	got, err := kqlBagRemoveKeys(`{"a\u0000é":1,"keep":true}`, `["a\u0000é"]`)
	if err != nil || got != `{"keep":true}` {
		t.Fatalf("unicode/NUL key: %v %v", got, err)
	}
}
