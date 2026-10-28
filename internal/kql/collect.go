package kql

import (
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/sqlast"
)

// collectOptions maps make_list, make_set, their _if forms,
// make_list_with_nulls and the deprecated makelist/makeset to Striem's
// collector, which adds the elements of dynamic arrays individually.
func collectOptions() []ksql.Option {
	var options []ksql.Option
	for name, runtime := range map[string]string{
		"make_list": "kql_collect_list", "makelist": "kql_collect_list", "make_list_if": "kql_collect_list",
		"make_set": "kql_collect_set", "makeset": "kql_collect_set", "make_set_if": "kql_collect_set",
		"make_list_with_nulls": "kql_collect_list_nulls",
	} {
		name, runtime := name, runtime
		options = append(options, ksql.WithTypedAggregate(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			conditional := strings.HasSuffix(name, "_if")
			fixed := 1
			if conditional {
				fixed = 2
			}
			maximum := fixed + 1
			if name == "make_list_with_nulls" {
				maximum = 1
			}
			if len(args) < fixed || len(args) > maximum {
				return ksql.TypedExpression{}, fmt.Errorf("%s takes %d to %d arguments", name, fixed, maximum)
			}
			value := args[0].SQL
			if conditional {
				if args[1].Type != ksql.TypeBool && args[1].Type != ksql.TypeUnknown {
					return ksql.TypedExpression{}, fmt.Errorf("%s predicate must be Boolean", name)
				}
				value = conditionalValue(value, args[1].SQL)
			}
			limit := sqlast.Expr(&sqlast.Literal{Kind: sqlast.NullLiteral})
			if len(args) == fixed+1 {
				if args[fixed].Type != ksql.TypeInt && args[fixed].Type != ksql.TypeLong {
					return ksql.TypedExpression{}, fmt.Errorf("%s maximum size must be an integer", name)
				}
				limit = args[fixed].SQL
			}
			kind := string(args[0].Type)
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: runtime, Args: []sqlast.Expr{
				value, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: kind}, limit}}}, nil
		}))
	}
	options = append(options, ksql.WithTypedAggregate("buildschema", func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
		if len(args) != 1 || (args[0].Type != ksql.TypeDynamic && args[0].Type != ksql.TypeUnknown) {
			return ksql.TypedExpression{}, fmt.Errorf("buildschema requires one dynamic value")
		}
		return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: "kql_buildschema", Args: []sqlast.Expr{args[0].SQL}}}, nil
	}))
	for name, sqlName := range map[string]string{"reduce_patterns": "kql_reduce", "reduce_source": "kql_reduce_source"} {
		options = append(options, ksql.WithTypedAggregate(name, func(args []ksql.TypedExpression) (ksql.TypedExpression, error) {
			if len(args) != 3 || (args[0].Type != ksql.TypeString && args[0].Type != ksql.TypeUnknown) {
				return ksql.TypedExpression{}, fmt.Errorf("reduce requires a string expression")
			}
			return ksql.TypedExpression{Type: ksql.TypeDynamic, SQL: &sqlast.Call{Name: sqlName, Args: []sqlast.Expr{args[0].SQL, args[1].SQL, args[2].SQL}}}, nil
		}))
	}
	return options
}
