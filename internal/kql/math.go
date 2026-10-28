package kql

import (
	"fmt"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func numericType(t ksql.ScalarType) bool {
	return t == ksql.TypeInt || t == ksql.TypeLong || t == ksql.TypeReal || t == ksql.TypeUnknown
}

func mathOptions() []ksql.Option {
	options := []ksql.Option{ksql.WithTypedFunction("pi", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 0 {
			return ksql.TypedExpression{}, fmt.Errorf("pi takes no arguments")
		}
		return ksql.TypedExpression{Type: ksql.TypeReal, SQL: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "3.141592653589793"}}, nil
	})}
	for _, name := range []string{"degrees", "radians", "cot", "exp2", "exp10", "log2", "gamma", "loggamma", "erf", "erfc"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 || !numericType(args[0].Type) {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires one numeric argument", name)
			}
			return ksql.TypedExpression{Type: ksql.TypeReal, SQL: &sqlast.Call{Name: "kql_math", Args: []sqlast.Expr{&sqlast.Literal{Kind: sqlast.StringLiteral, Value: name}, args[0].SQL}}}, nil
		}))
	}
	for _, name := range []string{"isfinite", "isinf", "isnan"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 || !numericType(args[0].Type) {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires one numeric argument", name)
			}
			return ksql.TypedExpression{Type: ksql.TypeBool, SQL: &sqlast.Call{Name: "kql_number_class", Args: []sqlast.Expr{&sqlast.Literal{Kind: sqlast.StringLiteral, Value: name}, args[0].SQL}}}, nil
		}))
	}
	for _, name := range []string{"max_of", "min_of"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 2 || len(args) > 64 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 2 to 64 arguments", name)
			}
			// All arguments share one type; int and long widen to long, and
			// integers combine with real as real.
			result := ksql.TypeUnknown
			values := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				values[i] = arg.SQL
				switch {
				case arg.Type == ksql.TypeUnknown || arg.Type == result:
				case result == ksql.TypeUnknown:
					result = arg.Type
				case numericType(arg.Type) && numericType(result):
					if arg.Type == ksql.TypeReal || result == ksql.TypeReal {
						result = ksql.TypeReal
					} else {
						result = ksql.TypeLong
					}
				default:
					return ksql.TypedExpression{}, fmt.Errorf("%s arguments must all have the same type", name)
				}
			}
			if result == ksql.TypeDynamic {
				return ksql.TypedExpression{}, fmt.Errorf("%s does not compare dynamic values", name)
			}
			return ksql.TypedExpression{Type: result, SQL: &sqlast.Call{Name: "kql_" + name, Args: values}}, nil
		}))
	}
	options = append(options, ksql.WithTypedFunction("strcmp", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 2 || (args[0].Type != ksql.TypeString && args[0].Type != ksql.TypeUnknown) || (args[1].Type != ksql.TypeString && args[1].Type != ksql.TypeUnknown) {
			return ksql.TypedExpression{}, fmt.Errorf("strcmp requires two string arguments")
		}
		return ksql.TypedExpression{Type: ksql.TypeInt, SQL: &sqlast.Call{Name: "kql_strcmp", Args: []sqlast.Expr{args[0].SQL, args[1].SQL}}}, nil
	}))
	for _, name := range []string{"isascii", "isutf8"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 || (args[0].Type != ksql.TypeString && args[0].Type != ksql.TypeUnknown) {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires one string argument", name)
			}
			return ksql.TypedExpression{Type: ksql.TypeBool, SQL: &sqlast.Call{Name: "kql_" + name, Args: []sqlast.Expr{args[0].SQL}}}, nil
		}))
	}
	return options
}
