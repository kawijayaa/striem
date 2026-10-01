package kql

import (
	"strings"
	"testing"
)

func TestParenthesizedPipelineOrderAndSpans(t *testing.T) {
	query := `((T | project x) | where x > (1+2)) | take 3`
	script, errs := Parse(query)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	pipeline := script.Statements[0].(*ExpressionStatement).Pipeline
	if pipeline.Span.Offset != 0 || pipeline.Span.Length != len(query) || pipeline.Source.Name != "T" || len(pipeline.Operators) != 3 {
		t.Fatalf("pipeline=%#v", pipeline)
	}
	for i, kind := range []string{"project", "where", "take"} {
		op := pipeline.Operators[i]
		if op.Kind != kind || !strings.HasPrefix(query[op.Span.Offset:op.Span.Offset+op.Span.Length], kind) {
			t.Fatalf("operator=%#v", op)
		}
	}
	// Each pair encloses another pipeline plus an operator. Flattening must
	// preserve all operators without descending recursively or rescanning groups.
	const depth = 2048
	script, errs = Parse(strings.Repeat("(", depth) + `T` + strings.Repeat(` | take 1)`, depth))
	if len(errs) != 0 || len(script.Statements[0].(*ExpressionStatement).Pipeline.Operators) != depth {
		t.Fatalf("deep pipeline parse errors=%v", errs)
	}
}
