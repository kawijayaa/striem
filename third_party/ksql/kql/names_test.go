package kql

import (
	"strings"
	"testing"
)

func TestQuotedDeclarationsAndReferenceStructure(t *testing.T) {
	query := `datatable(['a' ' b']:long)[1] | project ['output name']=['a b']`
	script, errors := Parse(query)
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	pipeline := script.Statements[0].(*ExpressionStatement).Pipeline
	if pipeline.Source.Columns[0].Name != `"a b"` {
		t.Fatal("lost compound declaration")
	}
	alias := pipeline.Operators[0].Body.(ExpressionSpec).Expressions[0].(*NamedExpression)
	if alias.Name != `'output name'` || alias.Value.(*NameExpression).Name != `'a b'` || alias.Span.Offset != strings.Index(query, "['output name']") {
		t.Fatalf("lost name/span: %#v", alias)
	}
}
