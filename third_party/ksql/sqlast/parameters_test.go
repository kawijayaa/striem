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

func TestRankedValueParametersRenderOnce(t *testing.T) {
	for _, target := range []dialect.Dialect{dialect.SQLite(), dialect.PostgreSQL()} {
		value := &sqlast.RankedValue{Value: &sqlast.Parameter{Index: 2}, Rank: &sqlast.Identifier{Parts: []string{"r"}}}
		query := &sqlast.Select{Projections: []sqlast.SelectItem{{Expr: value}, {Expr: value}}}
		args := sqlast.NormalizeParameters(query, []any{"unused", "selected"})
		if fmt.Sprint(args) != `[selected selected]` {
			t.Fatalf("%s args=%v", target.Name(), args)
		}
		rendered, err := dialect.Render(target, query)
		if err != nil {
			t.Fatal(err)
		}
		want := `SELECT MAX(CASE WHEN "r" = 1 THEN ? END), MAX(CASE WHEN "r" = 1 THEN ? END)`
		if target.Name() == "postgresql" {
			want = `SELECT (ARRAY_AGG($1) FILTER (WHERE "r" = 1))[1], (ARRAY_AGG($2) FILTER (WHERE "r" = 1))[1]`
		}
		if rendered != want {
			t.Fatalf("%s SQL=%s want=%s", target.Name(), rendered, want)
		}
	}
}
