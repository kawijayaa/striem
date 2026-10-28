package kql

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func characterStringOptions() []ksql.Option {
	return []ksql.Option{
		ksql.WithTypedFunction("translate", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 3 {
				return ksql.TypedExpression{}, fmt.Errorf("translate requires 3 string arguments")
			}
			values := make([]sqlast.Expr, 3)
			for i, arg := range args {
				if arg.Type != ksql.TypeString && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("translate requires string arguments")
				}
				values[i] = arg.SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_translate", Args: values}}, nil
		}),
		ksql.WithTypedFunction("reverse", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 {
				return ksql.TypedExpression{}, fmt.Errorf("reverse requires 1 scalar argument")
			}
			switch args[0].Type {
			case ksql.TypeString, ksql.TypeInt, ksql.TypeLong, ksql.TypeReal, ksql.TypeBool, ksql.TypeDateTime, ksql.TypeTimespan, ksql.TypeUnknown:
			default:
				return ksql.TypedExpression{}, fmt.Errorf("reverse scalar type %s remains unsupported", args[0].Type)
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_reverse", Args: []sqlast.Expr{args[0].SQL, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: string(args[0].Type)}}}}, nil
		}),
	}
}
