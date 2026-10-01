package ksql_test

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"reflect"
	"testing"
)

func TestPrintPositionalNames(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, tc := range []struct {
			query, rows string
			names       []string
		}{
			{`print a=1,2,tostring("x"),dynamic({"x":3}).x`, `[[1 2 x 3]]`, []string{"a", "print_1", "print_2", "print_3"}},
			{`let v=7; print v,v+1`, `[[7 8]]`, []string{"print_0", "print_1"}},
			{`print tostring("x"),explicit=5,tostring("y")`, `[[x 5 y]]`, []string{"print_0", "explicit", "print_2"}},
			{`print 1,2 | project print_1,print_0`, `[[2 1]]`, []string{"print_1", "print_0"}},
		} {
			result := compiler.Compile(tc.query)
			if !result.OK() {
				t.Fatal(result.Diagnostics)
			}
			var names []string
			for _, c := range result.Columns {
				names = append(names, c.Name)
			}
			if !reflect.DeepEqual(names, tc.names) {
				t.Fatalf("%s names=%v want=%v", tc.query, names, tc.names)
			}
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.rows {
				t.Fatalf("%s rows=%s want=%s", tc.query, got, tc.rows)
			}
		}
	}
}
