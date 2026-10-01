package ksql_test

import (
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/sqlast"
	"reflect"
	"testing"
)

func TestIndexOfUnnamedResultColumns(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithTypedFunction("indexof", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		return ksql.TypedExpression{SQL: &sqlast.Call{Name: "instr", Args: []sqlast.Expr{args[0].SQL, args[1].SQL}}, Type: ksql.TypeLong}, nil
	}))
	result := compiler.Compile(`print indexof("abc","a"),indexof("abc","b"),named=indexof("abc","c")`)
	want := []ksql.Column{{Name: "print_0", Type: ksql.TypeLong}, {Name: "print_1", Type: ksql.TypeLong}, {Name: "named", Type: ksql.TypeLong}}
	if !result.OK() || !reflect.DeepEqual(result.Columns, want) {
		t.Fatalf("result=%#v want=%#v", result, want)
	}
}
