package kql

import (
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

// hashKind names the byte form a value is hashed in.
func hashKind(t ksql.ScalarType) string {
	switch t {
	case ksql.TypeDateTime, ksql.TypeBool, ksql.TypeInt:
		return string(t)
	}
	return ""
}

func textToolOptions() []ksql.Option {
	text := func(value string) sqlast.Expr { return &sqlast.Literal{Kind: sqlast.StringLiteral, Value: value} }
	null := func() sqlast.Expr { return &sqlast.Literal{Kind: sqlast.NullLiteral} }
	number := func(value string) sqlast.Expr { return &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: value} }
	options := []ksql.Option{
		ksql.WithTypedFunction("hash", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 1 || len(args) > 2 || args[0].Type == ksql.TypeDynamic {
				return ksql.TypedExpression{}, fmt.Errorf("hash requires a scalar and an optional integer modulo")
			}
			mod := null()
			if len(args) == 2 {
				if args[1].Type != ksql.TypeInt && args[1].Type != ksql.TypeLong {
					return ksql.TypedExpression{}, fmt.Errorf("hash modulo must be an integer")
				}
				mod = args[1].SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeLong, SQL: &sqlast.Call{Name: "kql_hash", Args: []sqlast.Expr{args[0].SQL, text(hashKind(args[0].Type)), mod}}}, nil
		}),
		ksql.WithTypedFunction("hash_many", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 2 || len(args) > 64 {
				return ksql.TypedExpression{}, fmt.Errorf("hash_many takes 2 to 64 scalars")
			}
			kinds := make([]string, len(args))
			values := []sqlast.Expr{nil}
			for i, arg := range args {
				kinds[i] = hashKind(arg.Type)
				values = append(values, arg.SQL)
			}
			values[0] = text(strings.Join(kinds, ","))
			return ksql.TypedExpression{Type: ksql.TypeLong, SQL: &sqlast.Call{Name: "kql_hash_many", Args: values}}, nil
		}),
		ksql.WithTypedFunction("indexof_regex", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 2 || len(args) > 5 {
				return ksql.TypedExpression{}, fmt.Errorf("indexof_regex requires a string, a regex and optional start, length and occurrence")
			}
			values := []sqlast.Expr{args[0].SQL, args[1].SQL, number("0"), number("-1"), number("1")}
			for i := 2; i < len(args); i++ {
				if args[i].Type != ksql.TypeInt && args[i].Type != ksql.TypeLong {
					return ksql.TypedExpression{}, fmt.Errorf("indexof_regex positions must be integers")
				}
				values[i] = args[i].SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeLong, SQL: &sqlast.Call{Name: "kql_indexof_regex", Args: values}}, nil
		}),
		ksql.WithTypedFunction("around", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 3 {
				return ksql.TypedExpression{}, fmt.Errorf("around requires a value, a center and a delta")
			}
			value, center, delta := args[0], args[1], args[2]
			var low, high sqlast.Expr
			switch {
			case value.Type == ksql.TypeDateTime || center.Type == ksql.TypeDateTime:
				if delta.Type != ksql.TypeTimespan && delta.Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("around a datetime requires a timespan delta")
				}
				low = &sqlast.DatetimeAdd{Datetime: center.SQL, Ticks: &sqlast.Unary{Operator: "-", Operand: delta.SQL}}
				high = &sqlast.DatetimeAdd{Datetime: center.SQL, Ticks: delta.SQL}
			case (numericType(value.Type) || value.Type == ksql.TypeTimespan) && (numericType(center.Type) || center.Type == ksql.TypeTimespan):
				low = &sqlast.Binary{Left: center.SQL, Operator: "-", Right: delta.SQL}
				high = &sqlast.Binary{Left: center.SQL, Operator: "+", Right: delta.SQL}
			default:
				return ksql.TypedExpression{}, fmt.Errorf("around requires numbers, datetimes or timespans")
			}
			between := &sqlast.Binary{Left: value.SQL, Operator: "BETWEEN", Right: &sqlast.Binary{Left: low, Operator: "AND", Right: high}}
			return ksql.TypedExpression{Type: ksql.TypeBool, SQL: between}, nil
		}),
	}
	for _, name := range []string{"extract_json", "extractjson"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 2 || len(args) > 3 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires a JSONPath, a JSON value and an optional typeof(T)", name)
			}
			kind, result := text(""), ksql.TypeString
			if len(args) == 3 {
				requested, ok := requestedType(args[2])
				if !ok {
					return ksql.TypedExpression{}, fmt.Errorf("%s type must be typeof(T)", name)
				}
				kind, result = text(string(requested)), requested
				if result == ksql.TypeInt {
					result = ksql.TypeLong
				}
			}
			return ksql.TypedExpression{Type: result, SQL: &sqlast.Call{Name: "kql_extract_json", Args: []sqlast.Expr{args[0].SQL, args[1].SQL, kind}}}, nil
		}))
	}
	return options
}

func init() {
	scalarFunctions["parse_command_line"] = runtimeFunction{"kql_parse_command_line", "ss", 2, ksql.TypeDynamic, nil}
	scalarFunctions["parse_path"] = runtimeFunction{"kql_parse_path", "s", 1, ksql.TypeDynamic, nil}
	scalarFunctions["regex_quote"] = runtimeFunction{"kql_regex_quote", "s", 1, ksql.TypeString, nil}
	scalarFunctions["has_any_index"] = runtimeFunction{"kql_has_any_index", "sd", 2, ksql.TypeLong, nil}
	scalarFunctions["dynamic_to_json"] = runtimeFunction{"kql_dynamic_to_json", "d", 1, ksql.TypeString, nil}
}
