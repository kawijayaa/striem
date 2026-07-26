package kql

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

type parser struct {
	tokens  []token
	current int
}

const (
	maxParseCaptures   = 16
	maxParsePattern    = 2 << 10
	maxProjectionItems = 32
	maxBagUnpackItems  = 32
)

func Parse(input string) (Query, error) {
	tokens, err := lex(input)
	if err != nil {
		return Query{}, err
	}
	p := &parser{tokens: tokens}
	return p.parseQuery()
}

func (p *parser) parseQuery() (Query, error) {
	bindings := make([]LetBinding, 0)
	for p.matchIdentifier("let") {
		name, err := p.expect(tokenIdentifier, "expected variable name after 'let'")
		if err != nil {
			return Query{}, err
		}
		if _, err := p.expect(tokenAssign, "expected '=' after variable name"); err != nil {
			return Query{}, err
		}
		var expression Expression
		var tabular *Query
		if p.check(tokenIdentifier) && p.peekNext().Kind == tokenPipe {
			pipeline, parseErr := p.parsePipeline(tokenSemicolon)
			if parseErr != nil {
				return Query{}, parseErr
			}
			tabular = &pipeline
		} else {
			expression, err = p.parseExpression(0)
			if err != nil {
				return Query{}, err
			}
		}
		if _, err := p.expect(tokenSemicolon, "expected ';' after variable declaration"); err != nil {
			return Query{}, err
		}
		bindings = append(bindings, LetBinding{Name: name.Text, At: name, Expression: expression, Tabular: tabular})
	}
	query, err := p.parsePipeline(tokenEOF)
	query.Bindings = bindings
	return query, err
}

func (p *parser) parsePipeline(stop tokenKind) (Query, error) {
	query := Query{}
	source, err := p.expect(tokenIdentifier, "expected table name")
	if err != nil {
		return Query{}, err
	}
	query.Source, query.SourceAt = source.Text, source
	for !p.check(tokenEOF) && !p.check(stop) {
		if _, err := p.expect(tokenPipe, "expected '|' before query operator"); err != nil {
			return Query{}, err
		}
		name, err := p.expect(tokenIdentifier, "expected query operator")
		if err != nil {
			return Query{}, err
		}
		if p.match(tokenMinus) {
			suffix, suffixErr := p.expect(tokenIdentifier, "expected operator name after '-'")
			if suffixErr != nil {
				return Query{}, suffixErr
			}
			name.Text += "-" + suffix.Text
		}
		var operator Operator
		switch name.Text {
		case "where":
			expr, parseErr := p.parseExpression(0)
			err = parseErr
			operator = WhereOperator{At: name, Expression: expr}
		case "search":
			expr, parseErr := p.parseExpression(0)
			err = parseErr
			operator = SearchOperator{At: name, Expression: expr}
		case "project":
			var items []NamedExpression
			items, err = p.parseNamedList(false, false, maxProjectionItems)
			operator = ProjectOperator{At: name, Items: items}
		case "project-away":
			var patterns []ColumnPattern
			patterns, err = p.parseColumnPatterns()
			operator = ProjectAwayOperator{At: name, Patterns: patterns}
		case "project-rename":
			var items []ColumnRename
			items, err = p.parseColumnRenames()
			operator = ProjectRenameOperator{At: name, Items: items}
		case "extend":
			var items []NamedExpression
			items, err = p.parseNamedList(true, false, 0)
			operator = ExtendOperator{At: name, Items: items}
		case "summarize":
			var aggregates, groups []NamedExpression
			aggregates, err = p.parseNamedList(false, true, 0)
			if err == nil && p.matchIdentifier("by") {
				groups, err = p.parseNamedList(false, false, 0)
			}
			operator = SummarizeOperator{At: name, Aggregates: aggregates, Groups: groups}
		case "distinct":
			var items []NamedExpression
			items, err = p.parseNamedList(false, false, 0)
			operator = DistinctOperator{At: name, Items: items}
		case "order", "sort":
			_, err = p.expectIdentifier("by", "expected 'by' after sort operator")
			var terms []SortTerm
			if err == nil {
				terms, err = p.parseSortTerms()
			}
			operator = SortOperator{At: name, Terms: terms}
		case "take", "limit":
			var value Expression
			value, err = p.parseRowLimit()
			operator = TakeOperator{At: name, Count: value}
		case "top":
			var value Expression
			value, err = p.parseRowLimit()
			if err == nil {
				_, err = p.expectIdentifier("by", "expected 'by' after top row count")
			}
			var term SortTerm
			if err == nil {
				var expression Expression
				expression, err = p.parseExpression(0)
				term = SortTerm{Expression: expression, Descending: true}
				if p.matchIdentifier("asc") {
					term.Descending = false
				} else {
					p.matchIdentifier("desc")
				}
			}
			operator = TopOperator{At: name, Count: value, Term: term}
		case "count":
			operator = CountOperator{At: name}
		case "parse", "parse-where":
			operator, err = p.parseParse(name, name.Text == "parse-where")
		case "evaluate":
			operator, err = p.parseEvaluate(name)
		case "mv-expand":
			start := p.peek()
			columnName := ""
			if p.check(tokenIdentifier) && p.peekNext().Kind == tokenAssign {
				columnName = p.advance().Text
				p.advance()
			}
			var expression Expression
			expression, err = p.parseExpression(0)
			if err == nil && columnName == "" {
				columnName = expressionName(expression, 1)
				if strings.HasPrefix(columnName, "Column") {
					err = errorAt(start, "mv-expand expressions require a column name")
				}
			}
			limit := Expression(LiteralExpression{At: name, Value: float64(128)})
			if err == nil && p.matchIdentifier("limit") {
				limit, err = p.parseRowLimit()
			}
			operator = MVExpandOperator{At: name, Name: columnName, Expression: expression, Limit: limit}
		case "mv-apply":
			operator, err = p.parseMVApply(name)
		case "union":
			var queries []Query
			for {
				var nested Query
				if p.match(tokenLeftParen) {
					nested, err = p.parsePipeline(tokenRightParen)
					if err == nil {
						_, err = p.expect(tokenRightParen, "expected ')' after union query")
					}
				} else {
					var nestedSource token
					nestedSource, err = p.expect(tokenIdentifier, "expected table name after 'union'")
					nested = Query{Source: nestedSource.Text, SourceAt: nestedSource}
				}
				if err != nil {
					break
				}
				queries = append(queries, nested)
				if !p.match(tokenComma) {
					break
				}
			}
			operator = UnionOperator{At: name, Queries: queries}
		case "join":
			kind := JoinInner
			if p.matchIdentifier("kind") {
				_, err = p.expect(tokenAssign, "expected '=' after join kind")
				var kindToken token
				if err == nil {
					kindToken, err = p.expect(tokenIdentifier, "expected join kind")
				}
				if err == nil {
					switch kindToken.Text {
					case string(JoinInner):
						kind = JoinInner
					case string(JoinLeftOuter):
						kind = JoinLeftOuter
					case string(JoinLeftAnti):
						kind = JoinLeftAnti
					default:
						err = errorAt(kindToken, "join kind %q is not supported", kindToken.Text)
					}
				}
			}
			if err == nil {
				_, err = p.expect(tokenLeftParen, "expected '(' before join query")
			}
			var right Query
			if err == nil {
				right, err = p.parsePipeline(tokenRightParen)
			}
			if err == nil {
				_, err = p.expect(tokenRightParen, "expected ')' after join query")
			}
			if err == nil {
				_, err = p.expectIdentifier("on", "expected 'on' after join query")
			}
			var keys []JoinKey
			for err == nil {
				var key token
				key, err = p.expect(tokenIdentifier, "expected join column after 'on'")
				if err != nil {
					break
				}
				keys = append(keys, JoinKey{Name: key.Text, At: key})
				if !p.match(tokenComma) {
					break
				}
			}
			operator = JoinOperator{At: name, Kind: kind, Right: right, Keys: keys}
		default:
			return Query{}, errorAt(name, "operator %q is not supported", name.Text)
		}
		if err != nil {
			return Query{}, err
		}
		query.Operators = append(query.Operators, operator)
	}
	return query, nil
}

func (p *parser) parseRowLimit() (Expression, error) {
	if p.check(tokenPipe) || p.check(tokenEOF) {
		return nil, errorAt(p.peek(), "expected integer row limit")
	}
	return p.parseExpression(0)
}

func (p *parser) parseMVApply(at token) (Operator, error) {
	alias, err := p.expect(tokenIdentifier, "mv-apply requires an aliased expression")
	if err != nil {
		return nil, err
	}
	if _, err = p.expect(tokenAssign, "mv-apply requires an alias followed by '='"); err != nil {
		return nil, err
	}
	expression, err := p.parseExpression(0)
	if err != nil {
		return nil, err
	}
	if p.check(tokenComma) {
		return nil, errorAt(p.peek(), "mv-apply supports exactly one aliased expression")
	}

	limit := Expression(LiteralExpression{At: at, Value: float64(128)})
	if p.matchIdentifier("limit") {
		limit, err = p.parseRowLimit()
		if err != nil {
			return nil, err
		}
	}
	if _, err = p.expectIdentifier("on", "expected 'on' after mv-apply expression"); err != nil {
		return nil, err
	}
	if _, err = p.expect(tokenLeftParen, "expected '(' after mv-apply 'on'"); err != nil {
		return nil, err
	}

	wheres := make([]Expression, 0, 1)
	for p.matchIdentifier("where") {
		where, parseErr := p.parseExpression(0)
		if parseErr != nil {
			return nil, parseErr
		}
		wheres = append(wheres, where)
		if _, err = p.expect(tokenPipe, "mv-apply where must be followed by summarize"); err != nil {
			return nil, err
		}
	}
	if !p.matchIdentifier("summarize") {
		return nil, errorAt(p.peek(), "mv-apply body supports only where followed by summarize")
	}
	aggregates, err := p.parseNamedList(false, true, 0)
	if err != nil {
		return nil, err
	}
	if p.checkIdentifier("by") {
		return nil, errorAt(p.peek(), "mv-apply summarize does not support 'by'")
	}
	if p.check(tokenPipe) {
		return nil, errorAt(p.peek(), "mv-apply summarize must be the final body operator")
	}
	if _, err = p.expect(tokenRightParen, "expected ')' after mv-apply body"); err != nil {
		return nil, err
	}
	return MVApplyOperator{At: at, Alias: alias.Text, Expression: expression, Limit: limit, Wheres: wheres, Aggregates: aggregates}, nil
}

func (p *parser) parseNamedList(requireAlias, stopAtBy bool, maxItems int) ([]NamedExpression, error) {
	items := make([]NamedExpression, 0, 4)
	for {
		if p.check(tokenPipe) || p.check(tokenEOF) || (stopAtBy && p.checkIdentifier("by")) {
			break
		}
		start := p.peek()
		name := ""
		explicit := false
		if p.check(tokenIdentifier) && p.peekNext().Kind == tokenAssign {
			name = p.advance().Text
			p.advance()
			explicit = true
		}
		expr, err := p.parseExpression(0)
		if err != nil {
			return nil, err
		}
		if name == "" {
			if requireAlias {
				return nil, errorAt(start, "extend expressions require a column name")
			}
			name = expressionName(expr, len(items)+1)
		}
		items = append(items, NamedExpression{Name: name, At: start, Expression: expr, Explicit: explicit})
		if maxItems > 0 && len(items) > maxItems {
			return nil, errorAt(start, "projection supports at most %d items", maxItems)
		}
		if !p.match(tokenComma) {
			break
		}
	}
	if len(items) == 0 {
		return nil, errorAt(p.peek(), "expected expression")
	}
	return items, nil
}

func (p *parser) parseColumnPatterns() ([]ColumnPattern, error) {
	patterns := make([]ColumnPattern, 0, 4)
	for {
		start := p.peek()
		prefix := p.match(tokenStar)
		name := ""
		if p.check(tokenIdentifier) {
			name = p.advance().Text
		}
		suffix := p.match(tokenStar)
		if name == "" && !prefix {
			return nil, errorAt(start, "expected column name or wildcard pattern")
		}
		patterns = append(patterns, ColumnPattern{At: start, Name: name, PrefixWildcard: prefix, SuffixWildcard: suffix})
		if len(patterns) > maxProjectionItems {
			return nil, errorAt(start, "projection supports at most %d items", maxProjectionItems)
		}
		if !p.match(tokenComma) {
			break
		}
	}
	return patterns, nil
}

func (p *parser) parseColumnRenames() ([]ColumnRename, error) {
	items := make([]ColumnRename, 0, 4)
	for {
		name, err := p.expect(tokenIdentifier, "expected new column name")
		if err != nil {
			return nil, err
		}
		if _, err = p.expect(tokenAssign, "expected '=' after new column name"); err != nil {
			return nil, err
		}
		source, err := p.expect(tokenIdentifier, "expected source column name")
		if err != nil {
			return nil, err
		}
		items = append(items, ColumnRename{At: name, Name: name.Text, Source: source.Text})
		if len(items) > maxProjectionItems {
			return nil, errorAt(name, "projection supports at most %d items", maxProjectionItems)
		}
		if !p.match(tokenComma) {
			break
		}
	}
	return items, nil
}

func (p *parser) parseParse(at token, where bool) (Operator, error) {
	if p.matchIdentifier("kind") {
		if _, err := p.expect(tokenAssign, "expected '=' after parse kind"); err != nil {
			return nil, err
		}
		kind, err := p.expect(tokenIdentifier, "expected parse kind")
		if err != nil {
			return nil, err
		}
		if kind.Text != "simple" {
			return nil, errorAt(kind, "parse kind %q is not supported", kind.Text)
		}
	}
	expression, err := p.parseExpression(0)
	if err != nil {
		return nil, err
	}
	if _, err = p.expectIdentifier("with", "expected 'with' before parse pattern"); err != nil {
		return nil, err
	}
	pattern := make([]ParsePatternItem, 0, 8)
	captures := 0
	for !p.check(tokenPipe) && !p.check(tokenEOF) && !p.check(tokenRightParen) && !p.check(tokenSemicolon) {
		itemAt := p.peek()
		switch {
		case p.match(tokenString):
			pattern = append(pattern, ParsePatternItem{At: itemAt, Kind: ParseDelimiter, Text: p.previous().Text})
		case p.match(tokenStar):
			pattern = append(pattern, ParsePatternItem{At: itemAt, Kind: ParseWildcard})
		case p.check(tokenIdentifier):
			name := p.advance()
			if _, err = p.expect(tokenColon, "parse captures require an explicit type"); err != nil {
				return nil, err
			}
			valueType, typeErr := p.parseScalarType(false, "parse capture")
			if typeErr != nil {
				return nil, typeErr
			}
			captures++
			if captures > maxParseCaptures {
				return nil, errorAt(name, "parse supports at most %d captures", maxParseCaptures)
			}
			pattern = append(pattern, ParsePatternItem{At: name, Kind: ParseCapture, Text: name.Text, Type: valueType})
		default:
			return nil, errorAt(itemAt, "expected string delimiter, '*', or typed capture in parse pattern")
		}
	}
	if len(pattern) == 0 {
		return nil, errorAt(p.peek(), "parse pattern cannot be empty")
	}
	if captures == 0 {
		return nil, errorAt(at, "parse pattern requires at least one capture")
	}
	if len(pattern) > 1 {
		for index := 1; index < len(pattern); index++ {
			if pattern[index-1].Kind != ParseDelimiter && pattern[index].Kind != ParseDelimiter {
				return nil, errorAt(pattern[index].At, "parse variable fields require a literal separator")
			}
		}
	}
	return ParseOperator{At: at, Expression: expression, Pattern: pattern, Where: where}, nil
}

func (p *parser) parseEvaluate(at token) (Operator, error) {
	function, err := p.expect(tokenIdentifier, "expected evaluate function")
	if err != nil {
		return nil, err
	}
	if function.Text != "bag_unpack" {
		return nil, errorAt(function, "evaluate function %q is not supported", function.Text)
	}
	if _, err = p.expect(tokenLeftParen, "expected '(' after bag_unpack"); err != nil {
		return nil, err
	}
	column, err := p.expect(tokenIdentifier, "bag_unpack requires a dynamic column")
	if err != nil {
		return nil, err
	}
	prefix := ""
	if p.match(tokenComma) {
		value, prefixErr := p.expect(tokenString, "bag_unpack prefix must be a string literal")
		if prefixErr != nil {
			return nil, prefixErr
		}
		prefix = value.Text
	}
	if _, err = p.expect(tokenRightParen, "expected ')' after bag_unpack arguments"); err != nil {
		return nil, err
	}
	if _, err = p.expect(tokenColon, "bag_unpack requires an explicit output schema"); err != nil {
		return nil, err
	}
	if _, err = p.expect(tokenLeftParen, "expected '(' before bag_unpack output schema"); err != nil {
		return nil, err
	}
	if _, err = p.expect(tokenStar, "bag_unpack output schema must start with '*'"); err != nil {
		return nil, err
	}
	if _, err = p.expect(tokenComma, "bag_unpack output schema requires at least one typed column"); err != nil {
		return nil, err
	}
	items := make([]BagUnpackItem, 0, 4)
	for {
		name, itemErr := p.expect(tokenIdentifier, "expected bag_unpack output column")
		if itemErr != nil {
			return nil, itemErr
		}
		if _, itemErr = p.expect(tokenColon, "bag_unpack output columns require an explicit type"); itemErr != nil {
			return nil, itemErr
		}
		valueType, typeErr := p.parseScalarType(true, "bag_unpack output")
		if typeErr != nil {
			return nil, typeErr
		}
		items = append(items, BagUnpackItem{At: name, Name: name.Text, Type: valueType})
		if len(items) > maxBagUnpackItems {
			return nil, errorAt(name, "bag_unpack supports at most %d output columns", maxBagUnpackItems)
		}
		if !p.match(tokenComma) {
			break
		}
	}
	if _, err = p.expect(tokenRightParen, "expected ')' after bag_unpack output schema"); err != nil {
		return nil, err
	}
	return BagUnpackOperator{At: at, Column: column.Text, Prefix: prefix, Items: items}, nil
}

func (p *parser) parseScalarType(allowDynamic bool, context string) (ScalarType, error) {
	typeToken, err := p.expect(tokenIdentifier, "expected "+context+" type")
	if err != nil {
		return "", err
	}
	valueType := ScalarType(typeToken.Text)
	if valueType == ScalarString || valueType == ScalarLong || valueType == ScalarReal || allowDynamic && valueType == ScalarDynamic {
		return valueType, nil
	}
	return "", errorAt(typeToken, "%s type %q is not supported", context, typeToken.Text)
}

func (p *parser) parseSortTerms() ([]SortTerm, error) {
	terms := make([]SortTerm, 0, 2)
	for {
		expr, err := p.parseExpression(0)
		if err != nil {
			return nil, err
		}
		desc := false
		if p.matchIdentifier("desc") {
			desc = true
		} else {
			p.matchIdentifier("asc")
		}
		terms = append(terms, SortTerm{Expression: expr, Descending: desc})
		if !p.match(tokenComma) {
			break
		}
	}
	return terms, nil
}

func (p *parser) parseExpression(minPrecedence int) (Expression, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for {
		op, precedence := p.infixOperator()
		if precedence < minPrecedence {
			break
		}
		at := p.advance()
		if op == "in" || op == "in~" || op == "!in" || op == "!in~" || op == "has_any" || op == "has_all" {
			if _, err := p.expect(tokenLeftParen, "expected '(' after in"); err != nil {
				return nil, err
			}
			items := make([]Expression, 0, 4)
			for !p.check(tokenRightParen) {
				item, itemErr := p.parseExpression(0)
				if itemErr != nil {
					return nil, itemErr
				}
				items = append(items, item)
				if !p.match(tokenComma) {
					break
				}
			}
			if len(items) == 0 {
				return nil, errorAt(p.peek(), "in list cannot be empty")
			}
			if _, err := p.expect(tokenRightParen, "expected ')' after in list"); err != nil {
				return nil, err
			}
			left = BinaryExpression{At: at, Operator: op, Left: left, Right: ListExpression{At: at, Items: items}}
			continue
		}
		right, rightErr := p.parseExpression(precedence + 1)
		if rightErr != nil {
			return nil, rightErr
		}
		left = BinaryExpression{At: at, Operator: op, Left: left, Right: right}
	}
	return left, nil
}

func (p *parser) parseUnary() (Expression, error) {
	if p.matchIdentifier("not") {
		at := p.previous()
		expr, err := p.parseExpression(3)
		return UnaryExpression{At: at, Operator: "not", Operand: expr}, err
	}
	if p.match(tokenMinus) || p.match(tokenPlus) {
		at := p.previous()
		expr, err := p.parseUnary()
		return UnaryExpression{At: at, Operator: at.Text, Operand: expr}, err
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Expression, error) {
	tok := p.advance()
	switch tok.Kind {
	case tokenString:
		return LiteralExpression{At: tok, Value: tok.Text}, nil
	case tokenNumber:
		if p.check(tokenIdentifier) && tok.End == p.peek().Offset {
			unit := p.peek().Text
			if duration, ok := parseDuration(tok.Text + unit); ok {
				p.advance()
				return DurationExpression{At: tok, Value: duration}, nil
			}
		}
		value, err := strconv.ParseFloat(tok.Text, 64)
		if err != nil {
			return nil, errorAt(tok, "invalid number")
		}
		return LiteralExpression{At: tok, Value: value}, nil
	case tokenIdentifier:
		if tok.Text == "true" {
			return LiteralExpression{At: tok, Value: true}, nil
		}
		if tok.Text == "false" {
			return LiteralExpression{At: tok, Value: false}, nil
		}
		if tok.Text == "null" {
			return LiteralExpression{At: tok, Value: nil}, nil
		}
		if p.match(tokenLeftParen) {
			args := make([]Expression, 0, 2)
			for !p.check(tokenRightParen) {
				if p.match(tokenStar) {
					args = append(args, StarExpression{At: p.previous()})
				} else {
					arg, err := p.parseExpression(0)
					if err != nil {
						return nil, err
					}
					args = append(args, arg)
				}
				if !p.match(tokenComma) {
					break
				}
			}
			if _, err := p.expect(tokenRightParen, "expected ')' after function arguments"); err != nil {
				return nil, err
			}
			return FunctionExpression{At: tok, Name: tok.Text, Arguments: args}, nil
		}
		parts := []string{tok.Text}
		indices := make(map[int]int)
		for {
			if p.match(tokenDot) {
				part, err := p.expect(tokenIdentifier, "expected property name after '.'")
				if err != nil {
					return nil, err
				}
				parts = append(parts, part.Text)
				continue
			}
			if p.match(tokenLeftBracket) {
				part := p.advance()
				if part.Kind != tokenString && part.Kind != tokenNumber {
					return nil, errorAt(part, "expected quoted property name or array index after '['")
				}
				if _, err := p.expect(tokenRightBracket, "expected ']' after property name"); err != nil {
					return nil, err
				}
				if part.Kind == tokenNumber {
					index, parseErr := strconv.Atoi(part.Text)
					if parseErr != nil || index < 0 {
						return nil, errorAt(part, "array index must be a non-negative integer")
					}
					parts = append(parts, "")
					indices[len(parts)-1] = index
				} else {
					parts = append(parts, part.Text)
				}
				continue
			}
			break
		}
		return IdentifierExpression{At: tok, Parts: parts, Indices: indices}, nil
	case tokenLeftParen:
		expr, err := p.parseExpression(0)
		if err != nil {
			return nil, err
		}
		_, err = p.expect(tokenRightParen, "expected ')'")
		return expr, err
	default:
		return nil, errorAt(tok, "expected expression")
	}
}

func (p *parser) infixOperator() (string, int) {
	tok := p.peek()
	switch tok.Kind {
	case tokenEqual:
		return "==", 3
	case tokenNotEqual:
		return "!=", 3
	case tokenEqualInsensitive:
		return "=~", 3
	case tokenNotEqualInsensitive:
		return "!~", 3
	case tokenLess:
		return "<", 3
	case tokenLessEqual:
		return "<=", 3
	case tokenGreater:
		return ">", 3
	case tokenGreaterEqual:
		return ">=", 3
	case tokenPlus:
		return "+", 4
	case tokenMinus:
		return "-", 4
	case tokenStar:
		return "*", 5
	case tokenSlash:
		return "/", 5
	case tokenPercent:
		return "%", 5
	}
	if tok.Kind == tokenIdentifier {
		switch tok.Text {
		case "or":
			return "or", 1
		case "and":
			return "and", 2
		case "in", "in~", "!in", "!in~", "contains", "contains_cs", "startswith", "startswith_cs", "endswith", "endswith_cs", "has", "has_cs", "has_any", "has_all":
			return tok.Text, 3
		}
	}
	return "", -1
}

func parseDuration(value string) (time.Duration, bool) {
	for _, unit := range []struct {
		suffix string
		hours  float64
	}{
		{suffix: "w", hours: 168},
		{suffix: "d", hours: 24},
	} {
		if strings.HasSuffix(value, unit.suffix) {
			amount, err := strconv.ParseFloat(strings.TrimSuffix(value, unit.suffix), 64)
			if err != nil {
				return 0, false
			}
			duration, parseErr := time.ParseDuration(strconv.FormatFloat(amount*unit.hours, 'f', -1, 64) + "h")
			return duration, parseErr == nil
		}
	}
	for _, suffix := range []string{"ms", "s", "m", "h"} {
		if !strings.HasSuffix(value, suffix) {
			continue
		}
		if _, err := strconv.ParseFloat(strings.TrimSuffix(value, suffix), 64); err != nil {
			return 0, false
		}
		duration, err := time.ParseDuration(value)
		return duration, err == nil
	}
	return 0, false
}

func expressionName(expr Expression, index int) string {
	switch value := expr.(type) {
	case IdentifierExpression:
		if _, indexed := value.Indices[len(value.Parts)-1]; indexed {
			return fmt.Sprintf("Column%d", index)
		}
		return value.Parts[len(value.Parts)-1]
	case FunctionExpression:
		if value.Name == "count" {
			return "count_"
		}
		return value.Name + "_"
	default:
		return fmt.Sprintf("Column%d", index)
	}
}

func (p *parser) peek() token { return p.tokens[p.current] }
func (p *parser) peekNext() token {
	if p.current+1 >= len(p.tokens) {
		return p.tokens[len(p.tokens)-1]
	}
	return p.tokens[p.current+1]
}
func (p *parser) previous() token { return p.tokens[p.current-1] }
func (p *parser) advance() token {
	tok := p.peek()
	if tok.Kind != tokenEOF {
		p.current++
	}
	return tok
}
func (p *parser) check(kind tokenKind) bool { return p.peek().Kind == kind }
func (p *parser) match(kind tokenKind) bool {
	if !p.check(kind) {
		return false
	}
	p.advance()
	return true
}
func (p *parser) checkIdentifier(value string) bool {
	return p.peek().Kind == tokenIdentifier && p.peek().Text == value
}
func (p *parser) matchIdentifier(value string) bool {
	if !p.checkIdentifier(value) {
		return false
	}
	p.advance()
	return true
}
func (p *parser) expect(kind tokenKind, message string) (token, error) {
	if !p.check(kind) {
		return token{}, errorAt(p.peek(), "%s", message)
	}
	return p.advance(), nil
}
func (p *parser) expectIdentifier(value, message string) (token, error) {
	if !p.checkIdentifier(value) {
		return token{}, errorAt(p.peek(), "%s", message)
	}
	return p.advance(), nil
}
