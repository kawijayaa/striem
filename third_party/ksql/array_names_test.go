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
	for _, name := range []string{"array_concat", "array_reverse", "array_slice", "array_rotate_left", "array_rotate_right", "array_shift_left", "array_shift_right"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			return ksql.TypedExpression{SQL: &sqlast.Call{Name: name, Args: []sqlast.Expr{args[0].SQL}}, Type: ksql.TypeDynamic}, nil
		}))
	}
	compiler := ksql.New(dialect.SQLite(), options...)
	result := compiler.Compile(`print array_concat(dynamic([])),array_reverse(dynamic([])),named=array_reverse(dynamic([])),array_slice(dynamic([]),0,1)`)
	want := []ksql.Column{{Name: "Column1", Type: ksql.TypeDynamic}, {Name: "Column2", Type: ksql.TypeDynamic}, {Name: "named", Type: ksql.TypeDynamic}, {Name: "Column4", Type: ksql.TypeDynamic}}
	if !result.OK() || !reflect.DeepEqual(result.Columns, want) {
		t.Fatalf("result=%#v want=%#v", result, want)
	}
}

func TestArrayMoveUnnamedResultColumns(t *testing.T) {
	options := []ksql.Option{}
	for _, name := range []string{"array_rotate_left", "array_rotate_right", "array_shift_left", "array_shift_right"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			return ksql.TypedExpression{SQL: &sqlast.Call{Name: name, Args: []sqlast.Expr{args[0].SQL, args[1].SQL}}, Type: ksql.TypeDynamic}, nil
		}))
	}
	result := ksql.New(dialect.SQLite(), options...).Compile(`print array_rotate_left(dynamic([]),1),array_rotate_right(dynamic([]),1),array_shift_left(dynamic([]),1),array_shift_right(dynamic([]),1)`)
	want := []ksql.Column{{Name: "Column1", Type: ksql.TypeDynamic}, {Name: "Column2", Type: ksql.TypeDynamic}, {Name: "Column3", Type: ksql.TypeDynamic}, {Name: "Column4", Type: ksql.TypeDynamic}}
	if !result.OK() || !reflect.DeepEqual(result.Columns, want) {
		t.Fatalf("result=%#v want=%#v", result, want)
	}
}

func TestZipUnnamedResultColumn(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithTypedFunction("zip", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		return ksql.TypedExpression{SQL: &sqlast.Call{Name: "zip", Args: []sqlast.Expr{args[0].SQL, args[1].SQL}}, Type: ksql.TypeDynamic}, nil
	}))
	result := compiler.Compile(`print zip(dynamic([1]),dynamic([2]))`)
	want := []ksql.Column{{Name: "Column1", Type: ksql.TypeDynamic}}
	if !result.OK() || !reflect.DeepEqual(result.Columns, want) {
		t.Fatalf("result=%#v want=%#v", result, want)
	}
}

func TestArraySplitUnnamedResultColumn(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithTypedFunction("array_split", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		return ksql.TypedExpression{SQL: &sqlast.Call{Name: "array_split", Args: []sqlast.Expr{args[0].SQL, args[1].SQL}}, Type: ksql.TypeDynamic}, nil
	}))
	result := compiler.Compile(`print array_split(dynamic([1]),0)`)
	want := []ksql.Column{{Name: "Column1", Type: ksql.TypeDynamic}}
	if !result.OK() || !reflect.DeepEqual(result.Columns, want) {
		t.Fatalf("result=%#v want=%#v", result, want)
	}
}

func TestArraySumUnnamedResultColumns(t *testing.T) {
	options := []ksql.Option{}
	for _, name := range []string{"array_sum", "series_sum"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			return ksql.TypedExpression{SQL: &sqlast.Call{Name: name, Args: []sqlast.Expr{args[0].SQL}}, Type: ksql.TypeReal}, nil
		}))
	}
	result := ksql.New(dialect.SQLite(), options...).Compile(`print array_sum(dynamic([1])),series_sum(dynamic([2]))`)
	want := []ksql.Column{{Name: "Column1", Type: ksql.TypeReal}, {Name: "Column2", Type: ksql.TypeReal}}
	if !result.OK() || !reflect.DeepEqual(result.Columns, want) {
		t.Fatalf("result=%#v want=%#v", result, want)
	}
}

func TestArrayIffUnnamedResultColumn(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithTypedFunction("array_iff", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		return ksql.TypedExpression{SQL: &sqlast.Call{Name: "array_iff", Args: []sqlast.Expr{args[0].SQL, args[1].SQL, args[2].SQL}}, Type: ksql.TypeDynamic}, nil
	}))
	result := compiler.Compile(`print array_iff(dynamic([true]),1,0)`)
	want := []ksql.Column{{Name: "Column1", Type: ksql.TypeDynamic}}
	if !result.OK() || !reflect.DeepEqual(result.Columns, want) {
		t.Fatalf("result=%#v want=%#v", result, want)
	}
}
