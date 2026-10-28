package kql

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func stringJoinOptions() []ksql.Option {
	return []ksql.Option{
		ksql.WithTypedFunction("strcat_array", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 2 {
				return ksql.TypedExpression{}, fmt.Errorf("strcat_array requires an array and string delimiter")
			}
			if args[0].Type != ksql.TypeDynamic && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("strcat_array requires dynamic array input")
			}
			if args[1].Type != ksql.TypeString && args[1].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("strcat_array delimiter must be a string")
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_strcat_array", Args: []sqlast.Expr{args[0].SQL, args[1].SQL}}}, nil
		}),
		ksql.WithTypedFunction("strcat_delim", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 3 || len(args) > 65 {
				return ksql.TypedExpression{}, fmt.Errorf("strcat_delim requires a delimiter and 2 to 64 scalar arguments")
			}
			if args[0].Type != ksql.TypeString && args[0].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("strcat_delim delimiter must be a string")
			}
			values := []sqlast.Expr{args[0].SQL}
			for _, arg := range args[1:] {
				switch arg.Type {
				case ksql.TypeString, ksql.TypeBool, ksql.TypeInt, ksql.TypeLong, ksql.TypeReal, ksql.TypeDateTime, ksql.TypeUnknown:
				default:
					return ksql.TypedExpression{}, fmt.Errorf("strcat_delim scalar conversion for %s remains unsupported", arg.Type)
				}
				values = append(values, joinScalarSQL(arg))
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_strcat_delim", Args: values}}, nil
		}),

		ksql.WithTypedFunction("strrep", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 2 || len(args) > 3 {
				return ksql.TypedExpression{}, fmt.Errorf("strrep requires 2 or 3 arguments")
			}
			if args[1].Type != ksql.TypeInt && args[1].Type != ksql.TypeLong && args[1].Type != ksql.TypeUnknown {
				return ksql.TypedExpression{}, fmt.Errorf("strrep multiplier must be int or long")
			}
			for i, arg := range args {
				if i == 1 {
					continue
				}
				switch arg.Type {
				case ksql.TypeString, ksql.TypeInt, ksql.TypeLong, ksql.TypeReal, ksql.TypeBool, ksql.TypeDateTime, ksql.TypeUnknown:
				default:
					return ksql.TypedExpression{}, fmt.Errorf("strrep scalar conversion for %s remains unsupported", arg.Type)
				}
			}
			var delimiter sqlast.Expr = &sqlast.Literal{Kind: sqlast.StringLiteral, Value: ""}
			if len(args) == 3 {
				delimiter = joinScalarSQL(args[2])
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_strrep", Args: []sqlast.Expr{joinScalarSQL(args[0]), args[1].SQL, delimiter}}}, nil
		}),
	}
}

func joinScalarSQL(arg ksql.TypedExpression) sqlast.Expr {
	return &sqlast.Call{Name: "kql_join_scalar_text", Args: []sqlast.Expr{arg.SQL, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: string(arg.Type)}}}
}
