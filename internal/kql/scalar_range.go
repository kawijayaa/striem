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
		values := make([]sqlast.Expr, 0, 3)
		for _, arg := range args {
			switch arg.Type {
			case ksql.TypeInt, ksql.TypeLong, ksql.TypeReal, ksql.TypeUnknown:
			default:
				return ksql.TypedExpression{}, fmt.Errorf("scalar range currently requires numeric inputs; temporal ranges remain unsupported")
			}
			values = append(values, arg.SQL)
		}
		if len(values) == 2 {
			values = append(values, &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "1"})
		}
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_scalar_range", Args: values}}, nil
	})
}
