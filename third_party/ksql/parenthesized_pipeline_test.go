package ksql_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestParenthesizedPipelineSources(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()), ksql.WithParameterization(mode))
		for _, tc := range []struct{ query, rows string }{
			{`(Events | where Host == "alpha") | project Host`, `[[alpha]]`},
			{`((Events | project Host) | where Host == "beta") | project Host`, `[[beta]]`},
			{`(((Events))) | project Host | order by Host asc`, `[[alpha] [beta]]`},
			{`((print x=(1+2),s="a|b")) | project x,s`, `[[3 a|b]]`},
			{`(datatable(x:long)[1,2,3] | where x>1) | summarize n=count()`, `[[2]]`},
			{`(range x from 1 to 3 step 1) | summarize n=count()`, `[[3]]`},
			{`let T=((Events | project Host) | where Host=="alpha"); (T) | project Host`, `[[alpha]]`},
			{`((print x=1) | union (print x=2)) | order by x asc`, `[[1] [2]]`},
			{`((print x=1) | join kind=inner ((print x=1,y=2)) on x) | project y`, `[[2]]`},
			{`(print d=dynamic({"a":[7]}),b=true) | project d.a[0],b`, `[[7 1]]`},
			{strings.Repeat("(", 2048) + `print x=7` + strings.Repeat(")", 2048) + ` | project x`, `[[7]]`},
		} {
			if got := fmt.Sprint(queryRows(t, db, compiler.Compile(tc.query))); got != tc.rows {
				t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.rows)
			}
		}
		result := compiler.Compile(`(print d=dynamic([1]),b=true) | project d,b`)
		if !result.OK() || len(result.Columns) != 2 || result.Columns[0].Type != ksql.TypeDynamic || result.Columns[1].Type != ksql.TypeBool {
			t.Fatalf("grouped source lost types: %#v", result)
		}
	}
	for _, query := range []string{
		`(Events | take 1`, `(Events | take 1))`, `() | take 1`,
		`(Events |) | take 1`, `(print x=(1]) | project x`,
		`(print x=1) junk | project x`, `(Events | take 1).x`,
		`(1+2) | project x`, `(dynamic({"x":1})).x | project x`,
	} {
		result := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog())).Compile(query)
		if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Span.Length == 0 {
			t.Fatalf("accepted invalid tabular expression %s: %#v", query, result)
		}
	}
}
