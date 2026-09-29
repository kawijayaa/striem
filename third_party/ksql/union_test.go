package ksql_test

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestSQLiteUnionAlignment(t *testing.T) {
	db, err := sql.Open(sqliteTestDriver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	compiler := ksql.New(dialect.SQLite())
	for _, tc := range []struct {
		query, rows string
		columns     []ksql.Column
	}{
		{`print a=1,b="left" | union (print b="right",a=2)`, `[[1 left] [2 right]]`, []ksql.Column{{Name: "a", Type: ksql.TypeLong}, {Name: "b", Type: ksql.TypeString}}},
		{`union (print a=1), (print b=2)`, `[[1 <nil>] [<nil> 2]]`, []ksql.Column{{Name: "a", Type: ksql.TypeLong}, {Name: "b", Type: ksql.TypeLong}}},
		{`print x=1 | union (print x="two"), (print x_long=3), (print x_long1=4)`, `[[1 <nil> <nil> <nil>] [<nil> two <nil> <nil>] [<nil> <nil> 3 <nil>] [<nil> <nil> <nil> 4]]`, []ksql.Column{{Name: "x_long2", Type: ksql.TypeLong}, {Name: "x_string", Type: ksql.TypeString}, {Name: "x_long", Type: ksql.TypeLong}, {Name: "x_long1", Type: ksql.TypeLong}}},
		{`print a=1,b="left" | union kind=inner (print b="right",c=2)`, `[[left] [right]]`, []ksql.Column{{Name: "b", Type: ksql.TypeString}}},
		{`print a=1,b=2 | union kind=inner (print b=3,a="different")`, `[[2] [3]]`, []ksql.Column{{Name: "b", Type: ksql.TypeLong}}},
		{`let U=union (print a=1), (print b=2); U | where a == null | project b`, `[[2]]`, []ksql.Column{{Name: "b", Type: ksql.TypeLong}}},
		{`print a=1 | union (print a=1), (print a=1) | count`, `[[3]]`, []ksql.Column{{Name: "Count", Type: ksql.TypeLong}}},
		{`print a=1 | where a<0 | union (print b=2)`, `[[<nil> 2]]`, []ksql.Column{{Name: "a", Type: ksql.TypeLong}, {Name: "b", Type: ksql.TypeLong}}},
		{`union (print a=1 | union (print b=2)), (print c=3)`, `[[1 <nil> <nil>] [<nil> 2 <nil>] [<nil> <nil> 3]]`, []ksql.Column{{Name: "a", Type: ksql.TypeLong}, {Name: "b", Type: ksql.TypeLong}, {Name: "c", Type: ksql.TypeLong}}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			result := compiler.Compile(tc.query)
			rows := queryRows(t, db, result)
			if got := fmt.Sprint(rows); got != tc.rows {
				t.Fatalf("rows=%s want=%s SQL=%s", got, tc.rows, result.SQL)
			}
			if !reflect.DeepEqual(result.Columns, tc.columns) {
				t.Fatalf("schema=%v want=%v", result.Columns, tc.columns)
			}
		})
	}
}

func TestUnionDiagnostics(t *testing.T) {
	for _, query := range []string{`union`, `print a=1 | union`, `union (print a=1),`, `union kind=bogus (print a=1)`, `union withsource=S (print a=1)`, `union isfuzzy=true (print a=1)`, `T | union U`, `print a=1 | union kind=inner (print b=2)`} {
		t.Run(query, func(t *testing.T) {
			result := ksql.New(dialect.SQLite()).Compile(query)
			if result.OK() || result.SQL != "" {
				t.Fatalf("expected diagnostic and no SQL: %#v", result)
			}
		})
	}
}

func TestUnionProjectionLimit(t *testing.T) {
	result := ksql.New(dialect.SQLite(), ksql.WithLimits(ksql.Limits{MaxProjectionItems: 1})).Compile(`print a=1 | union (print b=2)`)
	if result.OK() || result.SQL != "" {
		t.Fatalf("union bypassed projection limit: %#v", result)
	}
}

func TestUnionBoundParametersAndDialects(t *testing.T) {
	db, err := sql.Open(sqliteTestDriver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	query := `union (print a="left"), (print b="right") | where b == "right" | project b`
	for _, target := range []dialect.Dialect{dialect.SQLite(), dialect.PostgreSQL(), dialect.MySQL(), dialect.SQLServer()} {
		t.Run(target.Name(), func(t *testing.T) {
			result := ksql.New(target, ksql.WithParameters()).Compile(query)
			if !result.OK() {
				t.Fatal(result.Diagnostics)
			}
			if !reflect.DeepEqual(result.Args, []any{"left", "right", "right"}) {
				t.Fatalf("wrong parameter traversal: %v", result.Args)
			}
			if target.Name() == dialect.SQLite().Name() {
				if got := fmt.Sprint(queryRows(t, db, result)); got != "[[right]]" {
					t.Fatal(got)
				}
			}
		})
	}
}
