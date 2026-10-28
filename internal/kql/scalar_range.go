package kql

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func scalarRangeOption() ksql.Option {
	return ksql.WithTypedFunction("range", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) < 2 || len(args) > 3 {
			return ksql.TypedExpression{}, fmt.Errorf("range requires 2 or 3 numeric arguments")
		}
		// range(datetime, datetime, timespan) and range(timespan, timespan,
		// timespan) need an explicit step.
		if (args[0].Type == ksql.TypeDateTime || args[0].Type == ksql.TypeTimespan) && len(args) == 3 {
			kind := args[0].Type
			if args[1].Type != kind && args[1].Type != ksql.TypeUnknown || args[2].Type != ksql.TypeTimespan && args[2].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("a %s range requires %s bounds and a timespan step", kind, kind)
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_temporal_range", Args: []sqlast.Expr{
				&sqlast.Literal{Kind: sqlast.StringLiteral, Value: string(kind)}, args[0].SQL, args[1].SQL, args[2].SQL}}}, nil
		}
		values := make([]sqlast.Expr, 0, 3)
		for _, arg := range args {
			switch arg.Type {
			case ksql.TypeInt, ksql.TypeLong, ksql.TypeReal, ksql.TypeUnknown:
			default:
				return ksql.TypedExpression{}, fmt.Errorf("range requires numeric bounds, or datetime/timespan bounds with a timespan step")
			}
			values = append(values, arg.SQL)
		}
		if len(values) == 2 {
			values = append(values, &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "1"})
		}
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_scalar_range", Args: values}}, nil
	})
}
