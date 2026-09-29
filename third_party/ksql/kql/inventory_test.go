package kql

import "testing"

func TestEveryQueryOperatorParsesAsRecognized(t *testing.T) {
	// Operators with required typed bodies are covered by focused parser tests.
	// Raw-body operators must still remain distinguishable from unknown syntax.
	typed := map[string]bool{
		"where": true, "filter": true, "take": true, "limit": true,
		"sample": true, "sample-distinct": true, "project": true,
		"project-away": true, "project-by-names": true, "project-keep": true,
		"project-rename": true, "project-reorder": true, "extend": true,
		"distinct": true, "serialize": true, "summarize": true, "sort": true,
		"order": true, "top": true, "join": true, "lookup": true,
		"union": true, "count": true, "mv-expand": true, "mvexpand": true,
		"mv-apply": true, "mvapply": true, "as": true,
	}
	for name := range queryOperators {
		if typed[name] {
			continue
		}
		script, errs := Parse("T | " + name)
		if len(errs) != 0 {
			t.Errorf("operator %s produced parse errors: %v", name, errs)
			continue
		}
		op := script.Statements[0].(*ExpressionStatement).Pipeline.Operators[0]
		canonical := map[string]string{"mvexpand": "mv-expand", "mvapply": "mv-apply"}[name]
		if canonical == "" {
			canonical = name
		}
		if op.Kind != canonical {
			t.Errorf("operator kind = %q, want %q", op.Kind, canonical)
		}
		if _, ok := op.Body.(RawSpec); !ok {
			t.Errorf("operator %s body = %T, want RawSpec", name, op.Body)
		}
	}
}
