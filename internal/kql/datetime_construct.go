package kql

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func datetimeConstructOptions() []ksql.Option {
	options := []ksql.Option{
		ksql.WithTypedFunction("make_datetime", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 3 || len(args) > 6 {
				return ksql.TypedExpression{}, fmt.Errorf("make_datetime requires 3 to 6 numeric components")
			}
			values := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				if arg.Type != ksql.TypeUnknown && arg.Type != ksql.TypeInt && arg.Type != ksql.TypeLong && !(i == 5 && arg.Type == ksql.TypeReal) {
					return ksql.TypedExpression{}, fmt.Errorf("make_datetime components must be integers except seconds, which may be real")
				}
				values[i] = arg.SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeDateTime, SQL: &sqlast.Call{Name: "kql_make_datetime", Args: values}}, nil
		}),
		ksql.WithTypedFunction("datetime_diff", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 3 {
				return ksql.TypedExpression{}, fmt.Errorf("datetime_diff requires period and two datetimes")
			}
			if args[0].Type != ksql.TypeString && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("datetime_diff period must be a string")
			}
			for _, arg := range args[1:] {
				if arg.Type != ksql.TypeDateTime && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("datetime_diff requires datetime operands")
				}
			}
			return ksql.TypedExpression{Type: ksql.TypeLong, SQL: &sqlast.Call{Name: "kql_datetime_diff", Args: []sqlast.Expr{args[0].SQL, args[1].SQL, args[2].SQL}}}, nil
		}),

		ksql.WithTypedFunction("datetime_add", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 3 {
				return ksql.TypedExpression{}, fmt.Errorf("datetime_add requires period, amount and datetime")
			}
			if args[0].Type != ksql.TypeString && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("datetime_add period must be a string")
			}
			if args[1].Type != ksql.TypeInt && args[1].Type != ksql.TypeLong && args[1].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("datetime_add amount must be an integer")
			}
			if args[2].Type != ksql.TypeDateTime && args[2].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("datetime_add requires datetime input")
			}
			return ksql.TypedExpression{Type: ksql.TypeDateTime, SQL: &sqlast.Call{Name: "kql_datetime_add", Args: []sqlast.Expr{args[0].SQL, args[1].SQL, args[2].SQL}}}, nil
		}),
	}
	for _, unit := range []string{"seconds", "milliseconds", "microseconds", "nanoseconds"} {
		name := "unixtime_" + unit + "_todatetime"
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 1 numeric argument", name)
			}
			switch args[0].Type {
			case ksql.TypeInt, ksql.TypeLong, ksql.TypeReal, ksql.TypeUnknown:
			default:
				return ksql.TypedExpression{}, fmt.Errorf("%s requires numeric input", name)
			}
			return ksql.TypedExpression{Type: ksql.TypeDateTime, SQL: &sqlast.Call{Name: "kql_" + name, Args: []sqlast.Expr{args[0].SQL}}}, nil
		}))
	}
	return options

}
