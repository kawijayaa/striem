package ksql_test

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestSQLiteProjectionPatterns(t *testing.T) {
	db, err := sql.Open(sqliteTestDriver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	compiler := ksql.New(dialect.SQLite(), ksql.WithParameters())
	source := `print b=0,a20=20,a3=3,a100=100,a1=1,tail=9`
	for _, tc := range []struct {
		op    string
		names []string
		rows  string
	}{
		{`project-keep a*`, []string{"a20", "a3", "a100", "a1"}, `[[20 3 100 1]]`},
		{`project-keep tail,a*`, []string{"a20", "a3", "a100", "a1", "tail"}, `[[20 3 100 1 9]]`},
		{`project-away a*`, []string{"b", "tail"}, `[[0 9]]`},
		{`project-away *0`, []string{"b", "a3", "a1", "tail"}, `[[0 3 1 9]]`},
		{`project-away *1*`, []string{"b", "a20", "a3", "tail"}, `[[0 20 3 9]]`},
		{`project-reorder a*`, []string{"a20", "a3", "a100", "a1", "b", "tail"}, `[[20 3 100 1 0 9]]`},
		{`project-reorder a* asc`, []string{"a1", "a100", "a20", "a3", "b", "tail"}, `[[1 100 20 3 0 9]]`},
		{`project-reorder a* desc`, []string{"a3", "a20", "a100", "a1", "b", "tail"}, `[[3 20 100 1 0 9]]`},
		{`project-reorder a* granny-asc`, []string{"a1", "a3", "a20", "a100", "b", "tail"}, `[[1 3 20 100 0 9]]`},
		{`project-reorder a* granny-desc`, []string{"a100", "a20", "a3", "a1", "b", "tail"}, `[[100 20 3 1 0 9]]`},
		{`project-reorder a20,a*,a20,* desc`, []string{"a20", "a3", "a100", "a1", "tail", "b"}, `[[20 3 100 1 9 0]]`},
		{`project-reorder absent*,tail`, []string{"tail", "b", "a20", "a3", "a100", "a1"}, `[[9 0 20 3 100 1]]`},
		{`project-keep a*,a20,a*`, []string{"a20", "a3", "a100", "a1"}, `[[20 3 100 1]]`},
		{`project-away A*`, []string{"b", "a20", "a3", "a100", "a1", "tail"}, `[[0 20 3 100 1 9]]`},
		{`project-reorder`, []string{"b", "a20", "a3", "a100", "a1", "tail"}, `[[0 20 3 100 1 9]]`},
	} {
		t.Run(tc.op, func(t *testing.T) {
			result := compiler.Compile(source + " | " + tc.op)
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.rows {
				t.Fatalf("rows=%s want=%s", got, tc.rows)
			}
			var names []string
			for _, column := range result.Columns {
				names = append(names, column.Name)
			}
			if !reflect.DeepEqual(names, tc.names) {
				t.Fatalf("columns=%v want=%v", names, tc.names)
			}
		})
	}
}

func TestProjectionPatternDiagnostics(t *testing.T) {
	for _, query := range []string{`print a=1 | project-keep a * a`, `print a=1 | project-away a* desc`, `print a=1 | project-reorder a* sideways`, `print a=1 | project-keep a*,`, `print a=1 | project-reorder missing`, `print a=1 | project-away *`, `T | project-keep a*`} {
		t.Run(query, func(t *testing.T) {
			result := ksql.New(dialect.SQLite()).Compile(query)
			if result.OK() || result.SQL != "" {
				t.Fatalf("expected positioned error, got %#v", result)
			}
		})
	}
	catalog := testCatalog{"T": {Name: "t", Schema: ksql.Schema{Columns: []ksql.Column{{Name: "a", Type: ksql.TypeLong}, {Name: "b", Type: ksql.TypeLong}}}}}
	result := ksql.New(dialect.SQLite(), ksql.WithCatalog(catalog), ksql.WithLimits(ksql.Limits{MaxProjectionItems: 1})).Compile(`T | project-keep *`)
	if result.OK() {
		t.Fatal("wildcard bypassed projection limit")
	}
}

func TestQuotedSelectionAndLargeNumericNames(t *testing.T) {
	db, err := sql.Open(sqliteTestDriver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE t ("a*b" INTEGER,"a.b" INTEGER,"αname" INTEGER,"a9999999999999999999999" INTEGER,"a10000000000000000000000" INTEGER); INSERT INTO t VALUES (1,2,3,4,5)`)
	if err != nil {
		t.Fatal(err)
	}
	var columns []ksql.Column
	for _, name := range []string{"a*b", "a.b", "αname", "a9999999999999999999999", "a10000000000000000000000"} {
		columns = append(columns, ksql.Column{Name: name, Type: ksql.TypeLong})
	}
	compiler := ksql.New(dialect.SQLite(), ksql.WithCatalog(testCatalog{"T": {Name: "t", Schema: ksql.Schema{Columns: columns}}}))
	for _, tc := range []struct{ query, want string }{
		{`T | project-keep ['a*b']`, `[[1]]`},
		{`T | project-keep ['a.b']`, `[[2]]`},
		{`T | project-keep α*`, `[[3]]`},
		{`T | project-reorder a* granny-asc`, `[[1 2 4 5 3]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(queryRows(t, db, compiler.Compile(tc.query))); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}
