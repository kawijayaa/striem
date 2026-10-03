package compiler_test

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"testing"
)

func TestDynamicGroupingRequiresScalarTypes(t *testing.T) {
	compiler := ksql.New(dialect.SQLite())
	for _, query := range []string{
		`print d=dynamic({"a":1}) | summarize n=count() by d`,
		`print d=dynamic({"a":1}) | summarize n=count() by k=d.a`,
		`print d=dynamic({"a":1}) | distinct d`,
		`print d=dynamic({"a":1}) | distinct d.a`,
		`print a=1,d=dynamic([1]) | distinct *`,
		`print d=dynamic(null) | distinct d`,
		`print d=dynamic([1]) | sample-distinct 1 of d`,
	} {
		result := compiler.Compile(query)
		if result.OK() || result.SQL != "" {
			t.Fatalf("accepted %s: %#v", query, result)
		}
		found := false
		for _, d := range result.Diagnostics {
			if d.Code == "KQLB0303" && d.Span.Length > 0 {
				found = true
			}
		}
		if !found {
			t.Fatalf("no positioned dynamic diagnostic for %s: %#v", query, result.Diagnostics)
		}
	}
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, tc := range []struct{ query, rows string }{
			{`datatable(d:dynamic)[dynamic({"a":1}),dynamic({"a":1})] | distinct tostring(d)`, `[[{"a":1}]]`},
			{`datatable(d:dynamic)[dynamic({"a":"x"}),dynamic({"a":"x"}),dynamic({"a":"y"})] | summarize n=count() by k=tostring(d.a) | order by k asc`, `[[x 2] [y 1]]`},
			{`datatable(s:string)["[]","[]","{}"] | distinct * | order by s asc`, `[[[]] [{}]]`},
			{`datatable(s:string)["x","x","y"] | sample-distinct 3 of s | order by s asc`, `[[x] [y]]`},
		} {
			if got := fmt.Sprint(queryRows(t, db, compiler.Compile(tc.query))); got != tc.rows {
				t.Fatalf("%s: rows=%s want=%s", tc.query, got, tc.rows)
			}
		}
	}
	for _, query := range []string{`print x=1 | sample-distinct 1 of 1`, `print x=1 | sample-distinct 1 of tostring(x)`, `let v=99; print x=1 | sample-distinct 1 of v`} {
		result := compiler.Compile(query)
		if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Span.Length == 0 {
			t.Fatalf("accepted non-column: %#v", result)
		}
	}
}
