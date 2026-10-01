package database

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSetResourceBounds(t *testing.T) {
	for _, operation := range []string{"set_union", "set_intersect", "set_difference"} {
		if _, err := kqlSetOperation(operation, "[]"); err == nil {
			t.Fatal("expected arity error")
		}
		if _, err := kqlSetOperation(operation, strings.Repeat("x", maxReplacementBytes+1), "[]"); err == nil {
			t.Fatal("expected input size error")
		}
		if _, err := kqlSetOperation(operation, `[`+strings.Repeat("0,", maxReplacementParts)+`0]`, "[]"); err == nil {
			t.Fatal("expected input element error")
		}
		if _, err := kqlSetOperation(operation, `[`+strings.Repeat("[", maxSetKeyDepth+1)+`0`+strings.Repeat("]", maxSetKeyDepth+1)+`]`, "[]"); err == nil {
			t.Fatal("expected nesting error")
		}
		if _, err := kqlSetOperation(operation, `[{"a":1,"a":2}]`, "[]"); err == nil {
			t.Fatal("expected duplicate object diagnostic")
		}
	}
	payload := strings.Repeat("x", maxReplacementBytes/2)
	if _, err := kqlSetOperation("set_union", `["a`+payload+`"]`, `["b`+payload+`"]`); err == nil {
		t.Fatal("expected output error")
	}
	values := make([]any, 64)
	for i := range values {
		values[i] = "[]" + strings.Repeat(" ", maxReplacementBytes-2)
	}
	if _, err := kqlSetOperation("set_union", values...); err == nil {
		t.Fatal("expected input work limit")
	}
	work := 0
	if _, err := setValueKey(json.RawMessage("1"), &work); err == nil {
		t.Fatal("expected comparison work limit")
	}
	work = maxReplacementWork
	hugeNested := `[` + strings.TrimSuffix(strings.Repeat("0,", maxReplacementParts), ",") + `]`
	if _, err := setValueKey(json.RawMessage(hugeNested), &work); err == nil {
		t.Fatal("expected nested node limit")
	}
	// Non-array/null input behavior remains characterization pending Kusto evidence.
	for _, value := range []any{nil, "null", "{}", "invalid"} {
		if got, err := kqlSetOperation("set_union", value, "[]"); err != nil || got != nil {
			t.Fatalf("invalid input: %v %v", got, err)
		}
	}
}
