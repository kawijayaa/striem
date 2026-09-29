package ksql

import (
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

// deduplicateJoinLeft selects one complete left row per equality-key tuple.
// A representative is intentionally unspecified, as in Kusto innerunique.
func (s *compileState) deduplicateJoinLeft(input Relation, condition sqlast.Expr, leftAlias, rightAlias string, operator kql.Operator) Relation {
	if input.Schema.Unknown {
		s.bindError("KQLB0313", operator.Span, "operator.join.innerunique", "innerunique requires a bound left schema")
		return Relation{}
	}
	keys, err := leftJoinKeys(condition, leftAlias, rightAlias)
	if err != nil {
		s.bindError("KQLB0314", operator.Span, "operator.join.innerunique", err.Error())
		return Relation{}
	}
	rankName := "__ksql_join_rank"
	for suffix := 0; ; suffix++ {
		if _, _, found, ambiguous := input.Schema.Lookup(rankName); !found && !ambiguous {
			break
		}
		rankName = fmt.Sprintf("__ksql_join_rank_%d", suffix)
	}
	partitions := make([]sqlast.Expr, 0, len(keys))
	orders := make([]sqlast.Order, 0, len(keys))
	for _, key := range keys {
		expression := &sqlast.Identifier{Parts: []string{leftAlias, key}}
		partitions = append(partitions, expression)
		orders = append(orders, sqlast.Order{Expr: expression})
	}
	items := make([]sqlast.SelectItem, 0, len(input.Schema.Columns)+1)
	for _, column := range input.Schema.Columns {
		items = append(items, sqlast.SelectItem{Expr: &sqlast.Identifier{Parts: []string{leftAlias, column.Name}}, Alias: column.Name})
	}
	items = append(items, sqlast.SelectItem{Expr: &sqlast.Window{Expr: &sqlast.Call{Name: "row_number"}, PartitionBy: partitions, OrderBy: orders}, Alias: rankName})
	ranked := &sqlast.Select{From: &sqlast.Subquery{Query: input.Query, Alias: leftAlias}, Projections: items}
	alias := s.nextAlias()
	projected := make([]sqlast.SelectItem, 0, len(input.Schema.Columns))
	for _, column := range input.Schema.Columns {
		projected = append(projected, sqlast.SelectItem{Expr: &sqlast.Identifier{Parts: []string{alias, column.Name}}, Alias: column.Name})
	}
	return Relation{Schema: input.Schema.Clone(), Query: &sqlast.Select{From: &sqlast.Subquery{Query: ranked, Alias: alias}, Projections: projected, Where: &sqlast.Binary{Left: &sqlast.Identifier{Parts: []string{alias, rankName}}, Operator: "=", Right: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "1"}}}}
}

func leftJoinKeys(condition sqlast.Expr, leftAlias, rightAlias string) ([]string, error) {
	binary, ok := condition.(*sqlast.Binary)
	if !ok {
		return nil, fmt.Errorf("innerunique requires column equality join keys")
	}
	if strings.EqualFold(binary.Operator, "AND") {
		left, err := leftJoinKeys(binary.Left, leftAlias, rightAlias)
		if err != nil {
			return nil, err
		}
		right, err := leftJoinKeys(binary.Right, leftAlias, rightAlias)
		if err != nil {
			return nil, err
		}
		return append(left, right...), nil
	}
	a, aok := binary.Left.(*sqlast.Identifier)
	b, bok := binary.Right.(*sqlast.Identifier)
	if binary.Operator != "=" || !aok || !bok || len(a.Parts) != 2 || len(b.Parts) != 2 {
		return nil, fmt.Errorf("innerunique requires column equality join keys")
	}
	if a.Parts[0] == rightAlias && b.Parts[0] == leftAlias {
		a, b = b, a
	}
	if a.Parts[0] != leftAlias || b.Parts[0] != rightAlias {
		return nil, fmt.Errorf("innerunique keys must compare left and right columns")
	}
	return []string{a.Parts[1]}, nil
}
