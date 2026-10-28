package kql

import (
	"fmt"
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

func ipRangeOptions() []ksql.Option {
	var options []ksql.Option
	for _, name := range []string{"ipv4_is_in_any_range", "ipv6_is_in_any_range"} {
		name := name
		options = append(options, ksql.WithTypedFunction(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) < 2 || len(args) > 65537 {
				return ksql.TypedExpression{}, fmt.Errorf("%s requires an address and 1 to 65536 ranges", name)
			}
			if typ := args[0].Type; typ != ksql.TypeUnknown && typ != ksql.TypeString && typ != ksql.TypeDynamic {
				return ksql.TypedExpression{}, fmt.Errorf("%s address must be string or dynamic", name)
			}
			var ranges sqlast.Expr
			if len(args) == 2 && args[1].Type == ksql.TypeDynamic {
				ranges = args[1].SQL
			} else {
				for _, arg := range args[1:] {
					if arg.Type != ksql.TypeString && arg.Type != ksql.TypeUnknown {
						return ksql.TypedExpression{}, fmt.Errorf("%s requires range strings or a single dynamic array", name)
					}
				}
				// Keep every SQLite call below the portable function argument limit.
				var chunks []sqlast.Expr
				for start := 1; start < len(args); start += 64 {
					end := min(start+64, len(args))
					values := make([]sqlast.Expr, 0, end-start)
					for _, arg := range args[start:end] {
						values = append(values, arg.SQL)
					}
					chunks = append(chunks, &sqlast.Call{Name: "json_array", Args: values})
				}
				for len(chunks) > 1 {
					var next []sqlast.Expr
					for i := 0; i < len(chunks); i += 2 {
						if i+1 == len(chunks) {
							next = append(next, chunks[i])
							continue
						}
						next = append(next, &sqlast.Call{Name: "kql_array_concat", Args: []sqlast.Expr{chunks[i], chunks[i+1]}})
					}
					chunks = next
				}
				ranges = chunks[0]
			}
			return ksql.TypedExpression{Type: ksql.TypeBool, SQL: &sqlast.Call{Name: "kql_" + name, Args: []sqlast.Expr{args[0].SQL, ranges, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: string(args[0].Type)}}}}, nil
		}))
	}
	return options
}
