package compiler_test

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"reflect"
	"testing"
)

func TestDynamicAccessorNames(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()), ksql.WithParameterization(mode))
		for _, tc := range []struct {
			query, rows string
			names       []string
		}{
			{`Events | take 1 | join kind=inner (Events | take 1) on Host | project RawData.process.command_line`, `[[PowerShell -enc x]]`, []string{"RawData_process_command_line"}},
			{`Events | take 1 | project RawData.process.command_line,RawData["a.b"],RawData.items[0]`, `[[PowerShell -enc x 11 1]]`, []string{"RawData_process_command_line", "RawData_a.b", "RawData_items_0"}},
			{`print d=dynamic({"where":{"a.b":31}}) | project d.['where'].["a.b"] | project ['d_where_a.b']`, `[[31]]`, []string{"d_where_a.b"}},
			{`print d=dynamic([11,22]),i=1 | project d[i]`, `[[22]]`, []string{"d_"}},
			{`print d=dynamic([11,22]) | project d[(-1)]`, `[[22]]`, []string{"d_"}},
			{`let original=dynamic({"a":19}); let alias=original; print root=1 | project alias["a"]`, `[[19]]`, []string{"alias_a"}},
			{`print dynamic({"a":23}).a,dynamic([29])[0]`, `[[23 29]]`, []string{"print_0", "print_1"}},
			{`print d=dynamic({"a":31}) | extend d.a | project d_a`, `[[31]]`, []string{"d_a"}},
		} {
			result := compiler.Compile(tc.query)
			if !result.OK() {
				t.Fatalf("%s: %#v", tc.query, result.Diagnostics)
			}
			var names []string
			for _, c := range result.Columns {
				names = append(names, c.Name)
			}
			if !reflect.DeepEqual(names, tc.names) {
				t.Fatalf("%s names=%v want=%v", tc.query, names, tc.names)
			}
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.rows {
				t.Fatalf("%s rows=%s want=%s", tc.query+" SQL="+result.SQL, got, tc.rows)
			}
		}
		for _, query := range []string{`let d=dynamic({"a":{"b":17}}); print root=1 | project d.a.b`, `let d=dynamic({"a":{"b":17}}); print root=1 | project d["a"]["b"]`} {
			result := compiler.Compile(query)
			if !result.OK() || len(result.Columns) != 1 || result.Columns[0] != (ksql.Column{Name: "d_a_b", Type: ksql.TypeDynamic}) {
				t.Fatal(result)
			}
			if got := fmt.Sprint(queryRows(t, db, result)); got != "[[17]]" {
				t.Fatal(got)
			}
		}
	}
}
