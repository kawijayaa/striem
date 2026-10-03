package ksql

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kawijayaa/ksql/kql"
)

func (s *compileState) validateUnique(columns []Column, span kql.Span, feature string) bool {
	seen := make(map[string]struct{}, len(columns))
	valid := true
	for _, column := range columns {
		key := strings.ToLower(column.Name)
		if _, duplicate := seen[key]; duplicate {
			s.bindError("KQLB0406", span, feature, "duplicate output column "+column.Name)
			valid = false
		}
		seen[key] = struct{}{}
	}
	return valid
}

func (s *compileState) checkNonDynamicColumns(columns []Column, span kql.Span, feature string) bool {
	valid := true
	for _, column := range columns {
		if column.Type == TypeDynamic {
			s.bindError("KQLB0303", span, feature, "dynamic column "+column.Name+" requires an explicit scalar cast for "+strings.TrimPrefix(feature, "operator."))
			valid = false
		}
	}
	return valid
}

func (s *compileState) checkProjectionWidth(count int, span kql.Span, feature string) bool {
	limit := s.compiler.limits.MaxProjectionItems
	if limit > 0 && count > limit {
		s.bindError("KQLB0344", span, feature, fmt.Sprintf("output projection exceeds MaxProjectionItems (%d)", limit))
		return false
	}
	return true
}

func (s *compileState) checkProjectionLimit(expressions []kql.Expression, operator kql.Operator) bool {
	limit := s.compiler.limits.MaxProjectionItems
	if limit > 0 && len(expressions) > limit {
		s.bindError("KQLB0344", operator.Span, "operator."+operator.Kind, fmt.Sprintf("projection exceeds MaxProjectionItems (%d)", limit))
		return false
	}
	return true
}

func (s *compileState) validateLimit(expression kql.Expression, operator kql.Operator) bool {
	literal, ok := expression.(*kql.LiteralExpression)
	if !ok || literal.Kind != kql.NumberToken || strings.ContainsAny(literal.Text, ".eE") {
		s.bindError("KQLB0302", expression.NodeSpan(), "operator."+operator.Kind, "row limit must be a constant integer")
		return false
	}
	value, err := strconv.ParseInt(literal.Text, 10, 64)
	if err != nil || value < 0 {
		s.bindError("KQLB0302", expression.NodeSpan(), "operator."+operator.Kind, "row limit must be a non-negative bounded integer")
		return false
	}
	if maximum := s.compiler.limits.MaxOutputRows; maximum > 0 && value > int64(maximum) {
		s.bindError("KQLB0303", expression.NodeSpan(), "operator."+operator.Kind, fmt.Sprintf("row limit exceeds MaxOutputRows (%d)", maximum))
		return false
	}
	return true
}

func (s *compileState) bindError(code string, span kql.Span, feature, message string) {
	s.diagnostics = append(s.diagnostics, Diagnostic{Code: code, Severity: SeverityError, Phase: PhaseBind, Message: message, Span: span, Feature: feature})
}

func spanOfExpressions(expressions []kql.Expression) kql.Span {
	if len(expressions) == 0 {
		return kql.Span{}
	}
	first, last := expressions[0].NodeSpan(), expressions[len(expressions)-1].NodeSpan()
	return kql.Span{Offset: first.Offset, Length: last.Offset + last.Length - first.Offset, Line: first.Line, Column: first.Column}
}
