package kql

import (
	"fmt"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func seriesArgument(arg ksql.TypedExpression, scalar bool) bool {
	return arg.Type == ksql.TypeDynamic || arg.Type == ksql.TypeUnknown || (scalar && numericType(arg.Type))
}

// seriesOptions maps the series_* family to Striem's series runtime.
func seriesOptions() []ksql.Option {
	var options []ksql.Option
	typed := func(name string, rule ksql.TypedFunctionRule) {
		options = append(options, ksql.WithTypedFunction(name, rule))
	}
	label := func(text string) sqlast.Expr { return &sqlast.Literal{Kind: sqlast.StringLiteral, Value: text} }
	for _, operation := range []string{"add", "subtract", "multiply", "divide", "pow", "greater", "greater_equals", "less", "less_equals", "equals", "not_equals"} {
		operation := operation
		typed("series_"+operation, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 2 || !seriesArgument(args[0], true) || !seriesArgument(args[1], true) {
				return ksql.TypedExpression{}, fmt.Errorf("series_%s requires two numeric series or scalars", operation)
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_series_binary", Args: []sqlast.Expr{label(operation), args[0].SQL, args[1].SQL}}}, nil
		})
	}
	for _, operation := range []string{"abs", "acos", "asin", "atan", "ceiling", "cos", "exp", "floor", "log", "sign", "sin", "sqrt", "tan"} {
		operation := operation
		typed("series_"+operation, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 || !seriesArgument(args[0], false) {
				return ksql.TypedExpression{}, fmt.Errorf("series_%s requires one numeric series", operation)
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_series_unary", Args: []sqlast.Expr{label(operation), args[0].SQL}}}, nil
		})
	}
	for kind, arity := range map[string][2]int{"const": {1, 3}, "forward": {1, 2}, "backward": {1, 2}, "linear": {1, 4}} {
		kind, arity := kind, arity
		typed("series_fill_"+kind, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < arity[0] || len(args) > arity[1] || !seriesArgument(args[0], false) {
				return ksql.TypedExpression{}, fmt.Errorf("series_fill_%s requires a series and up to %d options", kind, arity[1]-1)
			}
			values := []sqlast.Expr{label(kind)}
			for _, arg := range args {
				values = append(values, arg.SQL)
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_series_fill", Args: values}}, nil
		})
	}
	single := func(name, sqlName string) {
		typed(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 1 || len(args) > 2 || !seriesArgument(args[0], false) {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires one numeric series", name)
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: sqlName, Args: []sqlast.Expr{args[0].SQL}}}, nil
		})
	}
	single("series_stats_dynamic", "kql_series_stats")
	single("series_fit_line_dynamic", "kql_series_fit_line")
	typed("series_outliers", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) < 1 || len(args) > 5 || !seriesArgument(args[0], false) {
			return ksql.TypedExpression{}, fmt.Errorf("series_outliers requires a series and up to four options")
		}
		values := make([]sqlast.Expr, 5)
		for i := range values {
			values[i] = &sqlast.Literal{Kind: sqlast.NullLiteral}
			if i < len(args) {
				values[i] = args[i].SQL
			}
		}
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_series_outliers", Args: values}}, nil
	})
	typed("series_fir", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) < 2 || len(args) > 4 {
			return ksql.TypedExpression{}, fmt.Errorf("series_fir requires a series, a filter and optional normalize and center flags")
		}
		values := []sqlast.Expr{args[0].SQL, args[1].SQL, &sqlast.Literal{Kind: sqlast.NullLiteral}, &sqlast.Literal{Kind: sqlast.NullLiteral}}
		for i := 2; i < len(args); i++ {
			values[i] = args[i].SQL
		}
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_series_fir", Args: values}}, nil
	})
	typed("series_iir", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 3 {
			return ksql.TypedExpression{}, fmt.Errorf("series_iir requires a series, numerators and denominators")
		}
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_series_iir", Args: []sqlast.Expr{args[0].SQL, args[1].SQL, args[2].SQL}}}, nil
	})
	for operation, arity := range map[string]int{"magnitude": 1, "product": 1, "dot_product": 2, "cosine_similarity": 2, "pearson_correlation": 2} {
		operation, arity := operation, arity
		typed("series_"+operation, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != arity {
				return ksql.TypedExpression{}, fmt.Errorf("series_%s requires %d series", operation, arity)
			}
			values := []sqlast.Expr{label(operation)}
			for _, arg := range args {
				if !seriesArgument(arg, false) {
					return ksql.TypedExpression{}, fmt.Errorf("series_%s requires numeric series", operation)
				}
				values = append(values, arg.SQL)
			}
			kind := ksql.TypeReal
			if operation == "product" {
				kind = ksql.TypeUnknown
			}
			return ksql.TypedExpression{Type: kind, SQL: &sqlast.Call{Name: "kql_series_reduce", Args: values}}, nil
		})
	}
	return options
}
