package ksql

import (
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func (s *compileState) mvExpand(input Relation, spec kql.MvExpandSpec, operator kql.Operator) Relation {
	return s.expandJSON(input, spec.Items, spec.ItemIndex, spec.Limit, spec.Kind, operator)
}

func (s *compileState) mvApply(input Relation, spec kql.MvApplySpec, operator kql.Operator) Relation {
	relation := s.expandJSON(input, spec.Items, spec.ItemIndex, spec.Limit, "bag", operator)
	if relation.Query == nil {
		return Relation{}
	}
	for _, inner := range spec.Operators {
		switch inner.Kind {
		case "where", "extend", "serialize":
			relation = s.operator(relation, inner)
		default:
			s.error("KQLL0334", inner.Span, "operator.mv-apply."+inner.Kind, "SQLite mv-apply currently supports where, extend, and serialize subqueries")
			return Relation{}
		}
		if relation.Query == nil {
			return Relation{}
		}
	}
	return relation
}

func (s *compileState) expandJSON(input Relation, items []kql.MvExpansion, itemIndex string, limit kql.Expression, expansionKind string, operator kql.Operator) Relation {
	if !supportsJSONEach(s.compiler.dialect) {
		s.error("KQLL0330", operator.Span, "operator."+operator.Kind, "dialect "+s.compiler.dialect.Name()+" has no JSON array expansion")
		return Relation{}
	}
	if s.compiler.limits.MaxExpansionItems > 0 && len(items) > s.compiler.limits.MaxExpansionItems {
		s.bindError("KQLB0337", operator.Span, "operator."+operator.Kind, fmt.Sprintf("expansion exceeds MaxExpansionItems (%d)", s.compiler.limits.MaxExpansionItems))
		return Relation{}
	}
	if len(items) != 1 {
		s.error("KQLL0331", operator.Span, "operator."+operator.Kind, operator.Kind+" currently requires exactly one array expression")
		return Relation{}
	}
	item := items[0]
	sourceName := ""
	if name, ok := item.Expression.(*kql.NameExpression); ok {
		sourceName = name.Name
	}
	if item.Name == "" {
		s.error("KQLL0332", item.Span, "operator."+operator.Kind, operator.Kind+" requires an explicit output name for non-column expressions")
		return Relation{}
	}
	if input.Schema.Unknown && strings.EqualFold(item.Name, sourceName) {
		s.error("KQLL0333", item.Span, "operator."+operator.Kind, "column replacement requires a bound input schema")
		return Relation{}
	}

	inputAlias, expansionAlias := s.nextAlias(), s.nextAlias()
	previousQualifier := s.qualifier
	s.qualifier = inputAlias
	previousInput := s.input
	s.input = input.Schema
	value := s.expression(item.Expression)
	s.input = previousInput
	s.qualifier = previousQualifier
	if value.Type != TypeDynamic && value.Type != TypeUnknown {
		s.bindError("KQLB0338", item.Span, "operator."+operator.Kind, "expanded expression must be dynamic")
		return Relation{}
	}
	expansion := &sqlast.JSONEach{Value: value.SQL, Alias: expansionAlias}
	from := &sqlast.Join{
		Left:  &sqlast.Subquery{Query: input.Query, Alias: inputAlias},
		Right: expansion,
		Kind:  "CROSS",
	}
	element := sqlast.Expr(&sqlast.Identifier{Parts: []string{expansionAlias, "value"}})
	expansionType := sqlast.Expr(&sqlast.Identifier{Parts: []string{expansionAlias, "type"}})
	isNestedJSON := &sqlast.Binary{
		Left:     &sqlast.Binary{Left: expansionType, Operator: "=", Right: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "object"}},
		Operator: "OR",
		Right:    &sqlast.Binary{Left: expansionType, Operator: "=", Right: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "array"}},
	}
	jsonValue := sqlast.Expr(&sqlast.Case{Branches: []sqlast.When{{Condition: isNestedJSON, Result: &sqlast.Call{Name: "json", Args: []sqlast.Expr{element}}}}, Else: element})
	isObject := &sqlast.Binary{Left: &sqlast.Call{Name: "json_type", Args: []sqlast.Expr{value.SQL}}, Operator: "=", Right: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "object"}}
	objectElement := sqlast.Expr(&sqlast.Call{Name: "json_object", Args: []sqlast.Expr{&sqlast.Identifier{Parts: []string{expansionAlias, "key"}}, jsonValue}})
	if expansionKind == "array" {
		objectElement = &sqlast.Call{Name: "json_array", Args: []sqlast.Expr{&sqlast.Identifier{Parts: []string{expansionAlias, "key"}}, jsonValue}}
	}
	element = &sqlast.Case{Branches: []sqlast.When{{Condition: isObject, Result: objectElement}}, Else: element}
	if item.Type != "" {
		types := map[string]string{
			"long": "BIGINT", "int": "INTEGER", "real": "DOUBLE PRECISION", "double": "DOUBLE PRECISION",
			"decimal": "DECIMAL", "string": "VARCHAR", "bool": "BOOLEAN", "boolean": "BOOLEAN",
			"datetime": "TIMESTAMP", "guid": "UUID",
		}
		target, ok := types[item.Type]
		if !ok {
			s.error("KQLL0335", item.Span, "operator."+operator.Kind, "unsupported expansion type "+item.Type)
			return Relation{}
		}
		s.warn("KQLL0336", item.Span, "operator."+operator.Kind, "SQLite casts invalid expanded values instead of returning null")
		element = &sqlast.Cast{Expr: element, Type: target}
	}
	output := input.Schema.Clone()
	columnType := TypeDynamic
	if item.Type != "" {
		columnType = parseScalarType(item.Type)
	}
	_, replaceAt, replacing, _ := output.Lookup(item.Name)
	if replacing {
		output.Columns[replaceAt] = Column{Name: output.Columns[replaceAt].Name, Type: columnType}
	} else {
		output.Columns = append(output.Columns, Column{Name: item.Name, Type: columnType})
	}
	projections := make([]sqlast.SelectItem, 0, len(output.Columns)+1)
	if output.Unknown {
		projections = append(projections, sqlast.SelectItem{Expr: &sqlast.QualifiedStar{Parts: []string{inputAlias}}})
		projections = append(projections, sqlast.SelectItem{Expr: element, Alias: item.Name})
	} else {
		for _, column := range input.Schema.Columns {
			expr := sqlast.Expr(&sqlast.Identifier{Parts: []string{inputAlias, column.Name}})
			if strings.EqualFold(column.Name, item.Name) {
				expr = element
			}
			projections = append(projections, sqlast.SelectItem{Expr: expr, Alias: column.Name})
		}
		if !replacing {
			projections = append(projections, sqlast.SelectItem{Expr: element, Alias: item.Name})
		}
	}
	if itemIndex != "" {
		projections = append(projections, sqlast.SelectItem{
			Expr: &sqlast.Case{Branches: []sqlast.When{{
				Condition: &sqlast.Binary{Left: &sqlast.Call{Name: "json_type", Args: []sqlast.Expr{value.SQL}}, Operator: "=", Right: &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "array"}},
				Result:    &sqlast.Cast{Expr: &sqlast.Identifier{Parts: []string{expansionAlias, "key"}}, Type: "INTEGER"},
			}}, Else: &sqlast.Literal{Kind: sqlast.NullLiteral}},
			Alias: itemIndex,
		})
		output.Columns = append(output.Columns, Column{Name: itemIndex, Type: TypeLong})
	}
	query := &sqlast.Select{From: from, Projections: projections}
	if limit != nil {
		query.Where = &sqlast.Binary{
			Left:     &sqlast.Cast{Expr: &sqlast.Identifier{Parts: []string{expansionAlias, "key"}}, Type: "INTEGER"},
			Operator: "<",
			Right:    s.expression(limit).SQL,
		}
	}
	return Relation{Query: query, Schema: output}
}

func supportsJSONEach(target dialect.Dialect) bool {
	capability, ok := target.(interface {
		JSONEach(string, string) (string, bool)
	})
	if !ok {
		return false
	}
	_, ok = capability.JSONEach("", "")
	return ok
}
