package ksql

import (
	"fmt"
	"regexp"

	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func (s *compileState) search(from *sqlast.Subquery, input Schema, operator kql.Operator) Relation {
	if input.Unknown {
		s.bindError("KQLB0350", operator.Span, "operator.search", "search requires a bound input schema")
		return Relation{}
	}
	if maximum := s.compiler.limits.MaxProjectionItems; maximum > 0 && len(input.Columns) > maximum {
		s.bindError("KQLB0355", operator.Span, "operator.search", fmt.Sprintf("search input exceeds MaxProjectionItems (%d)", maximum))
		return Relation{}
	}
	spec, ok := operator.Body.(kql.SearchSpec)
	if !ok {
		s.error("KQLL0350", operator.Span, "operator.search", "unsupported search form")
		return Relation{}
	}
	literal, ok := spec.Term.(*kql.LiteralExpression)
	if !ok || literal.Kind != kql.StringToken {
		s.bindError("KQLB0351", spec.Term.NodeSpan(), "operator.search", "search currently requires one literal term")
		return Relation{}
	}
	term := stringLiteralValue(literal)
	if term == "" {
		s.bindError("KQLB0352", literal.Span, "operator.search", "search term must be non-empty")
		return Relation{}
	}
	leftBoundary, rightBoundary := s.termBoundaries()
	pattern := leftBoundary + regexp.QuoteMeta(term) + rightBoundary
	if s.compiler.limits.MaxRegexBytes > 0 && len(pattern) > s.compiler.limits.MaxRegexBytes {
		s.bindError("KQLB0353", literal.Span, "operator.search", fmt.Sprintf("search pattern exceeds MaxRegexBytes (%d)", s.compiler.limits.MaxRegexBytes))
		return Relation{}
	}
	if !supportsRegex(s.compiler.dialect) {
		s.error("KQLL0404", literal.Span, "operator.search", "search requires a term-match or regex-capable SQL dialect")
		return Relation{}
	}
	var predicate sqlast.Expr
	for _, column := range input.Columns {
		value := sqlast.Expr(&sqlast.Identifier{Parts: []string{column.Name}})
		if column.Type != TypeString && column.Type != TypeDynamic && column.Type != TypeUnknown {
			value = &sqlast.Cast{Expr: value, Type: "VARCHAR"}
		}
		match := &sqlast.Regex{Value: value, Pattern: s.bind(pattern), CaseSensitive: false}
		if predicate == nil {
			predicate = match
		} else {
			predicate = &sqlast.Binary{Left: predicate, Operator: "OR", Right: match}
		}
	}
	if predicate == nil {
		s.bindError("KQLB0354", operator.Span, "operator.search", "search input has no visible columns")
		return Relation{}
	}
	return Relation{Query: &sqlast.Select{From: from, Where: predicate}, Schema: input.Clone()}
}
