package kql

import "strings"

func parseColumnSelection(op Operator, tokens []Token) (Operator, []ParseError) {
	var spec ExpressionSpec
	var errs []ParseError
	if len(tokens) == 0 && op.Kind == "project-reorder" {
		op.Body = spec
		return op, nil
	}
	for _, part := range splitTopLevel(tokens, ",") {
		span := tokensSpan(part)
		order := ""
		if len(part) > 1 && op.Kind == "project-reorder" {
			switch lower(part[len(part)-1]) {
			case "asc", "desc", "granny-asc", "granny-desc":
				order = lower(part[len(part)-1])
				part = part[:len(part)-1]
			}
		}
		// A wildcard identifier is a contiguous sequence, not multiplication.
		var patternText strings.Builder
		valid := len(part) > 0
		for i, token := range part {
			if token.Kind != IdentifierToken && token.Kind != NumberToken && token.Text != "*" {
				valid = false
			}
			if i > 0 && part[i-1].Span.Offset+part[i-1].Span.Length != token.Span.Offset {
				valid = false
			}
			patternText.WriteString(token.Text)
		}
		pattern := patternText.String()
		if valid && strings.Contains(pattern, "*") {
			spec.Expressions = append(spec.Expressions, &ColumnPatternExpression{Pattern: pattern, Order: order, Span: span})
			continue
		}
		expression, expressionErrors := parseExpressionTokens(part)
		errs = append(errs, expressionErrors...)
		if _, ok := expression.(*NameExpression); !ok && len(expressionErrors) == 0 {
			errs = append(errs, ParseError{Code: "KQLP0340", Message: "expected column name or wildcard pattern", Span: span})
		}
		// Ordering an exact column cannot change its position within its match set.
		spec.Expressions = append(spec.Expressions, expression)
	}
	op.Body = spec
	return op, errs
}
