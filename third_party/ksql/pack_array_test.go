package ksql_test

import (
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/sqlast"
	"reflect"
	"testing"
)

func TestPackArrayWildcardArguments(t *testing.T) {
	var arguments []ksql.TypedExpression
	compiler := ksql.New(dialect.SQLite(), ksql.WithTypedFunction("pack_array", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		arguments = args
		values := make([]sqlast.Expr, len(args))
		for i, arg := range args {
			values[i] = arg.SQL
		}
		return ksql.TypedExpression{SQL: &sqlast.Call{Name: "json_array", Args: values}, Type: ksql.TypeDynamic}, nil
	}))
	result := compiler.Compile(`let a=99; datatable(a:long,b:string)[1,"x"] | project x=pack_array(*)`)
	if !result.OK() || len(arguments) != 2 {
		t.Fatal(result, arguments)
	}
	if !reflect.DeepEqual([]ksql.ScalarType{arguments[0].Type, arguments[1].Type}, []ksql.ScalarType{ksql.TypeLong, ksql.TypeString}) {
		t.Fatal(arguments)
	}
	if _, ok := arguments[0].SQL.(*sqlast.Identifier); !ok {
		t.Fatalf("wildcard column was shadowed by scalar let: %#v", arguments[0])
	}
	for _, query := range []string{`Unknown | project x=pack_array(*)`, `print x=pack_array(*)`} {
		result := compiler.Compile(query)
		if result.OK() || result.SQL != "" {
			t.Fatal(result)
		}
	}
}
