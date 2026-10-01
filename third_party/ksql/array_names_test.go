package ksql_test

import (
	"reflect"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/sqlast"
)

func TestArrayUnnamedResultColumns(t *testing.T) {
	options := []ksql.Option{}
	for _, name := range []string{"array_concat", "array_reverse"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			return ksql.TypedExpression{SQL: &sqlast.Call{Name: name, Args: []sqlast.Expr{args[0].SQL}}, Type: ksql.TypeDynamic}, nil
		}))
	}
	compiler := ksql.New(dialect.SQLite(), options...)
	result := compiler.Compile(`print array_concat(dynamic([])),array_reverse(dynamic([])),named=array_reverse(dynamic([]))`)
	want := []ksql.Column{{Name: "Column1", Type: ksql.TypeDynamic}, {Name: "Column2", Type: ksql.TypeDynamic}, {Name: "named", Type: ksql.TypeDynamic}}
	if !result.OK() || !reflect.DeepEqual(result.Columns, want) {
		t.Fatalf("result=%#v want=%#v", result, want)
	}
}
