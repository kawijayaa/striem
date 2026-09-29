package kql

import "strings"

var binaryPrecedence = map[string]int{
	"or": 1, "and": 2,
	"==": 3, "!=": 3, "<>": 3, "=~": 3, "!~": 3,
	"in": 3, "in~": 3, "!in": 3, "!in~": 3, "between": 3,
	"!between": 3, "has_any": 3, "has_all": 3,
	"<": 4, "<=": 4, ">": 4, ">=": 4,
	"+": 5, "-": 5,
	"*": 6, "/": 6, "%": 6,
	"has": 7, "!has": 7, "has_cs": 7, "!has_cs": 7,
	"hasprefix": 7, "!hasprefix": 7, "hasprefix_cs": 7, "!hasprefix_cs": 7,
	"hassuffix": 7, "!hassuffix": 7, "hassuffix_cs": 7, "!hassuffix_cs": 7,
	"contains": 7, "!contains": 7, "contains_cs": 7, "!contains_cs": 7,
	"containscs": 7, "notcontains": 7, "notcontainscs": 7,
	"startswith": 7, "!startswith": 7, "startswith_cs": 7, "!startswith_cs": 7,
	"endswith": 7, "!endswith": 7, "endswith_cs": 7, "!endswith_cs": 7,
	"like": 7, "notlike": 7, "likecs": 7, "notlikecs": 7,
	"matches regex": 7, ":": 7,
}

func parseExpressionTokens(tokens []Token) (Expression, []ParseError) {
	if len(tokens) == 0 {
		return &RawExpression{}, []ParseError{{Code: "KQLP0401", Message: "expected expression"}}
	}
	p := expressionParser{tokens: tokens}
	expr := p.parse(0)
	if expr == nil {
		expr = &RawExpression{Tokens: tokens, Span: tokensSpan(tokens)}
	}
	if p.position < len(tokens) {
		p.errors = append(p.errors, ParseError{Code: "KQLP0402", Message: "unexpected token " + tokens[p.position].Text, Span: tokens[p.position].Span})
	}
	return expr, p.errors
}

type expressionParser struct {
	tokens   []Token
	position int
	errors   []ParseError
}

func (p *expressionParser) parse(minPrecedence int) Expression {
	left := p.parsePrefix()
	if left == nil {
		return nil
	}
	for p.position < len(p.tokens) {
		op, width := p.binaryOperator()
		precedence, ok := binaryPrecedence[op]
		if !ok || precedence < minPrecedence {
			break
		}
		p.position += width

		var right Expression
		if isListOperator(op) && p.match("(") {
			right = p.parseList()
		} else if (op == "between" || op == "!between") && p.match("(") {
			right = p.parseRangeList()
		} else {
			right = p.parse(precedence + 1)
		}
		if right == nil {
			p.errors = append(p.errors, ParseError{Code: "KQLP0403", Message: "missing right operand for " + op, Span: left.NodeSpan()})
			return left
		}
		left = &BinaryExpression{Left: left, Operator: op, Right: right, Span: joinSpan(left.NodeSpan(), right.NodeSpan())}
	}
	return left
}

func (p *expressionParser) parsePrefix() Expression {
	if p.position >= len(p.tokens) {
		return nil
	}
	token := p.tokens[p.position]
	if token.Text == "+" || token.Text == "-" {
		p.position++
		operand := p.parse(8)
		if operand == nil {
			return nil
		}
		return &UnaryExpression{Operator: token.Text, Operand: operand, Span: joinSpan(token.Span, operand.NodeSpan())}
	}

	var expr Expression
	switch {
	case token.Text == "*":
		p.position++
		expr = &StarExpression{Span: token.Span}
	case token.Kind == NumberToken || token.Kind == StringToken || strings.EqualFold(token.Text, "true") || strings.EqualFold(token.Text, "false") || strings.EqualFold(token.Text, "null"):
		p.position++
		expr = &LiteralExpression{Text: token.Text, Kind: token.Kind, Span: token.Span}
	case token.Text == "(":
		p.position++
		expr = p.parse(0)
		if !p.consume(")") {
			p.errors = append(p.errors, ParseError{Code: "KQLP0404", Message: "missing closing parenthesis", Span: token.Span})
		}
	case token.Text == "[" && p.position+2 < len(p.tokens) && p.tokens[p.position+1].Kind == StringToken && p.tokens[p.position+2].Text == "]":
		nameTokens := p.tokens[p.position : p.position+3]
		p.position += 3
		expr = &NameExpression{Name: nameTokens[1].Text, Span: tokensSpan(nameTokens)}
	case token.Text == "{":
		start := p.position
		for p.position < len(p.tokens) && p.tokens[p.position].Text != "}" {
			p.position++
		}
		p.consume("}")
		raw := p.tokens[start:p.position]
		expr = &RawExpression{Tokens: raw, Span: tokensSpan(raw)}
	case token.Kind == IdentifierToken:
		p.position++
		expr = &NameExpression{Name: token.Text, Span: token.Span}
	default:
		p.errors = append(p.errors, ParseError{Code: "KQLP0405", Message: "expected expression", Span: token.Span})
		p.position++
		return nil
	}

	for p.position < len(p.tokens) {
		switch p.tokens[p.position].Text {
		case "(":
			start := expr.NodeSpan()
			p.position++
			args := p.parseCommaExpressions(")")
			end := start
			if p.position > 0 {
				end = p.tokens[p.position-1].Span
			}
			expr = &CallExpression{Function: expr, Args: args, Span: joinSpan(start, end)}
		case "[":
			start := expr.NodeSpan()
			p.position++
			index := p.parse(0)
			if !p.consume("]") {
				p.errors = append(p.errors, ParseError{Code: "KQLP0406", Message: "missing closing bracket", Span: start})
			}
			if index != nil {
				expr = &IndexExpression{Value: expr, Index: index, Span: joinSpan(start, index.NodeSpan())}
			}
		case ".":
			if p.position+1 >= len(p.tokens) || p.tokens[p.position+1].Kind != IdentifierToken {
				return expr
			}
			name := p.tokens[p.position+1]
			p.position += 2
			leftName, ok := expr.(*NameExpression)
			if !ok {
				return expr
			}
			expr = &NameExpression{Name: leftName.Name + "." + name.Text, Span: joinSpan(leftName.Span, name.Span)}
		default:
			return expr
		}
	}
	return expr
}

func (p *expressionParser) parseCommaExpressions(close string) []Expression {
	var expressions []Expression
	if p.consume(close) {
		return expressions
	}
	for p.position < len(p.tokens) {
		start := p.position
		expr := p.parse(0)
		if expr != nil {
			if equal := p.position; equal < len(p.tokens) && p.tokens[equal].Text == "=" && start+1 == equal {
				if name, ok := expr.(*NameExpression); ok {
					p.position++
					value := p.parse(0)
					expr = &NamedExpression{Name: name.Name, Value: value, Span: joinSpan(name.Span, value.NodeSpan())}
				}
			}
			expressions = append(expressions, expr)
		}
		if p.consume(close) {
			return expressions
		}
		if !p.consume(",") {
			break
		}
	}
	if !p.consume(close) {
		p.errors = append(p.errors, ParseError{Code: "KQLP0407", Message: "missing " + close, Span: p.tokens[len(p.tokens)-1].Span})
	}
	return expressions
}

func (p *expressionParser) parseList() Expression {
	open := p.tokens[p.position-1].Span
	items := p.parseCommaExpressions(")")
	span := open
	if len(items) > 0 {
		span = joinSpan(open, items[len(items)-1].NodeSpan())
	}
	return &ListExpression{Items: items, Span: span}
}

func (p *expressionParser) parseRangeList() Expression {
	open := p.tokens[p.position-1].Span
	left := p.parse(0)
	if !p.consume("..") {
		p.errors = append(p.errors, ParseError{Code: "KQLP0408", Message: "expected '..' in range", Span: open})
	}
	right := p.parse(0)
	p.consume(")")
	items := []Expression{left, right}
	return &ListExpression{Items: items, Span: joinSpan(open, right.NodeSpan())}
}

func (p *expressionParser) binaryOperator() (string, int) {
	if p.position >= len(p.tokens) {
		return "", 0
	}
	op := strings.ToLower(p.tokens[p.position].Text)
	if op == "matches" && p.position+1 < len(p.tokens) && strings.EqualFold(p.tokens[p.position+1].Text, "regex") {
		return "matches regex", 2
	}
	return op, 1
}

func (p *expressionParser) match(text string) bool {
	if p.position < len(p.tokens) && p.tokens[p.position].Text == text {
		p.position++
		return true
	}
	return false
}

func (p *expressionParser) consume(text string) bool { return p.match(text) }

func isListOperator(op string) bool {
	switch op {
	case "in", "in~", "!in", "!in~", "has_any", "has_all":
		return true
	default:
		return false
	}
}

func joinSpan(first, last Span) Span {
	if first.Length == 0 {
		return last
	}
	return Span{Offset: first.Offset, Length: last.Offset + last.Length - first.Offset, Line: first.Line, Column: first.Column}
}
