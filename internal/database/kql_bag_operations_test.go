package database

import (
	"encoding/json"
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

func TestBagSetKeyBounds(t *testing.T) {
	for _, tc := range []struct {
		name     string
		bag, key string
		value    any
	}{
		{"input", strings.Repeat("x", maxBagBytes+1), "a", 1},
		{"value", "{}", "a", strings.Repeat("x", maxBagBytes+1)},
		{"output", `{"a":"` + strings.Repeat("x", maxBagBytes/2) + `"}`, "b", strings.Repeat("x", maxBagBytes/2)},
		{"path", "{}", strings.Repeat("a", maxBagPathBytes+1), 1},
		{"root", "{}", "$", 1},
		{"array indexing", "{}", "$.a[0]", 1},
		{"depth", "{}", "$" + strings.Repeat(".a", maxBagPathDepth+1), 1},
		{"nonobject parent", `{"a":1}`, "$.a.b", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := kqlBagSetKey(tc.bag, tc.key, tc.value, "string"); err == nil {
				t.Fatal("expected diagnostic")
			}
		})
	}
	got, err := kqlBagSetKey(`{}`, "a\x00é", int64(3), "long")
	if err != nil || got != `{"a\u0000é":3}` {
		t.Fatalf("unicode/NUL key: %v %v", got, err)
	}
	work := 0
	if _, err := setBagProperty(map[string]json.RawMessage{}, []string{"a"}, json.RawMessage("1"), &work); err == nil {
		t.Fatal("expected work limit")
	}
}

func TestBagZipResourceBounds(t *testing.T) {
	for _, tc := range []struct{ name, keys, values string }{
		{"keys input", strings.Repeat("x", maxBagBytes+1), "[]"},
		{"values input", `["a"]`, strings.Repeat("x", maxBagBytes+1)},
		{"keys count", `[` + strings.Repeat("0,", maxReplacementParts) + `0]`, "[]"},
		{"values count", "[]", `[` + strings.Repeat("0,", maxReplacementParts) + `0]`},
		{"output", `["a"]`, `["` + strings.Repeat("x", maxBagBytes-4) + `"]`},
		{"escaped output", `["a"]`, `["` + strings.Repeat("&", maxBagBytes/2) + `"]`},
		{"duplicates", `["a","a"]`, "[1,2]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := kqlBagZip(tc.keys, tc.values); err == nil {
				t.Fatal("expected diagnostic")
			}
		})
	}
}
