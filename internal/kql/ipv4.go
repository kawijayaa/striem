package kql

import (
	"fmt"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func ipv4Options() []ksql.Option {
	var options []ksql.Option
	for _, name := range []string{"parse_ipv4", "parse_ipv4_mask", "format_ipv4", "format_ipv4_mask", "ipv4_netmask_suffix"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			minimum, maximum := 1, 1
			if name == "parse_ipv4_mask" {
				minimum, maximum = 2, 2
			}
			format := name == "format_ipv4" || name == "format_ipv4_mask"
			if format {
				maximum = 2
			}
			if len(args) < minimum || len(args) > maximum {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires %d to %d arguments", name, minimum, maximum)
			}
			kind := args[0].Type
			if kind != ksql.TypeUnknown && kind != ksql.TypeString && !(format && (kind == ksql.TypeInt || kind == ksql.TypeLong)) {
				expected := "string"
				if format {
					expected = "string, int or long"
				}
				return ksql.TypedExpression{}, fmt.Errorf("%s currently requires %s input", name, expected)
			}
			if len(args) == 2 && args[1].Type != ksql.TypeUnknown && args[1].Type != ksql.TypeInt && args[1].Type != ksql.TypeLong {
				return ksql.TypedExpression{}, fmt.Errorf("%s prefix must be int or long", name)
			}
			sqlArgs := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				sqlArgs[i] = arg.SQL
			}
			result := ksql.TypeLong
			if format {
				result = ksql.TypeString
			}
			return ksql.TypedExpression{SQL: &sqlast.Call{Name: "kql_" + name, Args: sqlArgs}, Type: result}, nil
		}))
	}
	for _, name := range []string{"ipv4_compare", "ipv4_is_match"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 2 || len(args) > 3 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 2 or 3 arguments", name)
			}
			for _, arg := range args[:2] {
				if arg.Type != ksql.TypeUnknown && arg.Type != ksql.TypeString {
					return ksql.TypedExpression{}, fmt.Errorf("%s addresses must be strings", name)
				}
			}
			if len(args) == 3 && args[2].Type != ksql.TypeUnknown && args[2].Type != ksql.TypeInt && args[2].Type != ksql.TypeLong {
				return ksql.TypedExpression{}, fmt.Errorf("%s prefix must be int or long", name)
			}
			sqlArgs := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				sqlArgs[i] = arg.SQL
			}
			kind := ksql.TypeLong
			if name == "ipv4_is_match" {
				kind = ksql.TypeBool
			}
			return ksql.TypedExpression{SQL: &sqlast.Call{Name: "kql_" + name, Args: sqlArgs}, Type: kind}, nil
		}))
	}
	options = append(options, ksql.WithTypedFunction("ipv4_range_to_cidr_list", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 2 {
			return ksql.TypedExpression{}, fmt.Errorf("ipv4_range_to_cidr_list requires 2 arguments")
		}
		for _, arg := range args {
			if arg.Type != ksql.TypeUnknown && arg.Type != ksql.TypeString {
				return ksql.TypedExpression{}, fmt.Errorf("ipv4_range_to_cidr_list endpoints must be strings")
			}
		}
		return ksql.TypedExpression{SQL: &sqlast.Call{Name: "kql_ipv4_range_to_cidr_list", Args: []sqlast.Expr{args[0].SQL, args[1].SQL}}, Type: ksql.TypeDynamic}, nil
	}))
	return options
}
