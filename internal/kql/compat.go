package kql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func compatibilityOptions() []ksql.Option {
	options := []ksql.Option{ksql.WithFunction("bin", binFunction)}
	for _, name := range []string{"variance", "variancep", "varianceif", "variancepif", "stdev", "stdevp", "stdevif", "stdevpif"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			required := 1
			if strings.HasSuffix(name, "if") {
				required = 2
			}
			if len(args) != required {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires %d arguments", name, required)
			}
			switch args[0].Type {
			case ksql.TypeInt, ksql.TypeLong, ksql.TypeReal, ksql.TypeUnknown:
			default:
				return ksql.TypedExpression{}, fmt.Errorf("%s requires a numeric expression", name)
			}
			value := args[0].SQL
			if required == 2 {
				if args[1].Type != ksql.TypeBool && args[1].Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("%s predicate must be Boolean", name)
				}
				value = conditionalValue(value, args[1].SQL)
			}
			return ksql.TypedExpression{Type: ksql.TypeReal, SQL: &sqlast.Call{Name: "kql_" + strings.TrimSuffix(name, "if"), Args: []sqlast.Expr{value}}}, nil
		}))
	}

	options = append(options, ksql.WithTypedFunction("replace_strings", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 3 {
			return ksql.TypedExpression{}, fmt.Errorf("replace_strings requires 3 arguments")
		}
		values := make([]sqlast.Expr, 3)
		for i, arg := range args {
			required := ksql.TypeDynamic
			if i == 0 {
				required = ksql.TypeString
			}
			if arg.Type != required && arg.Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("replace_strings requires string text and dynamic arrays")
			}
			values[i] = arg.SQL
		}
		return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_replace_strings", Args: values}}, nil
	}))
	for _, name := range []string{"replace_string", "replace"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 3 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 3 arguments", name)
			}
			values := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				if arg.Type != ksql.TypeString && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("%s arguments must be strings", name)
				}
				values[i] = arg.SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_replace_string", Args: values}}, nil
		}))
	}
	for _, name := range []string{"substring", "strlen"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			minimum, maximum := 1, 1
			resultType := ksql.TypeLong
			if name == "substring" {
				minimum, maximum = 2, 3
				resultType = ksql.TypeString
			}
			if len(args) < minimum || len(args) > maximum {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires %d to %d arguments", name, minimum, maximum)
			}
			if args[0].Type != ksql.TypeString && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires a string", name)
			}
			values := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				if i > 0 && arg.Type != ksql.TypeInt && arg.Type != ksql.TypeLong && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("substring offset and length must be integers")
				}
				values[i] = arg.SQL
			}
			return ksql.TypedExpression{Type: resultType, SQL: &sqlast.Call{Name: "kql_" + name, Args: values}}, nil
		}))
	}
	for _, name := range []string{"make_bag", "make_bag_if", "make_dictionary"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			required := 1
			if name == "make_bag_if" {
				required = 2
			}
			if len(args) < required || len(args) > required+1 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires a value%s and optional maxSize", name, map[bool]string{true: " and predicate"}[required == 2])
			}
			maximum := 1048576
			if name == "make_dictionary" {
				maximum = 128
			}
			if len(args) > required {
				literal, ok := args[required].SQL.(*sqlast.Literal)
				if !ok || literal.Kind != sqlast.NumberLiteral {
					return ksql.TypedExpression{}, fmt.Errorf("%s maxSize must be a constant integer from 1 to 1048576", name)
				}
				parsed, err := strconv.Atoi(literal.Value)
				if err != nil || parsed < 1 || parsed > 1048576 {
					return ksql.TypedExpression{}, fmt.Errorf("%s maxSize must be a constant integer from 1 to 1048576", name)
				}
				maximum = parsed
			}
			value := args[0].SQL
			if args[0].Type != ksql.TypeDynamic && args[0].Type != ksql.TypeUnknown {
				value = &sqlast.Literal{Kind: sqlast.NullLiteral}
			}
			if required == 2 {
				if args[1].Type != ksql.TypeBool && args[1].Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("%s predicate must be Boolean", name)
				}
				value = conditionalValue(value, args[1].SQL)
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_make_bag", Args: []sqlast.Expr{value, &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: strconv.Itoa(maximum)}}}}, nil
		}))
	}
	for _, name := range []string{"startofday", "endofday", "startofweek", "endofweek", "startofmonth", "endofmonth", "startofyear", "endofyear"} {
		name := name
		options = append(options, ksql.WithFunction(name, func(args []sqlast.Expr) (sqlast.Expr, error) {
			if len(args) < 1 || len(args) > 2 {
				return nil, fmt.Errorf("%s requires one datetime and an optional integer offset", name)
			}
			offset := sqlast.Expr(&sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "0"})
			if len(args) == 2 {
				offset = args[1]
			}
			unit := strings.TrimPrefix(strings.TrimPrefix(name, "startof"), "endof")
			return &sqlast.Call{Name: "kql_calendar", Args: []sqlast.Expr{args[0], stringLiteral(unit), offset, &sqlast.Literal{Kind: sqlast.BooleanLiteral, Value: strconv.FormatBool(strings.HasPrefix(name, "end"))}}}, nil
		}))
	}
	for _, name := range []string{"avgif", "minif", "maxif"} {
		name := name
		options = append(options, ksql.WithFunction(name, func(args []sqlast.Expr) (sqlast.Expr, error) {
			if len(args) != 2 {
				return nil, fmt.Errorf("%s requires a value and a predicate", name)
			}
			return &sqlast.Call{Name: strings.TrimSuffix(name, "if"), Args: []sqlast.Expr{conditionalValue(args[0], args[1])}}, nil
		}))
	}
	for _, name := range []string{"dcount", "dcountif", "count_distinct", "count_distinctif"} {
		name := name
		options = append(options, ksql.WithFunction(name, func(args []sqlast.Expr) (sqlast.Expr, error) {
			required := 1
			if strings.HasSuffix(name, "if") {
				required = 2
			}
			max := required
			if strings.HasPrefix(name, "dcount") {
				max++
			}
			if len(args) < required || len(args) > max {
				return nil, fmt.Errorf("%s requires %d arguments%s", name, required, map[bool]string{true: " and optional accuracy 0–4"}[max > required])
			}
			if len(args) > required {
				literal, ok := args[required].(*sqlast.Literal)
				if !ok || literal.Kind != sqlast.NumberLiteral {
					return nil, fmt.Errorf("%s accuracy must be a constant integer from 0 to 4", name)
				}
				accuracy, err := strconv.Atoi(literal.Value)
				if err != nil || accuracy < 0 || accuracy > 4 {
					return nil, fmt.Errorf("%s accuracy must be a constant integer from 0 to 4", name)
				}
			}
			value := args[0]
			if required == 2 {
				value = conditionalValue(value, args[1])
			}
			return &sqlast.Call{Name: "kql_count_distinct", Args: []sqlast.Expr{value}}, nil
		}))
	}
	return options
}

func conditionalValue(value, predicate sqlast.Expr) sqlast.Expr {
	return &sqlast.Case{Branches: []sqlast.When{{Condition: predicate, Result: value}}, Else: &sqlast.Literal{Kind: sqlast.NullLiteral}}
}

func binFunction(args []sqlast.Expr) (sqlast.Expr, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("bin requires a value and bin size")
	}
	if literal, ok := args[1].(*sqlast.Literal); ok && literal.Kind == sqlast.IntervalLiteral {
		duration, err := parseDuration(literal.Value)
		if err != nil {
			return nil, err
		}
		if value, ok := args[0].(*sqlast.Literal); ok && value.Kind == sqlast.IntervalLiteral {
			return nil, fmt.Errorf("bin currently supports numeric and datetime values, not timespans")
		}
		return &sqlast.Call{Name: "kql_bin_datetime", Args: []sqlast.Expr{args[0], &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: strconv.FormatInt(int64(duration), 10)}}}, nil
	}
	return &sqlast.Call{Name: "kql_bin", Args: args}, nil
}
