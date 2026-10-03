package ksql

import (
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func (s *compileState) projectItems(expressions []kql.Expression, inferAlias bool, input Schema, printStyle bool) ([]sqlast.SelectItem, Schema) {
	span := spanOfExpressions(expressions)
	if !s.checkProjectionWidth(len(expressions), span, "expression.projection") {
		return nil, Schema{}
	}
	items := make([]sqlast.SelectItem, 0, len(expressions))
	schema := Schema{}
	for expressionIndex, expression := range expressions {
		if _, star := expression.(*kql.StarExpression); star {
			if input.Unknown {
				items = append(items, sqlast.SelectItem{Expr: &sqlast.Star{}})
				schema.Unknown = true
				continue
			}
			if maximum := s.compiler.limits.MaxProjectionItems; maximum > 0 && len(input.Columns) > maximum-len(schema.Columns) {
				s.bindError("KQLB0344", expression.NodeSpan(), "expression.projection", fmt.Sprintf("output projection exceeds MaxProjectionItems (%d)", maximum))
				return nil, Schema{}
			}
			for _, column := range input.Columns {
				items = append(items, sqlast.SelectItem{Expr: &sqlast.Identifier{Parts: []string{column.Name}}, Alias: column.Name})
				schema.Columns = append(schema.Columns, column)
			}
			continue
		}
		item := sqlast.SelectItem{}
		bound := boundExpr{}
		if named, ok := expression.(*kql.NamedExpression); ok {
			bound = s.expression(named.Value)
			item.Expr = bound.SQL
			item.Alias = unquoteKQL(named.Name)
		} else {
			bound = s.expression(expression)
			item.Expr = bound.SQL
			if inferAlias {
				item.Alias = inferredName(expression, bound.Name)
			}
			if printStyle {
				item.Alias = fmt.Sprintf("print_%d", expressionIndex)
			}
		}
		if item.Expr != nil {
			if !s.checkProjectionWidth(len(schema.Columns)+1, expression.NodeSpan(), "expression.projection") {
				return nil, Schema{}
			}
			items = append(items, item)
			name := item.Alias
			if name == "" {
				name = bound.Name
			}
			if name != "" && item.Alias == "" {
				if _, identifier := item.Expr.(*sqlast.Identifier); !identifier {
					item.Alias = name
					items[len(items)-1].Alias = name
				}
			}
			if name == "" {
				name = s.defaultColumnName(expression.NodeSpan(), false)
				item.Alias = name
				items[len(items)-1].Alias = name
			}
			schema.Columns = append(schema.Columns, Column{Name: name, Type: bound.Type})
		}
	}
	s.validateUnique(schema.Columns, spanOfExpressions(expressions), "operator.project")
	return items, schema
}

func (s *compileState) extend(from *sqlast.Subquery, input Schema, expressions []kql.Expression, operator kql.Operator) Relation {
	if !s.checkProjectionLimit(expressions, operator) {
		return Relation{}
	}
	if input.Unknown {
		items, output := s.projectItems(expressions, true, input, false)
		return Relation{Query: &sqlast.Select{From: from, Projections: append([]sqlast.SelectItem{{Expr: &sqlast.Star{}}}, items...)}, Schema: Schema{Unknown: true, Columns: output.Columns}}
	}
	type extension struct {
		name  string
		value boundExpr
		span  kql.Span
	}
	values := make([]extension, 0, len(expressions))
	seen := make(map[string]struct{})
	for _, expression := range expressions {
		name := ""
		valueExpression := expression
		if named, ok := expression.(*kql.NamedExpression); ok {
			name, valueExpression = unquoteKQL(named.Name), named.Value
		}
		value := s.expression(valueExpression)
		if name == "" {
			name = inferredName(valueExpression, value.Name)
		}
		if name == "" {
			name = s.defaultColumnName(expression.NodeSpan(), true)
			if name == "" {
				continue
			}
		}
		key := strings.ToLower(name)
		if _, duplicate := seen[key]; duplicate {
			s.bindError("KQLB0406", expression.NodeSpan(), "operator.extend", "duplicate output column "+name)
			continue
		}
		seen[key] = struct{}{}
		values = append(values, extension{name: name, value: value, span: expression.NodeSpan()})
	}
	output := input.Clone()
	for _, extension := range values {
		if column, index, ok, ambiguous := output.Lookup(extension.name); ambiguous {
			s.bindError("KQLB0402", extension.span, "operator.extend", "ambiguous column "+extension.name)
		} else if ok {
			output.Columns[index] = Column{Name: column.Name, Type: extension.value.Type}
		} else {
			output.Columns = append(output.Columns, Column{Name: extension.name, Type: extension.value.Type})
		}
	}
	if !s.checkProjectionWidth(len(output.Columns), operator.Span, "operator.extend") {
		return Relation{}
	}
	items := make([]sqlast.SelectItem, 0, len(output.Columns))
	for _, column := range output.Columns {
		expr := s.columnIdentifier(column.Name)
		for _, extension := range values {
			if strings.EqualFold(column.Name, extension.name) {
				expr = extension.value.SQL
				break
			}
		}
		items = append(items, sqlast.SelectItem{Expr: expr, Alias: column.Name})
	}
	return Relation{Query: &sqlast.Select{From: from, Projections: items}, Schema: output}
}

func (s *compileState) projectionOperator(from *sqlast.Subquery, input Schema, operator kql.Operator) Relation {
	if input.Unknown {
		s.bindError("KQLB0340", operator.Span, "operator."+operator.Kind, operator.Kind+" requires a bound input schema")
		return Relation{}
	}
	spec := operator.Body.(kql.ExpressionSpec)
	if !s.checkProjectionLimit(spec.Expressions, operator) {
		return Relation{}
	}
	requested := make([]string, 0, len(spec.Expressions))
	renames := make(map[string]string)
	for _, expression := range spec.Expressions {
		if named, ok := expression.(*kql.NamedExpression); ok && operator.Kind == "project-rename" {
			old, ok := named.Value.(*kql.NameExpression)
			if !ok || (unquoteKQL(old.Name) == old.Name && strings.Contains(old.Name, ".")) {
				s.bindError("KQLB0341", expression.NodeSpan(), "operator."+operator.Kind, "project-rename requires new=old column names")
				continue
			}
			column, _, found, ambiguous := input.Lookup(unquoteKQL(old.Name))
			if ambiguous || !found {
				s.bindError("KQLB0401", old.Span, "operator."+operator.Kind, "unknown column "+old.Name)
				continue
			}
			renames[strings.ToLower(column.Name)] = unquoteKQL(named.Name)
			continue
		}
		if pattern, ok := expression.(*kql.ColumnPatternExpression); ok {
			matches := matchingColumns(input.Columns, pattern.Pattern, pattern.Order)
			requested = append(requested, matches...)
			continue
		}
		name, ok := expression.(*kql.NameExpression)
		if !ok {
			s.bindError("KQLB0342", expression.NodeSpan(), "operator."+operator.Kind, "expected a bound column name or pattern")
			continue
		}
		column, _, found, ambiguous := input.Lookup(unquoteKQL(name.Name))
		if ambiguous || !found {
			s.bindError("KQLB0401", name.Span, "operator."+operator.Kind, "unknown column "+name.Name)
			continue
		}
		requested = append(requested, column.Name)
	}
	output := Schema{}
	switch operator.Kind {
	case "project-away":
		removed := nameSet(requested)
		for _, column := range input.Columns {
			if _, ok := removed[strings.ToLower(column.Name)]; !ok {
				output.Columns = append(output.Columns, column)
			}
		}
	case "project-keep":
		kept := nameSet(requested)
		for _, column := range input.Columns {
			if _, ok := kept[strings.ToLower(column.Name)]; ok {
				output.Columns = append(output.Columns, column)
			}
		}
	case "project-reorder":
		used := make(map[string]struct{})
		for _, name := range requested {
			key := strings.ToLower(name)
			if _, found := used[key]; found {
				continue
			}
			used[key] = struct{}{}
			column, _, _, _ := input.Lookup(name)
			output.Columns = append(output.Columns, column)
		}
		for _, column := range input.Columns {
			if _, ok := used[strings.ToLower(column.Name)]; !ok {
				output.Columns = append(output.Columns, column)
			}
		}
	case "project-rename":
		for _, column := range input.Columns {
			if replacement, ok := renames[strings.ToLower(column.Name)]; ok {
				column.Name = replacement
			}
			output.Columns = append(output.Columns, column)
		}
	}
	if len(output.Columns) == 0 {
		s.bindError("KQLB0343", operator.Span, "operator."+operator.Kind, "projection cannot remove every column")
		return Relation{}
	}
	if maximum := s.compiler.limits.MaxProjectionItems; maximum > 0 && len(output.Columns) > maximum {
		s.bindError("KQLB0344", operator.Span, "operator."+operator.Kind, fmt.Sprintf("expanded projection exceeds MaxProjectionItems (%d)", maximum))
		return Relation{}
	}
	s.validateUnique(output.Columns, operator.Span, "operator."+operator.Kind)
	items := make([]sqlast.SelectItem, 0, len(output.Columns))
	for i, column := range output.Columns {
		sourceName := column.Name
		if operator.Kind == "project-rename" {
			sourceName = input.Columns[i].Name
		}
		items = append(items, sqlast.SelectItem{Expr: &sqlast.Identifier{Parts: []string{sourceName}}, Alias: column.Name})
	}
	return Relation{Query: &sqlast.Select{From: from, Projections: items}, Schema: output}
}
