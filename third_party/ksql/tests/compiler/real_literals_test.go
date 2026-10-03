package compiler_test

import (
	"fmt"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestWholeNumberRealLiteralsPreserveSQLArithmetic(t *testing.T) {
	db := openInvestigationDB(t)
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, query := range []string{`print result=100.0/3`, `print result=1e2/3`, `let n=100.0;print result=n/3`, `datatable(n:real)[100.0]|project result=n/3`} {
			result := compiler.Compile(query)
			if !result.OK() || len(result.Columns) != 1 || result.Columns[0].Type != ksql.TypeReal {
				t.Fatalf("%s: %#v", query, result)
			}
			rows := queryRows(t, db, result)
			if len(rows) != 1 || len(rows[0]) != 1 || rows[0][0] != 100.0/3.0 {
				t.Fatalf("mode=%d %s rows=%v", mode, query, rows)
			}
		}
		result := compiler.Compile(`print a=1.0,b=0.0,c=-0.0,d=1e2`)
		rows := queryRows(t, db, result)
		for _, value := range rows[0] {
			if _, ok := value.(float64); !ok {
				t.Fatalf("lost real storage type: %s %s", fmt.Sprint(rows), result.SQL)
			}
		}
	}
}
