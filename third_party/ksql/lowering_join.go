package ksql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func (s *compileState) join(input Relation, operator kql.Operator) Relation {
	spec := operator.Body.(kql.JoinSpec)
	if operator.Kind == "lookup" && (spec.Kind != "inner" && spec.Kind != "leftouter" || spec.Condition != nil) {
		s.bindError("KQLB0316", operator.Span, "operator.lookup", "lookup requires kind=inner or kind=leftouter and an on clause")
		return Relation{}
	}
	leftAlias := s.nextAlias()
	right := s.pipeline(spec.Right)
	if right.Query == nil {
		return Relation{}
	}
	if operator.Kind == "lookup" && (input.Schema.Unknown || right.Schema.Unknown) {
		s.bindError("KQLB0316", operator.Span, "operator.lookup", "lookup requires bound schemas to omit right-side key columns")
		return Relation{}
	}
	rightAlias := s.nextAlias()
	if s.compiler.limits.MaxJoinKeys > 0 && len(spec.Conditions) > s.compiler.limits.MaxJoinKeys {
		s.bindError("KQLB0312", operator.Span, "operator.join", fmt.Sprintf("join exceeds MaxJoinKeys (%d)", s.compiler.limits.MaxJoinKeys))
		return Relation{}
	}

	condition := s.joinCondition(spec, input.Schema, right.Schema, leftAlias, rightAlias, operator)
	if condition == nil && (len(spec.Conditions) > 0 || spec.Condition != nil) {
		return Relation{}
	}
	if spec.Kind == "leftanti" || spec.Kind == "leftsemi" || spec.Kind == "rightanti" || spec.Kind == "rightsemi" {
		outer, inner := input, right
		outerAlias, innerAlias := leftAlias, rightAlias
		if strings.HasPrefix(spec.Kind, "right") {
			outer, inner = right, input
			outerAlias, innerAlias = rightAlias, leftAlias
		}
		exists := sqlast.Expr(&sqlast.Exists{Query: &sqlast.Select{Projections: []sqlast.SelectItem{{Expr: &sqlast.Literal{Kind: sqlast.NumberLiteral, Value: "1"}}}, From: &sqlast.Subquery{Query: inner.Query, Alias: innerAlias}, Where: condition}})
		if strings.HasSuffix(spec.Kind, "anti") {
			exists = &sqlast.Unary{Operator: "NOT ", Operand: exists}
		}
		return Relation{Query: &sqlast.Select{From: &sqlast.Subquery{Query: outer.Query, Alias: outerAlias}, Where: exists}, Schema: outer.Schema.Clone()}
	}
	if spec.Kind == "innerunique" {
		input = s.deduplicateJoinLeft(input, condition, leftAlias, rightAlias, operator)
		if input.Query == nil {
			return Relation{}
		}
	}
	join := &sqlast.Join{
		Left:  &sqlast.Subquery{Query: input.Query, Alias: leftAlias},
		Right: &sqlast.Subquery{Query: right.Query, Alias: rightAlias},
		On:    condition,
	}
	switch spec.Kind {
	case "inner", "innerunique":
		join.Kind = "INNER"
	case "leftouter":
		join.Kind = "LEFT"
	case "rightouter":
		join.Kind = "RIGHT"
	case "fullouter":
		join.Kind = "FULL"
	default:
		s.error("KQLL0311", operator.Span, "operator."+operator.Kind+"."+spec.Kind, "join kind "+spec.Kind+" requires semi/anti relational lowering")
		return Relation{}
	}
	outputRight := right.Schema
	if operator.Kind == "lookup" {
		omitted := rightJoinKeys(condition, rightAlias)
		outputRight = Schema{}
		for _, column := range right.Schema.Columns {
			if !omitted[column.Name] {
				outputRight.Columns = append(outputRight.Columns, column)
			}
		}
	}
	output := joinSchema(input.Schema, outputRight)
	if maximum := s.compiler.limits.MaxProjectionItems; maximum > 0 && !output.Unknown && len(output.Columns) > maximum {
		s.bindError("KQLB0344", operator.Span, "operator."+operator.Kind, fmt.Sprintf("join output exceeds MaxProjectionItems (%d)", maximum))
		return Relation{}
	}
	var projections []sqlast.SelectItem
	if !output.Unknown {
		projections = joinProjections(input.Schema, outputRight, output, leftAlias, rightAlias)
	}
	return Relation{Query: &sqlast.Select{From: join, Projections: projections}, Schema: output}
}

func (s *compileState) joinCondition(spec kql.JoinSpec, left, right Schema, leftAlias, rightAlias string, operator kql.Operator) sqlast.Expr {
	previousQualifiers, previousInput := s.qualifiers, s.input
	s.qualifiers = map[string]relationScope{
		"$left":  {alias: leftAlias, schema: left},
		"$right": {alias: rightAlias, schema: right},
	}
	s.input = Schema{Unknown: true}
	defer func() { s.qualifiers, s.input = previousQualifiers, previousInput }()
	leftScope := relationScope{alias: leftAlias, schema: left}
	rightScope := relationScope{alias: rightAlias, schema: right}
	var result sqlast.Expr
	keys := 0
	for _, expression := range spec.Conditions {
		condition := s.bindJoinCondition(expression, leftScope, rightScope, &keys)
		if condition == nil {
			return nil
		}
		result = andExpression(result, condition)
	}
	if spec.Condition != nil {
		condition := s.expression(spec.Condition)
		if condition.SQL == nil {
			return nil
		}
		if condition.Type != TypeBool {
			s.bindError("KQLB0310", spec.Condition.NodeSpan(), "operator.join", "join where requires a Boolean condition")
			return nil
		}
		result = andExpression(result, condition.SQL)
	}
	return result
}

func joinSchema(left, right Schema) Schema {
	if left.Unknown || right.Unknown {
		return Schema{Unknown: true}
	}
	result := left.Clone()
	used := nameSetFromColumns(result.Columns)
	for _, column := range right.Columns {
		base := column.Name
		if _, collision := used[strings.ToLower(column.Name)]; collision {
			for suffix := 1; ; suffix++ {
				candidate := base + strconv.Itoa(suffix)
				if _, exists := used[strings.ToLower(candidate)]; !exists {
					column.Name = candidate
					break
				}
			}
		}
		used[strings.ToLower(column.Name)] = struct{}{}
		result.Columns = append(result.Columns, column)
	}
	return result
}

func joinProjections(left, right, output Schema, leftAlias, rightAlias string) []sqlast.SelectItem {
	items := make([]sqlast.SelectItem, 0, len(output.Columns))
	for _, column := range left.Columns {
		items = append(items, sqlast.SelectItem{Expr: &sqlast.Identifier{Parts: []string{leftAlias, column.Name}}, Alias: column.Name})
	}
	used := nameSetFromColumns(left.Columns)
	for _, column := range right.Columns {
		alias := column.Name
		if _, collision := used[strings.ToLower(alias)]; collision {
			for suffix := 1; ; suffix++ {
				candidate := column.Name + strconv.Itoa(suffix)
				if _, exists := used[strings.ToLower(candidate)]; !exists {
					alias = candidate
					break
				}
			}
		}
		used[strings.ToLower(alias)] = struct{}{}
		items = append(items, sqlast.SelectItem{Expr: &sqlast.Identifier{Parts: []string{rightAlias, column.Name}}, Alias: alias})
	}
	return items
}
