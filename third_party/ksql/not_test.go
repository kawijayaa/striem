package ksql_test

import (
	"database/sql"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestNotFunction(t *testing.T) {
	compiler := ksql.New(dialect.SQLite())
	db, err := sql.Open(sqliteTestDriver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE Events(n INTEGER); INSERT INTO Events VALUES (1),(2),(NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, query := range []string{
		`Events | where not(n == 1) | project n`,
		`Events | where not(not(n == 2)) | project n`,
	} {
		result := compiler.Compile(query)
		if !result.OK() {
			t.Fatalf("%s: %v", query, result.Diagnostics)
		}
		var n int
		if err := db.QueryRow(result.SQL, result.Args...).Scan(&n); err != nil || n != 2 {
			t.Fatalf("n=%d err=%v SQL=%s", n, err, result.SQL)
		}
	}
	result := compiler.Compile(`Events | take 1 | project A=not(true), B=not(false), C=not(null)`)
	if !result.OK() {
		t.Fatal(result.Diagnostics)
	}
	var a, b, c any
	if err := db.QueryRow(result.SQL, result.Args...).Scan(&a, &b, &c); err != nil {
		t.Fatal(err)
	}
	if a != int64(0) || b != int64(1) || c != nil {
		t.Fatalf("not results: %v %v %v", a, b, c)
	}
	for _, column := range result.Columns {
		if column.Type != ksql.TypeBool {
			t.Fatalf("not result type: %v", column)
		}
	}
	for _, query := range []string{`Events | project not()`, `Events | project not(true,false)`, `Events | project not(123)`, `Events | project not("false")`} {
		if result := compiler.Compile(query); result.OK() {
			t.Fatalf("invalid not() accepted: %s", query)
		}
	}
}
