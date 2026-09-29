package kql

import "testing"

func TestParseRelationalPipeline(t *testing.T) {
	script, errs := Parse(`T
| where s has_cs "Error" and a between (1 .. 10)
| extend square = a * a
| summarize total = sum(square) by bin(ts, 1h)
| order by total desc nulls last
| take 5`)
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	query := script.Statements[0].(*ExpressionStatement).Pipeline
	if query.Source.Kind != "table" || query.Source.Name != "T" {
		t.Fatalf("source = %#v", query.Source)
	}
	want := []string{"where", "extend", "summarize", "sort", "take"}
	for i, kind := range want {
		if query.Operators[i].Kind != kind {
			t.Errorf("operator %d = %q, want %q", i, query.Operators[i].Kind, kind)
		}
	}
}

func TestParseStatementsAndAliases(t *testing.T) {
	script, errs := Parse(`let cutoff = 10; T | filter a > cutoff | limit 2;`)
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	if len(script.Statements) != 2 {
		t.Fatalf("statement count = %d", len(script.Statements))
	}
	let := script.Statements[0].(*LetStatement)
	if let.Name != "cutoff" || let.Value == nil {
		t.Fatalf("let = %#v", let)
	}
	query := script.Statements[1].(*ExpressionStatement).Pipeline
	if query.Operators[0].Kind != "where" || query.Operators[1].Kind != "take" {
		t.Fatalf("operators = %#v", query.Operators)
	}
}

func TestParseRecognizesAdvancedOperator(t *testing.T) {
	script, errs := Parse(`T | make-series n=count() on ts step 1h`)
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	op := script.Statements[0].(*ExpressionStatement).Pipeline.Operators[0]
	if op.Kind != "make-series" {
		t.Fatalf("operator = %q", op.Kind)
	}
	if _, ok := op.Body.(RawSpec); !ok {
		t.Fatalf("body = %T, want RawSpec", op.Body)
	}
}

func TestParseJoinAndUnion(t *testing.T) {
	script, errs := Parse(`T | join kind=inner (U | where active == true) on key | union V, (W | take 1)`)
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	operators := script.Statements[0].(*ExpressionStatement).Pipeline.Operators
	join := operators[0].Body.(JoinSpec)
	if join.Kind != "inner" || join.Right.Operators[0].Kind != "where" || len(join.Conditions) != 1 {
		t.Fatalf("join = %#v", join)
	}
	union := operators[1].Body.(UnionSpec)
	if len(union.Inputs) != 2 || len(union.Inputs[1].Operators) != 1 {
		t.Fatalf("union = %#v", union)
	}
}

func TestParseDatatableAndRange(t *testing.T) {
	script, errs := Parse(`datatable(id:long, name:string)[1, "one", 2, "two"] | union (range id from 3 to 5 step 1)`)
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	query := script.Statements[0].(*ExpressionStatement).Pipeline
	if len(query.Source.Columns) != 2 || len(query.Source.Rows) != 2 {
		t.Fatalf("datatable source = %#v", query.Source)
	}
	union := query.Operators[0].Body.(UnionSpec)
	rangeSource := union.Inputs[0].Source
	if rangeSource.Kind != "range" || rangeSource.Name != "id" {
		t.Fatalf("range source = %#v", rangeSource)
	}
}

func TestParseReportsUnknownOperator(t *testing.T) {
	_, errs := Parse(`T | frobnicate a`)
	if len(errs) != 1 || errs[0].Code != "KQLP0301" {
		t.Fatalf("Parse() errors = %#v", errs)
	}
}

func TestParseMultiValueOperators(t *testing.T) {
	script, errs := Parse(`T
| mv-expand with_itemindex=i item=items to typeof(long) limit 2
| mv-apply value=values on (where value > 1 | extend doubled=value*2)`)
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	operators := script.Statements[0].(*ExpressionStatement).Pipeline.Operators
	expand := operators[0].Body.(MvExpandSpec)
	if expand.ItemIndex != "i" || len(expand.Items) != 1 || expand.Items[0].Name != "item" || expand.Items[0].Type != "long" || expand.Limit == nil {
		t.Fatalf("mv-expand = %#v", expand)
	}
	apply := operators[1].Body.(MvApplySpec)
	if len(apply.Items) != 1 || apply.Items[0].Name != "value" || len(apply.Operators) != 2 {
		t.Fatalf("mv-apply = %#v", apply)
	}
	if apply.Operators[0].Kind != "where" || apply.Operators[1].Kind != "extend" {
		t.Fatalf("mv-apply operators = %#v", apply.Operators)
	}
}

func TestParseSampleDistinctAndAs(t *testing.T) {
	script, errs := Parse(`T | sample-distinct 3 of state | as hint.materialized=false Sampled`)
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	operators := script.Statements[0].(*ExpressionStatement).Pipeline.Operators
	if _, ok := operators[0].Body.(SampleDistinctSpec); !ok {
		t.Fatalf("sample-distinct body = %T", operators[0].Body)
	}
	as := operators[1].Body.(AsSpec)
	if as.Name != "Sampled" || as.Materialized == nil || *as.Materialized {
		t.Fatalf("as = %#v", as)
	}
}

func TestParseLegacyMvExpandLimit(t *testing.T) {
	script, errs := Parse(`T | mvexpand item=items`)
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	spec := script.Statements[0].(*ExpressionStatement).Pipeline.Operators[0].Body.(MvExpandSpec)
	limit, ok := spec.Limit.(*LiteralExpression)
	if !spec.Legacy || !ok || limit.Text != "128" {
		t.Fatalf("mvexpand = %#v", spec)
	}
}

func TestParseTypedSearchWithSpan(t *testing.T) {
	script, errs := Parse(`Events | search "powershell"`)
	if len(errs) != 0 {
		t.Fatalf("Parse() errors = %v", errs)
	}
	operator := script.Statements[0].(*ExpressionStatement).Pipeline.Operators[0]
	spec, ok := operator.Body.(SearchSpec)
	if !ok {
		t.Fatalf("search body = %T", operator.Body)
	}
	if spec.Term.NodeSpan().Length == 0 || operator.Span.Length == 0 {
		t.Fatalf("search spans = term %v operator %v", spec.Term.NodeSpan(), operator.Span)
	}
}
