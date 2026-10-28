package kql

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func bitwiseOptions() []ksql.Option {
	var options []ksql.Option
	for _, name := range []string{"binary_and", "binary_or", "binary_xor", "binary_not", "binary_shift_left", "binary_shift_right", "bitset_count_ones"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			arity := 2
			if name == "binary_not" || name == "bitset_count_ones" {
				arity = 1
			}
			if len(args) != arity {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires %d integer arguments", name, arity)
			}
			values := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				if arg.Type != ksql.TypeInt && arg.Type != ksql.TypeLong && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("%s arguments must be int or long", name)
				}
				values[i] = arg.SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeLong, SQL: &sqlast.Call{Name: "kql_" + name, Args: values}}, nil
		}))
	}

	for _, name := range []string{"binary_all_and", "binary_all_or", "binary_all_xor"} {
		name := name
		options = append(options, ksql.WithTypedAggregate(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 1 integer argument", name)
			}
			if args[0].Type != ksql.TypeInt && args[0].Type != ksql.TypeLong && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires integer input", name)
			}
			return ksql.TypedExpression{Type: ksql.TypeLong, SQL: &sqlast.Call{Name: "kql_" + name, Args: []sqlast.Expr{args[0].SQL}}}, nil
		}))
	}
	options = append(options, ksql.WithTypedFunction("tohex", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) < 1 || len(args) > 2 {
			return ksql.TypedExpression{}, fmt.Errorf("tohex requires 1 or 2 integer arguments")
		}
		for _, arg := range args {
			if arg.Type != ksql.TypeInt && arg.Type != ksql.TypeLong && arg.Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("tohex requires integer arguments")
			}
		}
		var width sqlast.Expr = &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "1"}
		if len(args) == 2 {
			width = args[1].SQL
		}
		return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_tohex", Args: []sqlast.Expr{args[0].SQL, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: string(args[0].Type)}, width}}}, nil
	}))

	return options
}
