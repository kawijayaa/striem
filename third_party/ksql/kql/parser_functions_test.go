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
