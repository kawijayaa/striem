package ksql_test

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/sqlast"
	"testing"
)

func TestTypedFunctionBinding(t *testing.T) {
	rule := func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 1 || args[0].Type != ksql.TypeLong {
			return ksql.TypedExpression{}, fmt.Errorf("requires long")
		}
		return ksql.TypedExpression{SQL: &sqlast.Binary{Left: args[0].SQL, Operator: ">", Right: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "0"}}, Type: ksql.TypeBool}, nil
	}
	compiler := ksql.New(dialect.SQLite(), ksql.WithTypedFunction("positive", rule), ksql.WithParameters())
	result := compiler.Compile(`print a=positive(1) | where a | project b=a`)
	if !result.OK() || len(result.Columns) != 1 || result.Columns[0].Type != ksql.TypeBool || len(result.Args) != 1 {
		t.Fatalf("lost type or parameters: %#v", result)
	}
	result = compiler.Compile(`print a=positive("1")`)
	if result.OK() || result.SQL != "" {
		t.Fatalf("type error lost: %#v", result)
	}
	untyped := ksql.WithFunction("positive", ksql.SQLFunction("abs"))
	for _, tc := range []struct {
		options []ksql.Option
		want    ksql.ScalarType
	}{
		{[]ksql.Option{untyped, ksql.WithTypedFunction("positive", rule)}, ksql.TypeBool},
		{[]ksql.Option{ksql.WithTypedFunction("positive", rule), untyped}, ksql.TypeUnknown},
	} {
		result = ksql.New(dialect.SQLite(), tc.options...).Compile(`print a=positive(1)`)
		if !result.OK() || result.Columns[0].Type != tc.want {
			t.Fatalf("last mapping did not win: %#v", result)
		}
	}
	result = ksql.New(dialect.SQLite(), ksql.WithTypedFunction("bad", func([]ksql.TypedExpression) (ksql.TypedExpression, error) { return ksql.TypedExpression{}, nil })).Compile(`print a=bad()`)
	if result.OK() || result.SQL != "" {
		t.Fatalf("nil result accepted: %#v", result)
	}
}
