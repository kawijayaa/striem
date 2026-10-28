package kql

import (
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

// digestKind names the value kind a t-digest stores.
func digestKind(t ksql.ScalarType) (string, bool) {
	switch t {
	case ksql.TypeInt, ksql.TypeLong, ksql.TypeReal, ksql.TypeDateTime, ksql.TypeTimespan, ksql.TypeBool:
		return string(t), true
	case ksql.TypeUnknown:
		return "real", true
	}
	return "", false
}

func sketchText(value string) sqlast.Expr {
	return &sqlast.Literal{Kind: sqlast.StringLiteral, Value: value}
}
func sketchNull() sqlast.Expr { return &sqlast.Literal{Kind: sqlast.NullLiteral} }

// percentileList builds the JSON percentile list from constant arguments:
// numbers, or one dynamic array.
func percentileList(name string, args []ksql.TypedExpression) (sqlast.Expr, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("%s requires percentiles", name)
	}
	if len(args) == 1 && args[0].Type == ksql.TypeDynamic {
		return args[0].SQL, nil
	}
	values := make([]sqlast.Expr, len(args))
	for i, arg := range args {
		if !numericType(arg.Type) {
			return nil, fmt.Errorf("%s percentiles must be numbers", name)
		}
		values[i] = arg.SQL
	}
	return &sqlast.Call{Name: "json_array", Args: values}, nil
}

// requestedType reads an optional typeof(T) argument.
func requestedType(arg ksql.TypedExpression) (ksql.ScalarType, bool) {
	literal, ok := arg.SQL.(*sqlast.Literal)
	if !ok || literal.Kind != sqlast.StringLiteral || arg.Type != ksql.TypeString {
		return "", false
	}
	switch strings.ToLower(literal.Value) {
	case "int", "long", "real", "double", "datetime", "timespan", "bool", "string", "dynamic":
		kind := ksql.ScalarType(strings.ToLower(literal.Value))
		if kind == "double" {
			kind = ksql.TypeReal
		}
		return kind, true
	}
	return "", false
}

func sketchOptions() []ksql.Option {
	var options []ksql.Option
	aggregate := func(name string, rule ksql.TypedFunctionRule) {
		options = append(options, ksql.WithTypedAggregate(name, rule))
	}
	function := func(name string, rule ksql.TypedFunctionRule) {
		options = append(options, ksql.WithTypedFunction(name, rule))
	}
	weight := func(name string, arg ksql.TypedExpression) error {
		if arg.Type != ksql.TypeInt && arg.Type != ksql.TypeLong && arg.Type != ksql.TypeUnknown {
			return fmt.Errorf("%s weights must be integers", name)
		}
		return nil
	}
	// percentile(x, p), percentilew(x, w, p), percentiles_array(x, ...),
	// percentilesw_array(x, w, ...) and tdigest(x[, w]).
	for _, config := range []struct {
		name, runtime string
		weighted      bool
		array, digest bool
	}{
		{"percentile", "kql_percentile", false, false, false},
		{"percentilew", "kql_percentile", true, false, false},
		{"percentiles_array", "kql_percentiles_array", false, true, false},
		{"percentilesw_array", "kql_percentiles_array", true, true, false},
		{"tdigest", "kql_tdigest", false, false, true},
	} {
		config := config
		aggregate(config.name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			fixed := 1
			if config.weighted {
				fixed = 2
			}
			if len(args) < fixed || (!config.digest && len(args) == fixed) {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires a value%s and percentiles", config.name, map[bool]string{true: ", a weight"}[config.weighted])
			}
			kind, ok := digestKind(args[0].Type)
			if !ok {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires a numeric, datetime or timespan value", config.name)
			}
			weightSQL := sketchNull()
			if config.weighted {
				if err := weight(config.name, args[1]); err != nil {
					return ksql.TypedExpression{}, err
				}
				weightSQL = args[1].SQL
			}
			points := sketchNull()
			switch {
			case config.digest:
				if len(args) > 2 {
					return ksql.TypedExpression{}, fmt.Errorf("tdigest takes a value and an optional weight")
				}
				if len(args) == 2 {
					if err := weight(config.name, args[1]); err != nil {
						return ksql.TypedExpression{}, err
					}
					weightSQL = args[1].SQL
				}
			case config.array:
				list, err := percentileList(config.name, args[fixed:])
				if err != nil {
					return ksql.TypedExpression{}, err
				}
				points = list
			default:
				if len(args) != fixed+1 || !numericType(args[fixed].Type) {
					return ksql.TypedExpression{}, fmt.Errorf("%s requires one numeric percentile", config.name)
				}
				points = args[fixed].SQL
			}
			result := args[0].Type
			if config.array || config.digest {
				result = ksql.TypeDynamic
			} else if result == ksql.TypeUnknown {
				result = ksql.TypeReal
			}
			return ksql.TypedExpression{Type: result, SQL: &sqlast.Call{Name: config.runtime, Args: []sqlast.Expr{args[0].SQL, sketchText(kind), weightSQL, points}}}, nil
		})
	}
	// tdigest_merge / merge_tdigest aggregate one sketch per row; with
	// several arguments merge_tdigest merges them per row.
	for _, name := range []string{"tdigest_merge", "merge_tdigest"} {
		name := name
		aggregate(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			for _, arg := range args {
				if arg.Type != ksql.TypeDynamic && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("%s requires tdigest sketches", name)
				}
			}
			switch {
			case len(args) == 1:
				return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_tdigest_merge", Args: []sqlast.Expr{args[0].SQL}}}, nil
			case len(args) > 1 && len(args) <= 64:
				values := make([]sqlast.Expr, len(args))
				for i, arg := range args {
					values[i] = arg.SQL
				}
				return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_merge_tdigest", Args: values}}, nil
			}
			return ksql.TypedExpression{}, fmt.Errorf("%s takes 1 to 64 sketches", name)
		})
		options = append(options, ksql.WithScalarOverload(name, 2))
	}
	// percentile_tdigest(t, p[, typeof(T)]) and percentiles_array_tdigest.
	function("percentile_tdigest", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) < 2 || len(args) > 3 || !numericType(args[1].Type) {
			return ksql.TypedExpression{}, fmt.Errorf("percentile_tdigest requires a sketch, a percentile and an optional typeof(T)")
		}
		call := sqlast.Expr(&sqlast.Call{Name: "kql_percentile_tdigest", Args: []sqlast.Expr{args[0].SQL, args[1].SQL}})
		if len(args) == 3 {
			kind, ok := requestedType(args[2])
			if !ok {
				return ksql.TypedExpression{}, fmt.Errorf("percentile_tdigest type must be typeof(T)")
			}
			return ksql.TypedExpression{Type: kind, SQL: call}, nil
		}
		// Without a type the result is dynamic, as the sketch's kind is known
		// only at run time.
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "json_quote", Args: []sqlast.Expr{call}}}, nil
	})
	function("percentiles_array_tdigest", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) < 2 {
			return ksql.TypedExpression{}, fmt.Errorf("percentiles_array_tdigest requires a sketch and percentiles")
		}
		points := args[1:]
		if last := points[len(points)-1]; len(points) > 1 {
			if _, ok := requestedType(last); ok {
				points = points[:len(points)-1]
			}
		}
		list, err := percentileList("percentiles_array_tdigest", points)
		if err != nil {
			return ksql.TypedExpression{}, err
		}
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_percentiles_array_tdigest", Args: []sqlast.Expr{args[0].SQL, list}}}, nil
	})
	for name, result := range map[string]ksql.ScalarType{"rank_tdigest": ksql.TypeLong, "percentrank_tdigest": ksql.TypeReal} {
		name, result := name, result
		function(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 2 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires a sketch and a value", name)
			}
			return ksql.TypedExpression{Type: result, SQL: &sqlast.Call{Name: "kql_" + name, Args: []sqlast.Expr{args[0].SQL, args[1].SQL}}}, nil
		})
	}
	// hll(x[, accuracy]), hll_if(x, predicate[, accuracy]), hll_merge and
	// dcount_hll.
	for _, name := range []string{"hll", "hll_if"} {
		name := name
		aggregate(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			fixed := 1
			if name == "hll_if" {
				fixed = 2
			}
			if len(args) < fixed || len(args) > fixed+1 {
				return ksql.TypedExpression{}, fmt.Errorf("%s takes %d or %d arguments", name, fixed, fixed+1)
			}
			value := args[0].SQL
			if name == "hll_if" {
				if args[1].Type != ksql.TypeBool && args[1].Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("hll_if predicate must be Boolean")
				}
				value = conditionalValue(value, args[1].SQL)
			}
			accuracy := sqlast.Expr(&sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "1"})
			if len(args) == fixed+1 {
				if args[fixed].Type != ksql.TypeInt && args[fixed].Type != ksql.TypeLong {
					return ksql.TypedExpression{}, fmt.Errorf("%s accuracy must be an integer from 0 to 4", name)
				}
				accuracy = args[fixed].SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_hll", Args: []sqlast.Expr{value, accuracy}}}, nil
		})
	}
	aggregate("hll_merge", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		switch {
		case len(args) == 1:
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_hll_merge", Args: []sqlast.Expr{args[0].SQL, sketchNull()}}}, nil
		case len(args) > 1 && len(args) <= 64:
			values := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				values[i] = arg.SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_hll_merge_values", Args: values}}, nil
		}
		return ksql.TypedExpression{}, fmt.Errorf("hll_merge takes 1 to 64 sketches")
	})
	options = append(options, ksql.WithScalarOverload("hll_merge", 2))
	function("dcount_hll", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 1 {
			return ksql.TypedExpression{}, fmt.Errorf("dcount_hll requires one sketch")
		}
		return ksql.TypedExpression{Type: ksql.TypeLong, SQL: &sqlast.Call{Name: "kql_dcount_hll", Args: []sqlast.Expr{args[0].SQL}}}, nil
	})
	return options
}
