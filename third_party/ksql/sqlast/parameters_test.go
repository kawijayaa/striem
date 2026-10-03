package sqlast_test

import (
	"fmt"
	"testing"

	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/sqlast"
)

func TestNormalizeParametersSharedParents(t *testing.T) {
	for _, useWith := range []bool{false, true} {
		shared := &sqlast.Binary{Left: &sqlast.Parameter{Index: 1}, Operator: "+", Right: &sqlast.Parameter{Index: 2}}
		var query sqlast.Query = &sqlast.Select{Projections: []sqlast.SelectItem{{Expr: shared}, {Expr: shared}}}
		wantSQL := `SELECT ($1 + $2), ($3 + $4)`
		if useWith {
			query = &sqlast.With{Bindings: []sqlast.Binding{{Name: "v", Query: &sqlast.Select{Projections: []sqlast.SelectItem{{Expr: shared, Alias: "value"}}}}}, Query: &sqlast.Select{Projections: []sqlast.SelectItem{{Expr: shared}}}}
			wantSQL = `WITH "v" AS MATERIALIZED (SELECT ($1 + $2) AS "value") SELECT ($3 + $4)`
		}
		if got := fmt.Sprint(sqlast.NormalizeParameters(query, []any{10, 20})); got != `[10 20 10 20]` {
			t.Fatal(got)
		}
		got, err := dialect.Render(dialect.PostgreSQL(), query)
		if err != nil || got != wantSQL {
			t.Fatalf("got=%s err=%v want=%s", got, err, wantSQL)
		}
	}
}
