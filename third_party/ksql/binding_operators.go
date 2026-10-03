package ksql

import (
	"strings"
	"unicode"

	"github.com/kawijayaa/ksql/dialect"
	"github.com/kawijayaa/ksql/kql"
	"github.com/kawijayaa/ksql/sqlast"
)

func supportsRegex(target dialect.Dialect) bool {
	_, ok := target.Regex("", "", true)
	return ok
}

func (s *compileState) binary(value *kql.BinaryExpression) boundExpr {
	op := strings.ToLower(value.Operator)
	left := s.expression(value.Left)
	right := boundExpr{}
	if !isTermOperator(op) {
		right = s.expression(value.Right)
	}
	s.use(scalarOperatorID(op), "equivalent", value.Span)
	boolean := func(expression sqlast.Expr) boundExpr { return boundExpr{SQL: expression, Type: TypeBool} }
	if isNullExpression(right.SQL) && (op == "==" || op == "!=" || op == "<>") {
		operator := "IS"
		if op != "==" {
			operator = "IS NOT"
		}
		return boolean(&sqlast.Binary{Left: left.SQL, Operator: operator, Right: right.SQL})
	}
	if isNullExpression(left.SQL) && (op == "==" || op == "!=" || op == "<>") {
		operator := "IS"
		if op != "==" {
			operator = "IS NOT"
		}
		return boolean(&sqlast.Binary{Left: right.SQL, Operator: operator, Right: left.SQL})
	}
	switch op {
	case "==":
		op = "="
	case "!=", "<>":
		op = "<>"
	case "and", "or":
		op = strings.ToUpper(op)
	case "=~", "!~":
		comparison := "="
		if op == "!~" {
			comparison = "<>"
		}
		return boolean(&sqlast.Binary{Left: &sqlast.Call{Name: "lower", Args: []sqlast.Expr{left.SQL}}, Operator: comparison, Right: &sqlast.Call{Name: "lower", Args: []sqlast.Expr{right.SQL}}})
	case "in", "!in":
		if op == "!in" {
			op = "NOT IN"
		} else {
			op = "IN"
		}
	case "in~", "!in~":
		list, ok := right.SQL.(*sqlast.List)
		if !ok || len(list.Items) == 0 {
			s.error("KQLL0403", value.Span, "operator.scalar."+op, op+" requires a non-empty scalar value list")
			return boundExpr{}
		}
		items := make([]sqlast.Expr, len(list.Items))
		for i, item := range list.Items {
			items[i] = &sqlast.Call{Name: "lower", Args: []sqlast.Expr{item}}
		}
		operator := "IN"
		if op == "!in~" {
			operator = "NOT IN"
		}
		return boolean(&sqlast.Binary{Left: &sqlast.Call{Name: "lower", Args: []sqlast.Expr{left.SQL}}, Operator: operator, Right: &sqlast.List{Items: items}})
	case "between", "!between":
		list, ok := right.SQL.(*sqlast.List)
		if !ok || len(list.Items) != 2 {
			return boundExpr{}
		}
		between := &sqlast.Binary{Left: left.SQL, Operator: "BETWEEN", Right: &sqlast.Binary{Left: list.Items[0], Operator: "AND", Right: list.Items[1]}}
		if op == "!between" {
			return boolean(&sqlast.Unary{Operator: "NOT ", Operand: between})
		}
		return boolean(between)
	case "contains", "contains_cs", "containscs", "!contains", "!contains_cs", "notcontains", "notcontainscs":
		caseSensitive := strings.Contains(op, "_cs") || strings.HasSuffix(op, "cs")
		negative := strings.HasPrefix(op, "!") || strings.HasPrefix(op, "not")
		return boolean(stringMatch(left.SQL, right.SQL, "both", caseSensitive, negative))
	case "startswith", "startswith_cs", "!startswith", "!startswith_cs":
		return boolean(stringMatch(left.SQL, right.SQL, "suffix", strings.Contains(op, "_cs"), strings.HasPrefix(op, "!")))
	case "endswith", "endswith_cs", "!endswith", "!endswith_cs":
		return boolean(stringMatch(left.SQL, right.SQL, "prefix", strings.Contains(op, "_cs"), strings.HasPrefix(op, "!")))
	case "matches regex":
		return boolean(&sqlast.Regex{Value: left.SQL, Pattern: right.SQL, CaseSensitive: true})
	case "has", "!has", "has_cs", "!has_cs", "hasprefix", "!hasprefix", "hasprefix_cs", "!hasprefix_cs", "hassuffix", "!hassuffix", "hassuffix_cs", "!hassuffix_cs":
		if !supportsRegex(s.compiler.dialect) {
			s.error("KQLL0404", value.Span, "operator.scalar."+op, op+" requires a regex-capable SQL dialect")
			return boundExpr{}
		}
		mode := "term"
		if strings.Contains(op, "prefix") {
			mode = "prefix"
		} else if strings.Contains(op, "suffix") {
			mode = "suffix"
		}
		result, ok := s.termMatchExpression(left.SQL, value.Right, mode, strings.Contains(op, "_cs"), strings.HasPrefix(op, "!"))
		if !ok {
			s.error("KQLL0404", value.Span, "operator.scalar."+op, op+" currently requires one non-empty alphanumeric literal term")
			return boundExpr{}
		}
		return boolean(result)
	case "has_any", "has_all":
		if !supportsRegex(s.compiler.dialect) {
			s.error("KQLL0404", value.Span, "operator.scalar."+op, op+" requires a regex-capable SQL dialect")
			return boundExpr{}
		}
		list, ok := value.Right.(*kql.ListExpression)
		if !ok || len(list.Items) == 0 {
			s.error("KQLL0404", value.Span, "operator.scalar."+op, op+" requires a non-empty literal term list")
			return boundExpr{}
		}
		operator := "OR"
		if op == "has_all" {
			operator = "AND"
		}
		var result sqlast.Expr
		for _, item := range list.Items {
			match, matched := s.termMatchExpression(left.SQL, item, "term", false, false)
			if !matched {
				s.error("KQLL0404", value.Span, "operator.scalar."+op, op+" currently requires non-empty alphanumeric literal terms")
				return boundExpr{}
			}
			if result == nil {
				result = match
			} else {
				result = &sqlast.Binary{Left: result, Operator: operator, Right: match}
			}
		}
		return boolean(result)
	case ":":
		s.error("KQLL0404", value.Span, "operator.scalar.colon", ": search semantics require input schema information")
		return boundExpr{}
	}
	resultType := left.Type
	if op == "=" || op == "<>" || op == "<" || op == "<=" || op == ">" || op == ">=" || op == "AND" || op == "OR" || op == "IN" || op == "NOT IN" {
		resultType = TypeBool
	} else if left.Type == TypeReal || right.Type == TypeReal {
		resultType = TypeReal
	} else if left.Type == TypeLong || right.Type == TypeLong {
		resultType = TypeLong
	}
	return boundExpr{SQL: &sqlast.Binary{Left: left.SQL, Operator: op, Right: right.SQL}, Type: resultType}
}

func isTermOperator(operator string) bool {
	switch operator {
	case "has", "!has", "has_cs", "!has_cs", "hasprefix", "!hasprefix", "hasprefix_cs", "!hasprefix_cs", "hassuffix", "!hassuffix", "hassuffix_cs", "!hassuffix_cs", "has_any", "has_all":
		return true
	default:
		return false
	}
}

func isNullExpression(expression sqlast.Expr) bool {
	literal, ok := expression.(*sqlast.Literal)
	return ok && literal.Kind == sqlast.NullLiteral
}

func (s *compileState) termMatchExpression(left sqlast.Expr, expression kql.Expression, mode string, caseSensitive, negative bool) (sqlast.Expr, bool) {
	literal, ok := expression.(*kql.LiteralExpression)
	if !ok || literal.Kind != kql.StringToken {
		return nil, false
	}
	term := stringLiteralValue(literal)
	if !isAlphaNumeric(term) {
		return nil, false
	}
	leftBoundary, rightBoundary := s.termBoundaries()
	pattern := leftBoundary + term + rightBoundary
	switch mode {
	case "prefix":
		pattern = leftBoundary + term + `[[:alnum:]]*`
	case "suffix":
		pattern = leftBoundary + `[[:alnum:]]*` + term + rightBoundary
	}
	result := sqlast.Expr(&sqlast.Regex{Value: left, Pattern: s.bind(pattern), CaseSensitive: caseSensitive})
	if negative {
		result = &sqlast.Unary{Operator: "NOT ", Operand: result}
	}
	return result, true
}

func (s *compileState) termBoundaries() (string, string) {
	if s.compiler.dialect.Name() == "sqlite" {
		return `(^|[^A-Za-z0-9])`, `([^A-Za-z0-9]|$)`
	}
	return `(^|[^[:alnum:]])`, `([^[:alnum:]]|$)`
}

func stringMatch(left, right sqlast.Expr, wildcard string, caseSensitive, negative bool) sqlast.Expr {
	patternArgs := []sqlast.Expr{}
	if wildcard == "both" || wildcard == "prefix" {
		patternArgs = append(patternArgs, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "%"})
	}
	patternArgs = append(patternArgs, right)
	if wildcard == "both" || wildcard == "suffix" {
		patternArgs = append(patternArgs, &sqlast.Literal{Kind: sqlast.StringLiteral, Value: "%"})
	}
	pattern := sqlast.Expr(&sqlast.Call{Name: "concat", Args: patternArgs})
	if !caseSensitive {
		left = &sqlast.Call{Name: "lower", Args: []sqlast.Expr{left}}
		pattern = &sqlast.Call{Name: "lower", Args: []sqlast.Expr{pattern}}
	}
	op := "LIKE"
	if negative {
		op = "NOT LIKE"
	}
	return &sqlast.Binary{Left: left, Operator: op, Right: pattern}
}

func termMatch(left, right sqlast.Expr, mode string, caseSensitive, negative bool) (sqlast.Expr, bool) {
	literal, ok := right.(*sqlast.Literal)
	if !ok || literal.Kind != sqlast.StringLiteral || literal.Value == "" {
		return nil, false
	}
	for _, r := range literal.Value {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return nil, false
		}
	}
	term := literal.Value
	leftBoundary, rightBoundary := `(^|[^[:alnum:]])`, `([^[:alnum:]]|$)`
	pattern := leftBoundary + term + rightBoundary
	switch mode {
	case "prefix":
		pattern = leftBoundary + term + `[[:alnum:]]*`
	case "suffix":
		pattern = leftBoundary + `[[:alnum:]]*` + term + rightBoundary
	}
	result := sqlast.Expr(&sqlast.Regex{
		Value:         left,
		Pattern:       &sqlast.Literal{Kind: sqlast.StringLiteral, Value: pattern},
		CaseSensitive: caseSensitive,
	})
	if negative {
		result = &sqlast.Unary{Operator: "NOT ", Operand: result}
	}
	return result, true
}

func scalarOperatorID(operator string) string {
	names := map[string]string{
		"+": "add", "-": "subtract", "*": "multiply", "/": "divide", "%": "modulo",
		"<": "less_than", "<=": "less_than_or_equal", ">": "greater_than", ">=": "greater_than_or_equal",
		"==": "equal", "!=": "not_equal", "<>": "not_equal", "=~": "equal_tilde", "!~": "bang_tilde",
		"!has": "not_has", "!has_cs": "not_has_cs", "!hasprefix": "not_hasprefix",
		"!hasprefix_cs": "not_hasprefix_cs", "!hassuffix": "not_hassuffix",
		"!hassuffix_cs": "not_hassuffix_cs", "!contains": "not_contains",
		"!contains_cs": "not_contains_cs", "!startswith": "not_startswith",
		"!startswith_cs": "not_startswith_cs", "!endswith": "not_endswith",
		"!endswith_cs": "not_endswith_cs", "matches regex": "matches_regex",
		"!in": "not_in", "in~": "in_cs", "!in~": "not_in_cs", "!between": "not_between",
	}
	if name, ok := names[operator]; ok {
		return "operator.scalar." + name
	}
	return "operator.scalar." + strings.ReplaceAll(operator, " ", "_")
}

func andExpression(left, right sqlast.Expr) sqlast.Expr {
	if left == nil {
		return right
	}
	if right == nil {
		return left
	}
	return &sqlast.Binary{Left: left, Operator: "AND", Right: right}
}

func isAlphaNumeric(value string) bool {
	for _, r := range value {
		if !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return false
		}
	}
	return value != ""
}
