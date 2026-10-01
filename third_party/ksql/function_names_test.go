package ksql_test

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/sqlast"
	"testing"
)

func TestFirstArgumentFunctionNames(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		for _, name := range []string{"parse_json", "parsejson", "todynamic", "toobject"} {
			compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode), ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
				if args[0].Type == ksql.TypeDynamic {
					return args[0], nil
				}
				return ksql.TypedExpression{SQL: &sqlast.Call{Name: "json", Args: []sqlast.Expr{args[0].SQL}}, Type: ksql.TypeDynamic}, nil
			}))
			for _, tc := range []struct{ query, name, rows string }{
				{`print ` + name + `('{"a":1}')`, "print_0", `[[{"a":1}]]`},
				{`print raw='{"a":1}' | project ` + name + `(raw)`, "raw", `[[{"a":1}]]`},
				{`print d=dynamic({"a":{"b":17}}) | project ` + name + `(d.a)`, "d_a", `[[{"b":17}]]`},
				{`let raw='{"a":1}'; print root=1 | project ` + name + `(raw) | project raw.a`, "raw_a", `[[1]]`},
				{`print ` + name + `('{"a":1}').a`, "print_0", `[[1]]`},
				{`print raw='{"a":1}' | project named=` + name + `(raw)`, "named", `[[{"a":1}]]`},
			} {
				result := compiler.Compile(tc.query)
				if !result.OK() || len(result.Columns) != 1 || result.Columns[0].Name != tc.name {
					t.Fatalf("%s: %#v", tc.query, result)
				}
				if got := fmt.Sprint(queryRows(t, db, result)); got != tc.rows {
					t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.rows)
				}
			}
		}
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, tc := range []struct{ query, name, rows string }{
			{`let raw=99; print raw=17 | project tostring(raw) | project raw`, "raw", `[[17]]`},
			{`let d=dynamic({"a":99}); print d=dynamic({"a":17}) | project d.a`, "d_a", `[[17]]`},
			{`print tostring("hello")`, "print_0", `[[hello]]`},
			{`print raw=17 | project tostring(raw) | project raw`, "raw", `[[17]]`},
			{`print d=dynamic({"a":19}) | project tostring(d.a) | project d_a`, "d_a", `[[19]]`},
			{`let raw=23; print tostring(tostring(raw))`, "print_0", `[[23]]`},
		} {
			result := compiler.Compile(tc.query)
			if !result.OK() || len(result.Columns) != 1 || result.Columns[0].Name != tc.name {
				t.Fatal(result)
			}
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.rows {
				t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.rows)
			}
		}
	}
}
