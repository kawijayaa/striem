package kql

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func hashOptions() []ksql.Option {
	var options []ksql.Option
	for _, name := range []string{"hash_md5", "hash_sha1", "hash_sha256"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 1 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires 1 scalar argument", name)
			}
			switch args[0].Type {
			case ksql.TypeString, ksql.TypeBool, ksql.TypeInt, ksql.TypeLong, ksql.TypeReal, ksql.TypeDateTime, ksql.TypeUnknown:
			default:
				return ksql.TypedExpression{}, fmt.Errorf("%s scalar type %s is not yet supported", name, args[0].Type)
			}
			return ksql.TypedExpression{Type: ksql.TypeString, SQL: &sqlast.Call{Name: "kql_" + name, Args: []sqlast.Expr{args[0].SQL, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: string(args[0].Type)}}}}, nil
		}))
	}
	return options
}
