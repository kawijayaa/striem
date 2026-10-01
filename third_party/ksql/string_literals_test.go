package ksql_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestStringLiteralExecution(t *testing.T) {
	db, err := sql.Open(sqliteTestDriver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, tc := range []struct{ query, want string }{
			{`print x='hello\nworld'`, "[[hello\nworld]]"},
			{`print x='it\'s "quoted"'`, `[[it's "quoted"]]`},
			{`print x="it\'s \"quoted\""`, `[[it's "quoted"]]`},
			{`print x='Hello\u00A0World'`, "[[Hello\u00a0World]]"},
			{`print x='\U0001F600\uD83D\uDE00'`, `[[😀😀]]`},
			{`print x="Hello" ', ' @"world!"`, `[[Hello, world!]]`},
			{"print x='Hello' //comment\n', '@'world!'", `[[Hello, world!]]`},
			{`print x=dynamic(['a' 'b'])[0]`, `[[ab]]`},
			{`print x='x\'; DROP TABLE events; --'`, `[[x'; DROP TABLE events; --]]`},
		} {
			t.Run(fmt.Sprintf("%d/%s", mode, tc.query), func(t *testing.T) {
				if got := fmt.Sprint(queryRows(t, db, compiler.Compile(tc.query))); got != tc.want {
					t.Fatalf("got %q want %q", got, tc.want)
				}
			})
		}
	}
}
