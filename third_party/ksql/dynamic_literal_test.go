package ksql_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestDynamicJSONLiterals(t *testing.T) {
	db, err := sql.Open(sqliteTestDriver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, tc := range []struct{ query, want string }{
			{`print x=dynamic({"a":{"b":17}}).a.b`, `[[17]]`},
			{`print x=(dynamic({"a":[{"b":23}]})).a[0].b + 1`, `[[24]]`},
			{`print x=dynamic({"a":{"b":17}}).a.missing`, `[[<nil>]]`},
			{`print x=dynamic({"where":{"a.b":31}}).['where'].["a.b"]`, `[[31]]`},
			{`let d=dynamic({"a.b":{"where":41}}); print x=d.['a.b'].['where']`, `[[41]]`},
			{`print x=dynamic({"é":43}).[@'é']`, `[[43]]`},
			{`print x=dynamic({"a.b":1}).['missing']`, `[[<nil>]]`},
			{`print x=dynamic([11,22,33])[(-1)],y=dynamic([11,22,33])[(-3)]`, `[[33 11]]`},
			{`datatable(i:bool)[true,false] | project x=dynamic([11,22,33])[i]`, `[[22] [11]]`},
			{`print x=dynamic([11,22,33])[true],y=dynamic([11,22,33])[false]`, `[[22 11]]`},
			{`print x=dynamic([11,22,33])[(-4)],y=dynamic([11,22,33])[3]`, `[[<nil> <nil>]]`},
			{`let i=1; print x=dynamic([11,22,33])[-i],y=dynamic([11,22,33])[(i+1)]`, `[[33 33]]`},
			{`datatable(i:long)[-1,0,2,3,-4] | project x=dynamic([11,22,33])[i]`, `[[33] [11] [33] [<nil>] [<nil>]]`},
			{`print x=dynamic([])[(-1)],y=dynamic([11])[-9223372036854775807-1],z=dynamic([11])[9223372036854775807]`, `[[<nil> <nil> <nil>]]`},
			{`print x=dynamic(['a','b'])`, `[[["a","b"]]]`},
			{`print x=dynamic([]),y=dynamic({})`, `[[[] {}]]`},
			{`print x=dynamic({'a':[1,-2,9223372036854775807,true,null]})["a"][2]`, `[[9223372036854775807]]`},
			{`print x=dynamic("a"),y=dynamic(false),z=dynamic(null)`, `[["a" false <nil>]]`},
			{`let x=dynamic([1,2]); print y=x[1]`, `[[2]]`},
			{`print x=dynamic(["a\u0000b","\"quote\""])`, `[[["a\u0000b","\"quote\""]]]`},
		} {
			t.Run(fmt.Sprintf("%d/%s", mode, tc.query), func(t *testing.T) {
				if got := fmt.Sprint(queryRows(t, db, compiler.Compile(tc.query))); got != tc.want {
					t.Fatalf("got %q want %q", got, tc.want)
				}
			})
		}
		result := compiler.Compile(`print x=dynamic([])`)
		if !result.OK() || len(result.Columns) != 1 || result.Columns[0].Type != ksql.TypeDynamic {
			t.Fatal(result)
		}
		for _, query := range []string{`print x=dynamic([1 2])`, `print x=dynamic([1,])`, `print x=dynamic()`, `print x=dynamic(foo)`, `print x=dynamic([datetime("2024-01-01")])`, `print x=dynamic([1)`, `print x=dynamic({"x":})`} {
			result := compiler.Compile(query)
			if result.OK() || result.SQL != "" {
				t.Fatalf("accepted invalid constant %s: %#v", query, result)
			}
		}
	}
}
