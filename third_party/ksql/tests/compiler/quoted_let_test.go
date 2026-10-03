package compiler_test

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"testing"
)

func TestQuotedLetNames(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(investigationCatalog()), ksql.WithParameterization(mode))
		for _, tc := range []struct{ query, rows string }{
			{`let ['some number']=20; range y from 0 to ['some number'] step 5`, `[[0] [5] [10] [15] [20]]`},
			{`let ['a.b']=dynamic({"x":5}); print v=['a.b'].x`, `[[5]]`},
			{`let ['a.b']=dynamic({"x":5}); print v=['a.b']['x']`, `[[5]]`},
			{`let ['é\u002e名']=7; print v=['é.名']`, `[[7]]`},
			{`let [@'a''b']=11; print v=["a'b"]`, `[[11]]`},
			{`let ['where']=((Events | take 1)); ['where'] | project Host`, `[[alpha]]`},
			{`let ['T U']=((datatable(x:long)[1,2])); ['T U'] | union ['T U'] | summarize n=count()`, `[[4]]`},
			{`let ["quoted name"]=(print d=dynamic([1]),b=true); ["quoted name"] | project d,b`, `[[[1] 1]]`},
		} {
			if got := fmt.Sprint(queryRows(t, db, compiler.Compile(tc.query))); got != tc.rows {
				t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.rows)
			}
		}
	}
	for _, query := range []string{`let ['x']=; print y=1`, `let ['x'] 1; print y=1`, `let [x]=1; print y=1`, `let ['x'=1; print y=1`} {
		result := ksql.New(dialect.SQLite()).Compile(query)
		if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Span.Length == 0 {
			t.Fatalf("accepted malformed declaration: %#v", result)
		}
	}
}
