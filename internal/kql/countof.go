package kql

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func countOfOption() ksql.Option {
	return ksql.WithTypedFunction("countof", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) < 2 || len(args) > 3 {
			return ksql.TypedExpression{}, fmt.Errorf("countof requires 2 or 3 string arguments")
		}
		values := make([]sqlast.Expr, 0, 3)
		for _, arg := range args {
			if arg.Type != ksql.TypeString && arg.Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("countof arguments must be strings")
			}
			values = append(values, arg.SQL)
		}
		if len(values) == 2 {
			values = append(values, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "normal"})
		}
		if literal, ok := values[2].(*sqlast.Literal); ok && literal.Kind == sqlast.StringLiteral && literal.Value != "normal" && literal.Value != "regex" {
			return ksql.TypedExpression{}, fmt.Errorf("countof kind must be normal or regex")
		}
		return ksql.TypedExpression{Type: ksql.TypeLong, SQL: &sqlast.Call{Name: "kql_countof", Args: values}}, nil
	})
}
