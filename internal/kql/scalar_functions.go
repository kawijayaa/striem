package kql

import (
	"fmt"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

// runtimeFunction describes a runtime-backed call: its SQL function, the
// kind of each parameter (n numeric, d dynamic, s string, b bool,
// t datetime, g guid), how many are required and the result type. Omitted
// optional parameters take the given defaults, or NULL.
type runtimeFunction struct {
	sql      string
	params   string
	required int
	result   ksql.ScalarType
	defaults map[int]sqlast.Expr
}

var scalarFunctions = map[string]runtimeFunction{
	"string_size":            {"kql_string_size", "s", 1, ksql.TypeLong, nil},
	"base64_encode_fromguid": {"kql_base64_encode_fromguid", "g", 1, ksql.TypeString, nil},
	"base64_decode_toguid":   {"kql_base64_decode_toguid", "s", 1, ksql.TypeString, nil},
	"datetime_utc_to_local":  {"kql_datetime_utc_to_local", "ts", 2, ksql.TypeDateTime, nil},
	"datetime_local_to_utc":  {"kql_datetime_local_to_utc", "ts", 2, ksql.TypeDateTime, nil},
	"new_guid":               {"kql_new_guid", "", 0, ksql.TypeString, nil},
}

func init() {
	// has_ipv4 and has_ipv4_prefix take one address; the has_any forms take
	// several, or dynamic arrays.
	scalarFunctions["has_ipv4"] = runtimeFunction{"kql_has_ipv4", "ss", 2, ksql.TypeBool, nil}
	scalarFunctions["has_ipv4_prefix"] = runtimeFunction{"kql_has_ipv4_prefix", "ss", 2, ksql.TypeBool, nil}
}

func scalarParameterAccepts(kind byte, t ksql.ScalarType) bool {
	switch kind {
	case 't':
		return t == ksql.TypeDateTime || t == ksql.TypeUnknown
	case 'g':
		return t == ksql.TypeString || t == ksql.TypeUnknown
	}
	if t == ksql.TypeUnknown {
		return true
	}
	switch kind {
	case 'n':
		return numericType(t)
	case 'd':
		return t == ksql.TypeDynamic
	case 's':
		return t == ksql.TypeString
	case 'b':
		return t == ksql.TypeBool
	}
	return false
}

func scalarFunctionOptions(functions map[string]runtimeFunction, accepts func(byte, ksql.ScalarType) bool) []ksql.Option {
	var options []ksql.Option
	for name, runtime := range map[string]string{"has_any_ipv4": "kql_has_ipv4", "has_any_ipv4_prefix": "kql_has_ipv4_prefix"} {
		name, runtime := name, runtime
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 2 || len(args) > 65 {
				return ksql.TypedExpression{}, fmt.Errorf("%s takes a source and 1 to 64 addresses", name)
			}
			values := make([]sqlast.Expr, len(args))
			for i, arg := range args {
				if arg.Type != ksql.TypeString && arg.Type != ksql.TypeUnknown && (i == 0 || arg.Type != ksql.TypeDynamic) {
					return ksql.TypedExpression{}, fmt.Errorf("%s requires strings or dynamic arrays", name)
				}
				values[i] = arg.SQL
			}
			return ksql.TypedExpression{Type: ksql.TypeBool, SQL: &sqlast.Call{Name: runtime, Args: values}}, nil
		}))
	}
	for name, function := range functions {
		name, function := name, function
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < function.required || len(args) > len(function.params) {
				return ksql.TypedExpression{}, fmt.Errorf("%s takes %d to %d arguments", name, function.required, len(function.params))
			}
			values := make([]sqlast.Expr, len(function.params))
			for i := range values {
				values[i] = &sqlast.Literal{Kind: sqlast.NullLiteral}
				if fallback, ok := function.defaults[i]; ok {
					values[i] = fallback
				}
				if i < len(args) {
					if !accepts(function.params[i], args[i].Type) {
						return ksql.TypedExpression{}, fmt.Errorf("%s argument %d has type %s", name, i+1, args[i].Type)
					}
					values[i] = args[i].SQL
				}
			}
			return ksql.TypedExpression{Type: function.result, SQL: &sqlast.Call{Name: function.sql, Args: values}}, nil
		}))
	}
	return options
}
