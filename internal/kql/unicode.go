package kql

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func unicodeOptions() []ksql.Option {
	var options []ksql.Option
	for _, name := range []string{"unicode_codepoints_from_string", "to_utf8"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 || (args[0].Type != ksql.TypeString && args[0].Type != ksql.TypeUnknown) {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 1 string argument", name)
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_codepoints_from_string", Args: []sqlast.Expr{args[0].SQL}}}, nil
		}))
	}
	for _, name := range []string{"unicode_codepoints_to_string", "make_string"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 1 || len(args) > 64 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 1 to 64 integer or array arguments", name)
			}
			arrays := make([]sqlast.Expr, 0, len(args))
			for _, arg := range args {
				switch arg.Type {
				case ksql.TypeDynamic:
					arrays = append(arrays, arg.SQL)
				case ksql.TypeInt, ksql.TypeLong, ksql.TypeUnknown:
					arrays = append(arrays, &sqlast.Call{Name: "json_array", Args: []sqlast.Expr{arg.SQL}})
				default:
					return ksql.TypedExpression{}, fmt.Errorf("%s requires integer or dynamic array arguments", name)
				}
			}
			var values sqlast.Expr = arrays[0]
			if len(arrays) > 1 {
				values = &sqlast.Call{Name: "kql_array_concat", Args: arrays}
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_codepoints_to_string", Args: []sqlast.Expr{values}}}, nil
		}))
	}
	return options
}
