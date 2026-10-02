package kql

import "testing"

func TestFunctionDefinitionStructure(t *testing.T) {
	query := `let f=(x:long,options:dynamic=dynamic({"a":1})){let offset=1; let g=(){x}; g()+offset}; print f(3)`
	script, errors := Parse(query)
	if len(errors) != 0 || len(script.Statements) != 2 {
		t.Fatalf("script=%#v errors=%v", script, errors)
	}
	function := script.Statements[0].(*LetStatement).Function
	if function == nil || len(function.Parameters) != 2 || len(function.Lets) != 2 || function.Result == nil || function.Pipeline != nil {
		t.Fatalf("function=%#v", function)
	}
	if function.Parameters[1].Default == nil || function.Lets[1].Function == nil {
		t.Fatal("lost parameter default or nested declaration")
	}
	for _, query := range []string{`let f=(t:(x:long)){t | project x}; print 1`, `let f=(){print 1}; print 1`} {
		if _, errors := Parse(query); len(errors) != 0 {
			t.Fatalf("tabular syntax lost: %s %v", query, errors)
		}
	}
}

func TestInvalidFunctionDefinitionSyntax(t *testing.T) {
	for _, query := range []string{
		`let f=(x){x}; print 1`, `let f=(x:){x}; print 1`,
		`let f=(x:long,){x}; print 1`, `let f=(a.b:long){1}; print 1`,
		`let f=(x:long){}; print 1`, `let f=(){let x=1;}; print 1`,
		`let f=(){1;2}; print 1`, `let f=(){1} 2; print 1`,
	} {
		if _, errors := Parse(query); len(errors) == 0 || errors[0].Span.Line < 1 {
			t.Fatalf("%s: %v", query, errors)
		}
	}
}

func TestTabularFunctionStructure(t *testing.T) {
	query := `let f=(T: (['some column']:long, d:dynamic), U:(*), n:long=2){T|take n}; f((range x from 1 to 3 step 1),datatable(y:long)[1]) | invoke f(U=(print y=1))`
	script, errors := Parse(query)
	if len(errors) != 0 {
		t.Fatal(errors)
	}
	definition := script.Statements[0].(*LetStatement).Function
	if definition.Pipeline == nil || definition.Parameters[0].Type != "tabular" || len(definition.Parameters[0].Columns) != 2 || definition.Parameters[0].Columns[0].Name != "'some column'" || !definition.Parameters[1].Wildcard || definition.Parameters[2].Default == nil {
		t.Fatalf("lost schema/body structure: %#v", definition)
	}
	pipeline := script.Statements[1].(*ExpressionStatement).Pipeline
	call := pipeline.Source.Expr.(*CallExpression)
	for _, argument := range call.Args {
		if _, ok := argument.(*PipelineExpression); !ok {
			t.Fatalf("lost table argument: %T", argument)
		}
	}
	invoke := pipeline.Operators[0]
	if invoke.Kind != "invoke" || len(invoke.Body.(ExpressionSpec).Expressions) != 1 {
		t.Fatal("lost typed invoke body")
	}
}

func TestInvalidTabularFunctionSyntax(t *testing.T) {
	for _, query := range []string{
		`let f=(T:()){T}; print 1`, `let f=(T:(x)){T}; print 1`,
		`let f=(T:(a.b:long)){T}; print 1`, `let f=(T:(x:long,)){T}; print 1`,
		`print 1 | invoke`, `print 1 | invoke f(x=)`,
		`let f=(T:(*)){T}; f(datatable(x:long))`,
	} {
		if _, errors := Parse(query); len(errors) == 0 || errors[0].Span.Line < 1 {
			t.Fatalf("%s: %v", query, errors)
		}
	}
}
