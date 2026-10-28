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
	options = append(options, ksql.WithTypedFunction("floor", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		// floor(value, roundTo) is documented as an alias of bin.
		if len(args) == 2 {
			return binFunction(args)
		}
		if len(args) != 1 || !numericType(args[0].Type) {
			return ksql.TypedExpression{}, fmt.Errorf("floor requires one number, or a value and bin size")
		}
		return ksql.TypedExpression{Type: args[0].Type, SQL: &sqlast.Call{Name: "floor", Args: []sqlast.Expr{args[0].SQL}}}, nil
	}))
	options = append(options, ksql.WithTypedFunction("rand", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		switch {
		case len(args) == 0:
			return ksql.TypedExpression{Type: ksql.TypeReal, SQL: &sqlast.Call{Name: "kql_rand"}}, nil
		case len(args) == 1 && (args[0].Type == ksql.TypeInt || args[0].Type == ksql.TypeLong || args[0].Type == ksql.TypeUnknown):
			return ksql.TypedExpression{Type: ksql.TypeLong, SQL: &sqlast.Call{Name: "kql_rand", Args: []sqlast.Expr{args[0].SQL}}}, nil
		}
		return ksql.TypedExpression{}, fmt.Errorf("rand takes no arguments or one integer range")
	}))
	options = append(options, ksql.WithTypedFunction("bin_at", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 3 {
			return ksql.TypedExpression{}, fmt.Errorf("bin_at requires a value, bin size and fixed point")
		}
		value, size, fixed := args[0], args[1], args[2]
		values := []sqlast.Expr{value.SQL, size.SQL, fixed.SQL}
		switch {
		case value.Type == ksql.TypeDateTime && size.Type == ksql.TypeTimespan && fixed.Type == ksql.TypeDateTime:
			return ksql.TypedExpression{Type: ksql.TypeDateTime, SQL: &sqlast.Call{Name: "kql_bin_at_datetime", Args: values}}, nil
		case value.Type == ksql.TypeTimespan && size.Type == ksql.TypeTimespan && fixed.Type == ksql.TypeTimespan:
			return ksql.TypedExpression{Type: ksql.TypeTimespan, SQL: &sqlast.Call{Name: "kql_bin_at", Args: values}}, nil
		case numericType(value.Type) && numericType(size.Type) && numericType(fixed.Type):
			kind := ksql.TypeLong
			if value.Type == ksql.TypeReal || size.Type == ksql.TypeReal || fixed.Type == ksql.TypeReal {
				kind = ksql.TypeReal
			}
			return ksql.TypedExpression{Type: kind, SQL: &sqlast.Call{Name: "kql_bin_at", Args: values}}, nil
		}
		return ksql.TypedExpression{}, fmt.Errorf("bin_at requires matching numeric, timespan or datetime values with a timespan size for temporal values")
	}))
	for _, name := range []string{"array_sort_asc", "array_sort_desc"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) == 0 || len(args) > 64 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 1 to 64 arrays and an optional nulls_last", name)
			}
			values := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				last := i == len(args)-1 && i > 0
				if arg.Type == ksql.TypeBool && last {
					values[i] = arg.SQL
					continue
				}
				if arg.Type != ksql.TypeDynamic && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("%s arguments must be dynamic arrays", name)
				}
				values[i] = arg.SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_" + name, Args: values}}, nil
		}))
	}
	options = append(options, ksql.WithTypedFunction("round", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) < 1 || len(args) > 2 || !numericType(args[0].Type) || (len(args) == 2 && args[1].Type != ksql.TypeInt && args[1].Type != ksql.TypeLong && args[1].Type != ksql.TypeUnknown) {
			return ksql.TypedExpression{}, fmt.Errorf("round requires a number and an optional integer precision")
		}
		precision := sqlast.Expr(&sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "0"})
		if len(args) == 2 {
			precision = args[1].SQL
		}
		result := args[0].Type
		if result == ksql.TypeInt {
			result = ksql.TypeLong
		}
		return ksql.TypedExpression{Type: result, SQL: &sqlast.Call{Name: "kql_round", Args: []sqlast.Expr{args[0].SQL, precision}}}, nil
	}))
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
