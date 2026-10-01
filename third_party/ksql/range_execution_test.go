package ksql_test

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestSQLiteNumericRange(t *testing.T) {
	db, err := sql.Open(sqliteTestDriver, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, tc := range []struct{ query, want string }{
			{`range x from 1 to 8 step 3`, `[[1] [4] [7]]`},
			{`range x from 8 to 1 step -3`, `[[8] [5] [2]]`},
			{`range x from 2 to 1 step 1`, `[]`},
			{`range x from 1 to 2 step -1`, `[]`},
			{`range x from 1 to 1 step 1`, `[[1]]`},
			{`range x from 1 to 10 step 0`, `[]`},
			{`let start=2; let finish=6; let increment=2; range x from start to finish step increment`, `[[2] [4] [6]]`},
			{`range x from 9007199254740992 to 9007199254740994 step 1`, `[[9007199254740992] [9007199254740993] [9007199254740994]]`},
			{`range x from 9223372036854775806 to 9223372036854775807 step 1`, `[[9223372036854775806] [9223372036854775807]]`},
			{`range x from -9223372036854775807 to -9223372036854775808 step -1`, `[[-9223372036854775807] [-9223372036854775808]]`},
			{`range x from 1 to 2 step 0.5`, `[[1] [1.5] [2]]`},
			{`range x from 0.5 to 2.0 step 0.5`, `[[0.5] [1] [1.5] [2]]`},
			{`range x from 3.0 to 1.0 step -0.5`, `[[3] [2.5] [2] [1.5] [1]]`},
			{`range x from 1.0 to 2.0 step 1e-300 | count`, `[[1]]`},
			{`range x from 1 to 9223372036854775807 step 1 | take 3`, `[[1] [2] [3]]`},
			{`let R=range x from 1 to 3 step 1; R | where x>1 | count`, `[[2]]`},
			{`range x from 1 to 2 step 1 | union (range x from 3 to 4 step 1) | count`, `[[4]]`},
			{`range x from 1 to 3 step 1 | join kind=inner (range x from 2 to 4 step 1) on x | project x`, `[[2] [3]]`},
		} {
			t.Run(fmt.Sprintf("%d/%s", mode, tc.query), func(t *testing.T) {
				if got := fmt.Sprint(queryRows(t, db, compiler.Compile(tc.query))); got != tc.want {
					t.Fatalf("got %s want %s", got, tc.want)
				}
			})
		}
		result := compiler.Compile(`range x from 0.5 to 2.0 step 0.5`)
		if !reflect.DeepEqual(result.Columns, []ksql.Column{{Name: "x", Type: ksql.TypeReal}}) {
			t.Fatal(result.Columns)
		}
	}
}

func TestSQLiteRangeTypeDiagnostics(t *testing.T) {
	for _, query := range []string{`range x from "1" to 3 step 1`, `range x from datetime("2024-01-01") to datetime("2024-01-03") step 1d`, `range x from 1 to 3 step true`, `range x from Unknown to 3 step 1`} {
		result := ksql.New(dialect.SQLite()).Compile(query)
		if result.OK() || result.SQL != "" {
			t.Fatalf("accepted unsupported types: %#v", result)
		}
	}
}
