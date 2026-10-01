package ksql_test

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"strings"
	"testing"
)

func TestParenthesizedTabularLets(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()), ksql.WithParameterization(mode))
		for _, tc := range []struct{ query, rows string }{
			{`let T=(print x=1); T | project x`, `[[1]]`},
			{`let T=((Events | where Host=="alpha")); T | project Host`, `[[alpha]]`},
			{`let T=((datatable(x:long)[1,2])); T | summarize n=count()`, `[[2]]`},
			{`let T=((range x from 1 to 3 step 1)); T | summarize n=count()`, `[[3]]`},
			{`let T=((print x=1)); T | join kind=inner ((print x=1,y=2)) on x | project y`, `[[2]]`},
			{`let T=((print x=1)); T | union ((print x=2)) | order by x asc`, `[[1] [2]]`},
			{`let n=((1)+(2)); print x=n`, `[[3]]`},
			{`let d=((dynamic({"p":"|"}))); print x=d.p`, `[[|]]`},
			{`let T=((print d=dynamic({"a":5}))); T | project d.a`, `[[5]]`},
			{`let T=` + strings.Repeat("(", 2048) + `print x=7` + strings.Repeat(")", 2048) + `; T`, `[[7]]`},
		} {
			result := compiler.Compile(tc.query)
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.rows {
				t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.rows)
			}
		}
	}
	for _, query := range []string{`let T=(); T`, `let T=((print x=1); T`, `let T=(print x=1)); T`, `let n=(1)+(2)); print x=n`} {
		result := ksql.New(dialect.SQLite()).Compile(query)
		if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 {
			t.Fatalf("accepted %s: %#v", query, result)
		}
	}
}
