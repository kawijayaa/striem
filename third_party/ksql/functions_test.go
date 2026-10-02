package ksql_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestScalarQueryFunctions(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, tc := range []struct {
			query, want string
			kind        ksql.ScalarType
		}{
			{`let add=(a:long,b:long){a+b}; print result=add(2,3)`, `[[5]]`, ksql.TypeLong},
			{`let f=(){17}; print result=f()`, `[[17]]`, ksql.TypeLong},
			{`let f=(a:long,b:long=3,c:long=5){a+b*c}; print result=f(c=7,a=2,b=4)`, `[[30]]`, ksql.TypeLong},
			{`let f=(a:long,b:long=3){a+b}; print result=f(2)`, `[[5]]`, ksql.TypeLong},
			{`let f=(a:long,b:long=3){a+b}; print result=f(2,b=7)`, `[[9]]`, ksql.TypeLong},
			{`let base=11; let f=(x:long){let base=2; let offset=base+1; x+offset}; print result=f(4)+base`, `[[18]]`, ksql.TypeLong},
			{`let f=(x:long){x+1}; let g=(x:long){let f=(y:long){y*2}; f(x)}; print result=g(3)+f(3)`, `[[10]]`, ksql.TypeLong},
			{`let f=(x:long){let g=(){x+3}; g()}; print result=f(2)+f(5)`, `[[13]]`, ksql.TypeLong},
			{`let base=7; let f=(x:long){base+x}; let g=(){let base=100; f(3)}; print result=g()`, `[[10]]`, ksql.TypeLong},
			{`let f=(x:long){x+1}; datatable(x:long)[2,5] | project result=f(x)`, `[[3] [6]]`, ksql.TypeLong},
			{`let f=(x:bool){not(x)}; print result=f(false)`, `[[1]]`, ksql.TypeBool},
			{`let f=(x:real){x/2}; print result=f(3)`, `[[1.5]]`, ksql.TypeReal},
			{`let f=(x:dynamic){x.a}; print result=f(dynamic({"a":3}))`, `[[3]]`, ksql.TypeDynamic},
			{`let f=(x:string="hello"){x}; print result=f()`, `[[hello]]`, ksql.TypeString},
			{`let ['with space']=(['some value']:long){['some value']+1}; print result=['with space'](5)`, `[[6]]`, ksql.TypeLong},
			{`let f=(x:long=-3){x}; print result=f()`, `[[-3]]`, ksql.TypeLong},
		} {
			result := compiler.Compile(tc.query)
			if !result.OK() || len(result.Columns) != 1 || result.Columns[0].Type != tc.kind {
				t.Fatalf("%s: %#v", tc.query, result)
			}
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.want {
				t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
			}
		}
	}
}

func TestScalarQueryFunctionDiagnostics(t *testing.T) {
	compiler := ksql.New(dialect.SQLite())
	for _, query := range []string{
		`let f=(x:long){x}; print f()`,
		`let f=(x:long){x}; print f(1,2)`,
		`let f=(x:long){x}; print f("bad")`,
		`let f=(x:long){x}; print f(x=1,x=2)`,
		`let f=(x:long){x}; print f(other=1)`,
		`let f=(x:long,y:long){x+y}; print f(y=1,2)`,
		`let f=(x:long=1,y:long){x+y}; print 1`,
		`let f=(x:long=1+1){x}; print 1`,
		`let f=(x:long="bad"){x}; print 1`,
		`let f=(x:long,x:long){x}; print 1`,
		`let f=(x:long){let x=3; x}; print 1`,
		`let f=(){unknown}; print 1`,
		`let f=(x:long){f(x)}; print 1`,
		`let f=(x:long){x}; let f=3; print 1`,
		`let f=(){let hidden=3; hidden}; print hidden`,
		`let f=(){x}; datatable(x:long)[1] | project f()`,
		`let f=(x:decimal){x}; print 1`,
		`let f=(x:long){x}; print f(x=)`,
		`let f=(x:long){x}; print f(x=`,
		`let f=(x:long){}; print 1`,
		`let f=(x:long){let y=1;}; print 1`,
	} {
		result := compiler.Compile(query)
		if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 {
			t.Fatalf("accepted invalid function script %s: %#v", query, result)
		}
		if result.Diagnostics[0].Span.Line < 1 {
			t.Fatalf("missing diagnostic position: %#v", result)
		}
	}
}

func TestQueryFunctionExpansionLimits(t *testing.T) {
	compiler := ksql.New(dialect.SQLite())
	query := `let f=(x:long){x+x}; print result=` + strings.Repeat("f(", 40) + "1" + strings.Repeat(")", 40)
	result := compiler.Compile(query)
	if result.OK() || result.SQL != "" || !strings.Contains(fmt.Sprint(result.Diagnostics), "work limit") {
		t.Fatalf("unbounded function expansion: %#v", result.Diagnostics)
	}
	query = strings.Repeat("let f=(){", 65) + "1" + strings.Repeat(";1};", 64) + "1};print 1"
	result = compiler.Compile(query)
	if result.OK() || result.SQL != "" {
		t.Fatal("unbounded function declaration nesting")
	}
}

func FuzzQueryFunctions(f *testing.F) {
	for _, source := range []string{
		`let f=(x:long){x+1}; print f(2)`,
		`let f=(x:long){x}; print f(x=)`,
		`let f=(x:long){let g=(){x}; g()}; print f(2)`,
		`print (())()`, `print (()).x`,
		`let f=(x:long=1+1){x}; print 1`,
		`let f=(x:dynamic){x.["a"]}; print f(dynamic({"a":1}))`,
		`let f=(T:(x:long)){T}; f((print x=2))`,
		`let f=(T:(*)){T}; datatable(x:long)[1] | invoke f()`,
		`let f=(T:(*)){T}; f(datatable(x:long))`,
		`print 1 | invoke`,
	} {
		f.Add(source)
	}
	compiler := ksql.New(dialect.SQLite(), ksql.WithLimits(ksql.Limits{MaxSourceBytes: 4096}))
	f.Fuzz(func(t *testing.T, source string) {
		result := compiler.Compile(source)
		if !result.OK() && result.SQL != "" {
			t.Fatal("invalid function query emitted SQL")
		}
	})
}
