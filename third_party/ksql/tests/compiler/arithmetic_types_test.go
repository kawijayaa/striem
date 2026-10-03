package compiler_test

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"testing"
)

func TestArithmeticRejectsStringAndBooleanOperands(t *testing.T) {
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, op := range []string{"+", "-", "*", "/", "%"} {
			for _, operands := range [][2]string{{`"2"`, `1`}, {`1`, `"2"`}, {`true`, `1`}, {`1`, `false`}} {
				query := fmt.Sprintf("print %s %s %s", operands[0], op, operands[1])
				result := compiler.Compile(query)
				if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Code != "KQLB0408" {
					t.Fatalf("accepted %s: %#v", query, result)
				}
			}
		}
		result := compiler.Compile(`datatable(n:long,s:string)[1,"abc"]|summarize arg_max(s+"!",n)`)
		if result.OK() || result.SQL != "" {
			t.Fatalf("accepted invalid ordering expression: %#v", result)
		}
		result = compiler.Compile(`print n=2+3*4`)
		if got := fmt.Sprint(queryRows(t, openInvestigationDB(t), result)); got != `[[14]]` {
			t.Fatal(got)
		}
	}
}
