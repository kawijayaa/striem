package kql

import (
	"fmt"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func ipv6Options() []ksql.Option {
	var options []ksql.Option
	for _, name := range []string{"parse_ipv6", "parse_ipv6_mask", "ipv6_compare", "ipv6_is_match", "ipv6_is_in_range"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			minimum, maximum, addresses := 1, 1, 1
			kind := ksql.TypeString
			if name == "parse_ipv6_mask" {
				minimum, maximum = 2, 2
			}
			if name == "ipv6_compare" || name == "ipv6_is_match" {
				minimum, maximum, addresses = 2, 3, 2
				kind = ksql.TypeLong
			}
			if name == "ipv6_is_in_range" {
				minimum, maximum, addresses = 2, 2, 2
				kind = ksql.TypeBool
			}
			if name == "ipv6_is_match" {
				kind = ksql.TypeBool
			}
			if len(args) < minimum || len(args) > maximum {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires %d to %d arguments", name, minimum, maximum)
			}
			for _, arg := range args[:addresses] {
				if arg.Type != ksql.TypeUnknown && arg.Type != ksql.TypeString {
					return ksql.TypedExpression{}, fmt.Errorf("%s addresses must be strings", name)
				}
			}
			if len(args) > addresses {
				prefix := args[addresses].Type
				if prefix != ksql.TypeUnknown && prefix != ksql.TypeInt && prefix != ksql.TypeLong {
					return ksql.TypedExpression{}, fmt.Errorf("%s prefix must be int or long", name)
				}
			}
			sqlArgs := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				sqlArgs[i] = arg.SQL
			}
			return ksql.TypedExpression{SQL: &sqlast.Call{Name: "kql_" + name, Args: sqlArgs}, Type: kind}, nil
		}))
	}
	return options
}
