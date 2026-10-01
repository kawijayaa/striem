package kql

import (
	"fmt"
	"regexp"
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

	options = append(options, ksql.WithTypedFunction("repeat", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 2 {
			return ksql.TypedExpression{}, fmt.Errorf("repeat requires 2 arguments")
		}
		if args[1].Type != ksql.TypeInt && args[1].Type != ksql.TypeLong && args[1].Type != ksql.TypeUnknown {
			return ksql.TypedExpression{}, fmt.Errorf("repeat count must be an integer")
		}
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_repeat", Args: []sqlast.Expr{args[0].SQL, args[1].SQL, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: string(args[0].Type)}}}}, nil
	}))

	for _, name := range []string{"array_iff", "array_iif"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 3 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 3 arguments", name)
			}
			if args[0].Type != ksql.TypeDynamic && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires a dynamic condition array", name)
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_array_iff", Args: []sqlast.Expr{args[0].SQL, args[1].SQL, args[2].SQL, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: string(args[1].Type)}, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: string(args[2].Type)}}}}, nil
		}))
	}

	for _, name := range []string{"array_rotate_left", "array_rotate_right", "array_shift_left", "array_shift_right"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			shift := strings.HasPrefix(name, "array_shift_")
			maximum := 2
			if shift {
				maximum = 3
			}
			if len(args) < 2 || len(args) > maximum {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 2 to %d arguments", name, maximum)
			}
			if args[0].Type != ksql.TypeDynamic && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires a dynamic array", name)
			}
			if args[1].Type != ksql.TypeInt && args[1].Type != ksql.TypeLong && args[1].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("%s count must be an integer", name)
			}
			values := []sqlast.Expr{args[0].SQL, args[1].SQL}
			if shift {
				var padding sqlast.Expr = &sqlast.Literal{Kind: sqlast.NullLiteral}
				kind := "__array_default"
				if len(args) == 3 {
					padding = args[2].SQL
					kind = string(args[2].Type)
				}
				values = append(values, padding, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: kind})
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_" + name, Args: values}}, nil
		}))
	}

	for _, name := range []string{"array_index_of", "set_has_element"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			maximum := 5
			resultType := ksql.TypeLong
			function := "kql_array_index_of"
			if name == "set_has_element" {
				maximum = 2
				resultType = ksql.TypeBool
				function = "kql_set_has_element_typed"
			}
			if len(args) < 2 || len(args) > maximum {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 2 to %d arguments", name, maximum)
			}
			if args[0].Type != ksql.TypeDynamic && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires a dynamic array", name)
			}
			values := []sqlast.Expr{args[0].SQL, args[1].SQL, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: string(args[1].Type)}}
			for i, arg := range args[2:] {
				if arg.Type != ksql.TypeInt && arg.Type != ksql.TypeLong && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("array_index_of start, length, and occurrence must be integers")
				}
				if i == 2 && !integerSQLConstant(arg.SQL) {
					return ksql.TypedExpression{}, fmt.Errorf("array_index_of occurrence must be a constant integer")
				}
				values = append(values, arg.SQL)
			}
			return ksql.TypedExpression{Type: resultType, SQL: &sqlast.Call{Name: function, Args: values}}, nil
		}))
	}

	for _, name := range []string{"array_sum", "series_sum"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 1 argument", name)
			}
			if args[0].Type != ksql.TypeDynamic && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires a dynamic array", name)
			}
			return ksql.TypedExpression{Type: ksql.TypeReal, SQL: &sqlast.Call{Name: "kql_array_sum", Args: []sqlast.Expr{args[0].SQL}}}, nil
		}))
	}

	options = append(options, ksql.WithTypedFunction("array_split", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 2 {
			return ksql.TypedExpression{}, fmt.Errorf("array_split requires 2 arguments")
		}
		if args[0].Type != ksql.TypeDynamic && args[0].Type != ksql.TypeUnknown {
			return ksql.TypedExpression{}, fmt.Errorf("array_split requires a dynamic array")
		}
		switch args[1].Type {
		case ksql.TypeInt, ksql.TypeLong, ksql.TypeDynamic, ksql.TypeUnknown:
		default:
			return ksql.TypedExpression{}, fmt.Errorf("array_split requires an integer or dynamic integer array")
		}
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_array_split", Args: []sqlast.Expr{args[0].SQL, args[1].SQL}}}, nil
	}))

	options = append(options, ksql.WithTypedFunction("array_slice", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 3 {
			return ksql.TypedExpression{}, fmt.Errorf("array_slice requires 3 arguments")
		}
		if args[0].Type != ksql.TypeDynamic && args[0].Type != ksql.TypeUnknown {
			return ksql.TypedExpression{}, fmt.Errorf("array_slice requires a dynamic array")
		}
		for _, arg := range args[1:] {
			if arg.Type != ksql.TypeInt && arg.Type != ksql.TypeLong && arg.Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("array_slice indices must be integers")
			}
		}
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_array_slice", Args: []sqlast.Expr{args[0].SQL, args[1].SQL, args[2].SQL}}}, nil
	}))

	for _, name := range []string{"array_concat", "array_reverse", "zip"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			maximum := 64
			if name == "array_reverse" {
				maximum = 1
			}
			minimum := 1
			if name == "zip" {
				minimum = 2
			}
			if len(args) < minimum || len(args) > maximum {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires %d to %d dynamic arrays", name, minimum, maximum)
			}
			values := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				if arg.Type != ksql.TypeDynamic && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("%s arguments must be dynamic arrays", name)
				}
				values[i] = arg.SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_" + name, Args: values}}, nil
		}))
	}

	options = append(options, ksql.WithTypedFunction("bag_set_key", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 3 {
			return ksql.TypedExpression{}, fmt.Errorf("bag_set_key requires 3 arguments")
		}
		if args[0].Type != ksql.TypeDynamic && args[0].Type != ksql.TypeUnknown {
			return ksql.TypedExpression{}, fmt.Errorf("bag_set_key bag must be dynamic")
		}
		if args[1].Type != ksql.TypeString && args[1].Type != ksql.TypeUnknown {
			return ksql.TypedExpression{}, fmt.Errorf("bag_set_key key must be a string")
		}
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_bag_set_key", Args: []sqlast.Expr{args[0].SQL, args[1].SQL, args[2].SQL, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: string(args[2].Type)}}}}, nil
	}))

	for _, name := range []string{"bag_merge", "bag_remove_keys"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			maximum := 64
			if name == "bag_remove_keys" {
				maximum = 2
			}
			if len(args) < 2 || len(args) > maximum {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 2 to %d arguments", name, maximum)
			}
			values := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				if arg.Type != ksql.TypeDynamic && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("%s requires dynamic arguments", name)
				}
				values[i] = arg.SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_" + name, Args: values}}, nil
		}))
	}
	for _, name := range []string{"pack_array", "bag_pack", "pack", "pack_dictionary"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			array := name == "pack_array"
			if len(args) == 0 || (array && len(args) > 64) || (!array && (len(args)%2 != 0 || len(args) > 128)) {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires %s", name, map[bool]string{true: "1 to 64 values", false: "1 to 64 key/value pairs"}[array])
			}
			values := make([]sqlast.Expr, 1, len(args)+1)
			kinds := make([]string, 0, len(args))
			for i, arg := range args {
				if _, star := arg.SQL.(*sqlast.Star); star {
					return ksql.TypedExpression{}, fmt.Errorf("pack_array wildcard must be the sole argument")
				}
				if !array && i%2 == 0 {
					if arg.Type != ksql.TypeString && arg.Type != ksql.TypeUnknown {
						return ksql.TypedExpression{}, fmt.Errorf("%s keys must be strings", name)
					}
				} else {
					kinds = append(kinds, string(arg.Type))
				}
				values = append(values, arg.SQL)
			}
			values[0] = &sqlast.Literal{Kind: sqlast.StringLiteral, Value: strings.Join(kinds, ",")}
			function := "kql_bag_pack"
			if array {
				function = "kql_pack_array"
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: function, Args: values}}, nil
		}))
	}
	for _, name := range []string{"trim", "trim_start", "trim_end"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 2 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 2 arguments", name)
			}
			for _, arg := range args {
				if arg.Type != ksql.TypeString && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("%s arguments must be strings", name)
				}
			}
			pattern, ok := args[0].SQL.(*sqlast.Literal)
			if !ok || pattern.Kind != sqlast.StringLiteral {
				return ksql.TypedExpression{}, fmt.Errorf("%s regex must be a constant string", name)
			}
			if len(pattern.Value) > 512 {
				return ksql.TypedExpression{}, fmt.Errorf("%s regex exceeds 512 bytes", name)
			}
			if _, err := regexp.Compile(pattern.Value); err != nil {
				return ksql.TypedExpression{}, fmt.Errorf("%s regex is invalid: %w", name, err)
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_" + name, Args: []sqlast.Expr{args[0].SQL, args[1].SQL}}}, nil
		}))
	}
	options = append(options, ksql.WithTypedFunction("split", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) < 2 || len(args) > 3 {
			return ksql.TypedExpression{}, fmt.Errorf("split requires 2 or 3 arguments")
		}
		if args[1].Type != ksql.TypeString && args[1].Type != ksql.TypeUnknown {
			return ksql.TypedExpression{}, fmt.Errorf("split delimiter must be a string")
		}
		if len(args) == 3 && args[2].Type != ksql.TypeInt && args[2].Type != ksql.TypeLong && args[2].Type != ksql.TypeUnknown {
			return ksql.TypedExpression{}, fmt.Errorf("split requestedIndex must be an integer")
		}
		values := make([]sqlast.Expr, len(args))
		for i, arg := range args {
			values[i] = arg.SQL
		}
		values[0] = scalarTextSQL(args[0])
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_split", Args: values}}, nil
	}))
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
	for _, name := range []string{"replace_string"} {
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
	for _, name := range []string{"replace_regex", "replace"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 3 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 3 arguments", name)
			}
			if name == "replace" {
				args = []ksql.TypedExpression{args[2], args[0], args[1]}
			}
			values := make([]sqlast.Expr, 3)
			for i, arg := range args {
				if arg.Type != ksql.TypeString && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("%s arguments must be strings", name)
				}
				values[i] = arg.SQL
				if i > 0 {
					literal, ok := arg.SQL.(*sqlast.Literal)
					if !ok || literal.Kind != sqlast.StringLiteral {
						return ksql.TypedExpression{}, fmt.Errorf("%s regex and rewrite must be constant strings", name)
					}
					if i == 1 {
						if len(literal.Value) > 512 {
							return ksql.TypedExpression{}, fmt.Errorf("%s regex exceeds 512 bytes", name)
						}
						if _, err := regexp.Compile(literal.Value); err != nil {
							return ksql.TypedExpression{}, fmt.Errorf("%s regex is invalid: %w", name, err)
						}
					}
				}
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_replace_regex", Args: values}}, nil
		}))
	}
	options = append(options, ksql.WithTypedFunction("indexof", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) < 2 || len(args) > 5 {
			return ksql.TypedExpression{}, fmt.Errorf("indexof requires 2 to 5 arguments")
		}
		values := make([]sqlast.Expr, len(args))
		for i, arg := range args {
			values[i] = arg.SQL
			if i < 2 {
				values[i] = scalarTextSQL(arg)
				continue
			}
			if arg.Type != ksql.TypeInt && arg.Type != ksql.TypeLong && arg.Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("indexof start, length, and occurrence must be integers")
			}
			if i == 4 {
				if !integerSQLConstant(arg.SQL) {
					return ksql.TypedExpression{}, fmt.Errorf("indexof occurrence must be a constant integer")
				}
			}
		}
		return ksql.TypedExpression{Type: ksql.TypeLong, SQL: &sqlast.Call{Name: "kql_indexof", Args: values}}, nil
	}))
	for _, name := range []string{"tolower", "toupper"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 1 argument", name)
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_" + name, Args: []sqlast.Expr{scalarTextSQL(args[0])}}}, nil
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
			if name == "strlen" && args[0].Type != ksql.TypeString && args[0].Type != ksql.TypeDynamic && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires a string or dynamic value", name)
			}
			values := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				if i > 0 && arg.Type != ksql.TypeInt && arg.Type != ksql.TypeLong && arg.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("substring offset and length must be integers")
				}
				values[i] = arg.SQL
			}
			values[0] = scalarTextSQL(args[0])
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

func scalarTextSQL(value ksql.TypedExpression) sqlast.Expr {
	return &sqlast.Call{Name: "kql_scalar_text", Args: []sqlast.Expr{value.SQL, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: string(value.Type)}}}
}

func integerSQLConstant(expr sqlast.Expr) bool {
	if unary, ok := expr.(*sqlast.Unary); ok && (unary.Operator == "-" || unary.Operator == "+") {
		expr = unary.Operand
	}
	literal, ok := expr.(*sqlast.Literal)
	if !ok || literal.Kind != sqlast.NumberLiteral {
		return false
	}
	return !strings.ContainsAny(literal.Value, ".eE")
}
