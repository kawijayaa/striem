package kql

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func datetimePartOptions() []ksql.Option {
	var options []ksql.Option
	for _, name := range []string{"datetime_part", "datepart"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 2 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires part and datetime", name)
			}
			if args[0].Type != ksql.TypeString && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("%s part must be a string", name)
			}
			if args[1].Type != ksql.TypeDateTime && args[1].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires datetime input", name)
			}
			return ksql.TypedExpression{Type: ksql.TypeInt, SQL: &sqlast.Call{Name: "kql_datetime_part", Args: []sqlast.Expr{args[0].SQL, args[1].SQL}}}, nil
		}))
	}
	for _, config := range []struct{ name, part string }{
		{"getyear", "year"}, {"getmonth", "month"}, {"monthofyear", "month"}, {"dayofmonth", "day"}, {"dayofyear", "dayofyear"}, {"hourofday", "hour"}, {"week_of_year", "week_of_year"}, {"weekofyear", "week_of_year"},
	} {
		config := config
		options = append(options, ksql.WithTypedFunction(config.name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 1 datetime argument", config.name)
			}
			if args[0].Type != ksql.TypeDateTime && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires datetime input", config.name)
			}
			return ksql.TypedExpression{Type: ksql.TypeInt, SQL: &sqlast.Call{Name: "kql_datetime_part", Args: []sqlast.Expr{&sqlast.Literal{Kind: sqlast.StringLiteral, Value: config.part}, args[0].SQL}}}, nil
		}))
	}
	return options
}
