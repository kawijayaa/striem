package kql

import (
	"fmt"
	"strings"
)

// ParseError is a stable syntax diagnostic.
type ParseError struct {
	Code    string
	Message string
	Span    Span
}

func (e ParseError) Error() string {
	return fmt.Sprintf("%s: %s at %s", e.Code, e.Message, e.Span)
}

var queryOperators = map[string]struct{}{
	"as": {}, "assert-schema": {}, "consume": {}, "count": {}, "distinct": {},
	"evaluate": {}, "extend": {}, "facet": {}, "filter": {}, "find": {},
	"fork": {}, "getschema": {}, "graph-mark-components": {}, "graph-match": {},
	"graph-shortest-paths": {}, "graph-to-table": {}, "graph-where-edges": {},
	"graph-where-nodes": {}, "invoke": {}, "join": {}, "limit": {}, "lookup": {},
	"macro-expand": {}, "make-graph": {}, "make-series": {}, "mv-apply": {},
	"mv-expand": {}, "mvapply": {}, "mvexpand": {}, "order": {}, "parse": {},
	"parse-kv": {}, "parse-where": {}, "partition": {}, "project": {},
	"project-away": {}, "project-by-names": {}, "project-keep": {},
	"project-rename": {}, "project-reorder": {}, "reduce": {}, "render": {},
	"sample": {}, "sample-distinct": {}, "scan": {}, "search": {}, "serialize": {},
	"sort": {}, "summarize": {}, "take": {}, "top": {}, "top-hitters": {},
	"top-nested": {}, "union": {}, "where": {}, "__executeandcache": {},
	"__partitionby": {},
}

var sourceKeywords = map[string]string{
	"datatable": "datatable", "externaldata": "externaldata",
	"external_data": "externaldata", "inline_external_table": "inline_external_table",
	"inline-external-table": "inline_external_table", "print": "print",
	"range": "range", "entity_group": "entity_group", "union": "union",
	"__contextual_datatable":    "contextual_datatable",
	"materialized-view-combine": "materialized_view_combine",
}

// Parse lexes and parses a KQL script.
func Parse(source string) (*Script, []ParseError) {
	tokens, lexErrors := Lex(source)
	errs := make([]ParseError, 0, len(lexErrors))
	for _, err := range lexErrors {
		errs = append(errs, ParseError{Code: "KQLP0001", Message: err.Message, Span: err.Span})
	}

	content := tokens[:len(tokens)-1]
	script := &Script{Source: source, Tokens: tokens}
	for _, part := range splitTopLevel(content, ";") {
		if len(part) == 0 {
			continue
		}
		statement, statementErrs := parseStatement(part)
		errs = append(errs, statementErrs...)
		if statement != nil {
			script.Statements = append(script.Statements, statement)
		}
	}
	if len(script.Statements) == 0 && len(content) == 0 {
		errs = append(errs, ParseError{Code: "KQLP0002", Message: "query is empty", Span: tokens[len(tokens)-1].Span})
	}
	return script, errs
}

func parseStatement(tokens []Token) (Statement, []ParseError) {
	span := tokensSpan(tokens)
	first := lower(tokens[0])
	switch first {
	case "let":
		return parseLet(tokens)
	case "alias", "declare", "restrict", "set":
		return &ControlStatement{Kind: first, Raw: tokens, Span: span}, nil
	default:
		pipeline, errs := parsePipeline(tokens)
		return &ExpressionStatement{Pipeline: pipeline, Span: span}, errs
	}
}

func parseLet(tokens []Token) (Statement, []ParseError) {
	statement := &LetStatement{Raw: tokens, Span: tokensSpan(tokens)}
	if len(tokens) < 4 || tokens[1].Kind != IdentifierToken || tokens[2].Text != "=" {
		return statement, []ParseError{{Code: "KQLP0101", Message: "expected 'let name = expression'", Span: statement.Span}}
	}
	statement.Name = tokens[1].Text
	value := tokens[3:]
	if hasTopLevel(value, "|") || beginsTabularSource(value) {
		pipeline, errs := parsePipeline(value)
		statement.Pipeline = pipeline
		return statement, errs
	}
	expr, errs := parseExpressionTokens(value)
	statement.Value = expr
	return statement, errs
}

func parsePipeline(tokens []Token) (*Pipeline, []ParseError) {
	parts := splitTopLevel(tokens, "|")
	pipeline := &Pipeline{Span: tokensSpan(tokens)}
	if len(parts) == 0 || len(parts[0]) == 0 {
		return pipeline, []ParseError{{Code: "KQLP0201", Message: "missing pipeline source", Span: pipeline.Span}}
	}

	source, errs := parseSource(parts[0])
	pipeline.Source = source
	for _, part := range parts[1:] {
		if len(part) == 0 {
			errs = append(errs, ParseError{Code: "KQLP0202", Message: "missing query operator after pipe", Span: pipeline.Span})
			continue
		}
		op, opErrs := parseOperator(part)
		errs = append(errs, opErrs...)
		pipeline.Operators = append(pipeline.Operators, op)
	}
	return pipeline, errs
}

func parseSource(tokens []Token) (Source, []ParseError) {
	source := Source{Kind: "expression", Raw: tokens, Span: tokensSpan(tokens)}
	if lower(tokens[0]) == "union" {
		op, errs := parseUnion(Operator{Kind: "union", Span: source.Span}, tokens[1:])
		spec := op.Body.(UnionSpec)
		source.Kind, source.Union = "union", &spec
		return source, errs
	}
	if kind, ok := sourceKeywords[lower(tokens[0])]; ok {
		source.Kind = kind
		if kind == "print" {
			items, errs := parseExpressionList(tokens[1:])
			source.Items = items
			return source, errs
		}
		if kind == "range" {
			return parseRangeSource(source, tokens[1:])
		}
		if kind == "datatable" {
			return parseDatatableSource(source, tokens[1:])
		}
		return source, nil
	}
	expr, errs := parseExpressionTokens(tokens)
	source.Expr = expr
	if name, ok := expr.(*NameExpression); ok {
		source.Kind = "table"
		source.Name = name.Name
	}
	return source, errs
}

func parseRangeSource(source Source, tokens []Token) (Source, []ParseError) {
	fromAt, toAt, stepAt := topLevelKeyword(tokens, "from"), topLevelKeyword(tokens, "to"), topLevelKeyword(tokens, "step")
	if fromAt != 1 || toAt <= fromAt+1 || stepAt <= toAt+1 || stepAt == len(tokens)-1 {
		return source, []ParseError{{Code: "KQLP0210", Message: "expected 'range name from expression to expression step expression'", Span: source.Span}}
	}
	source.Name = tokens[0].Text
	var errs []ParseError
	source.From, errs = parseExpressionTokens(tokens[fromAt+1 : toAt])
	var expressionErrs []ParseError
	source.To, expressionErrs = parseExpressionTokens(tokens[toAt+1 : stepAt])
	errs = append(errs, expressionErrs...)
	source.Step, expressionErrs = parseExpressionTokens(tokens[stepAt+1:])
	errs = append(errs, expressionErrs...)
	return source, errs
}

func parseDatatableSource(source Source, tokens []Token) (Source, []ParseError) {
	if len(tokens) < 4 || tokens[0].Text != "(" {
		return source, []ParseError{{Code: "KQLP0211", Message: "datatable requires a parenthesized schema", Span: source.Span}}
	}
	closeSchema := matchingClose(tokens, 0)
	if closeSchema < 0 || closeSchema+1 >= len(tokens) || tokens[closeSchema+1].Text != "[" {
		return source, []ParseError{{Code: "KQLP0212", Message: "datatable requires a bracketed value list", Span: source.Span}}
	}
	for _, definition := range splitTopLevel(tokens[1:closeSchema], ",") {
		colon := topLevelToken(definition, ":")
		if colon != 1 || len(definition) != 3 {
			return source, []ParseError{{Code: "KQLP0213", Message: "expected datatable column name:type", Span: tokensSpan(definition)}}
		}
		source.Columns = append(source.Columns, ColumnDefinition{Name: definition[0].Text, Type: lower(definition[2])})
	}
	closeValues := matchingClose(tokens, closeSchema+1)
	if closeValues < 0 {
		return source, []ParseError{{Code: "KQLP0214", Message: "unterminated datatable value list", Span: source.Span}}
	}
	values, errs := parseExpressionList(tokens[closeSchema+2 : closeValues])
	if len(source.Columns) == 0 || len(values)%len(source.Columns) != 0 {
		errs = append(errs, ParseError{Code: "KQLP0215", Message: "datatable values must fill complete rows", Span: source.Span})
		return source, errs
	}
	for len(values) > 0 {
		source.Rows = append(source.Rows, append([]Expression(nil), values[:len(source.Columns)]...))
		values = values[len(source.Columns):]
	}
	return source, errs
}

func matchingClose(tokens []Token, open int) int {
	if open >= len(tokens) {
		return -1
	}
	pairs := map[string]string{"(": ")", "[": "]", "{": "}"}
	close, ok := pairs[tokens[open].Text]
	if !ok {
		return -1
	}
	depth := 0
	for i := open; i < len(tokens); i++ {
		if tokens[i].Text == tokens[open].Text {
			depth++
		} else if tokens[i].Text == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func parseOperator(tokens []Token) (Operator, []ParseError) {
	kind := lower(tokens[0])
	if kind == "order" && len(tokens) > 1 && lower(tokens[1]) == "by" {
		kind = "sort"
		tokens = append([]Token{tokens[0]}, tokens[2:]...)
	} else if kind == "filter" {
		kind = "where"
	} else if kind == "limit" {
		kind = "take"
	} else if kind == "mvexpand" {
		kind = "mv-expand"
	} else if kind == "mvapply" {
		kind = "mv-apply"
	}
	op := Operator{Kind: kind, Raw: tokens, Span: tokensSpan(tokens)}
	if _, ok := queryOperators[lower(tokens[0])]; !ok {
		op.Body = RawSpec{Tokens: tokens[1:]}
		return op, []ParseError{{Code: "KQLP0301", Message: "unknown query operator " + tokens[0].Text, Span: tokens[0].Span}}
	}

	body := tokens[1:]
	if kind == "sort" && len(body) > 0 && lower(body[0]) == "by" {
		body = body[1:]
	}
	switch kind {
	case "where", "take", "sample":
		expr, errs := parseExpressionTokens(body)
		op.Body = ExpressionSpec{Expressions: []Expression{expr}}
		return op, errs
	case "search":
		if len(body) == 0 {
			op.Body = RawSpec{Tokens: body}
			return op, nil
		}
		expr, errs := parseExpressionTokens(body)
		op.Body = SearchSpec{Term: expr}
		return op, errs
	case "project", "project-away", "project-by-names", "project-keep", "project-rename", "project-reorder", "extend", "distinct", "serialize":
		exprs, errs := parseExpressionList(body)
		op.Body = ExpressionSpec{Expressions: exprs}
		return op, errs
	case "summarize":
		return parseSummarize(op, body)
	case "sort":
		spec, errs := parseOrderedList(body)
		op.Body = spec
		return op, errs
	case "top":
		return parseTop(op, body)
	case "join", "lookup":
		return parseJoin(op, body)
	case "union":
		return parseUnion(op, body)
	case "mv-expand":
		return parseMvExpand(op, body)
	case "mv-apply":
		return parseMvApply(op, body)
	case "as":
		return parseAs(op, body)
	case "sample-distinct":
		return parseSampleDistinct(op, body)
	case "count":
		op.Body = RawSpec{Tokens: body}
		return op, nil
	default:
		op.Body = RawSpec{Tokens: body}
		return op, nil
	}
}

func parseMvExpand(op Operator, tokens []Token) (Operator, []ParseError) {
	spec, errs := parseMvExpansions(tokens, true)
	spec.Legacy = len(op.Raw) > 0 && lower(op.Raw[0]) == "mvexpand"
	if spec.Legacy && spec.Limit == nil {
		spec.Limit = &LiteralExpression{Text: "128", Kind: NumberToken, Span: op.Span}
	}
	op.Body = spec
	return op, errs
}

func parseMvApply(op Operator, tokens []Token) (Operator, []ParseError) {
	on := topLevelKeyword(tokens, "on")
	if on < 0 || on == len(tokens)-1 {
		op.Body = MvApplySpec{}
		return op, []ParseError{{Code: "KQLP0330", Message: "mv-apply requires an on (...) subquery", Span: op.Span}}
	}
	inner := trimParentheses(tokens[on+1:])
	if len(inner) == len(tokens[on+1:]) || len(inner) == 0 {
		op.Body = MvApplySpec{}
		return op, []ParseError{{Code: "KQLP0330", Message: "mv-apply requires an on (...) subquery", Span: op.Span}}
	}
	expand, errs := parseMvExpansions(tokens[:on], false)
	spec := MvApplySpec{ItemIndex: expand.ItemIndex, Items: expand.Items, Limit: expand.Limit}
	for _, part := range splitTopLevel(inner, "|") {
		if len(part) == 0 {
			continue
		}
		operator, operatorErrs := parseOperator(part)
		spec.Operators = append(spec.Operators, operator)
		errs = append(errs, operatorErrs...)
	}
	if len(spec.Operators) == 0 {
		errs = append(errs, ParseError{Code: "KQLP0331", Message: "mv-apply subquery must contain an operator", Span: op.Span})
	}
	op.Body = spec
	return op, errs
}

func parseMvExpansions(tokens []Token, allowKind bool) (MvExpandSpec, []ParseError) {
	spec := MvExpandSpec{Kind: "bag"}
	var errs []ParseError
	position := 0
	for position+2 < len(tokens) && tokens[position+1].Text == "=" {
		name, value := lower(tokens[position]), lower(tokens[position+2])
		recognized := true
		switch name {
		case "with_itemindex":
			if tokens[position+2].Kind != IdentifierToken {
				errs = append(errs, ParseError{Code: "KQLP0332", Message: "with_itemindex requires a column name", Span: tokens[position+2].Span})
			} else {
				spec.ItemIndex = tokens[position+2].Text
			}
		case "kind", "bagexpansion":
			if !allowKind {
				recognized = false
			} else if value != "bag" && value != "array" {
				errs = append(errs, ParseError{Code: "KQLP0332", Message: "unsupported multi-value expansion parameter", Span: tokens[position].Span})
			} else {
				spec.Kind = value
			}
		default:
			recognized = false
		}
		if !recognized {
			break
		}
		position += 3
	}
	if position >= len(tokens) {
		errs = append(errs, ParseError{Code: "KQLP0333", Message: "multi-value expansion requires an expression", Span: tokensSpan(tokens)})
		return spec, errs
	}
	tokens = tokens[position:]
	if limit := topLevelKeyword(tokens, "limit"); limit >= 0 {
		if limit == len(tokens)-1 {
			errs = append(errs, ParseError{Code: "KQLP0334", Message: "limit requires a row count", Span: tokens[limit].Span})
		} else {
			var limitErrs []ParseError
			spec.Limit, limitErrs = parseExpressionTokens(tokens[limit+1:])
			errs = append(errs, limitErrs...)
		}
		tokens = tokens[:limit]
	}
	for _, part := range splitTopLevel(tokens, ",") {
		if len(part) == 0 {
			continue
		}
		item := MvExpansion{Span: tokensSpan(part)}
		expressionTokens := part
		if to := topLevelKeyword(part, "to"); to >= 0 {
			expressionTokens = part[:to]
			typeTokens := part[to+1:]
			if len(typeTokens) != 4 || lower(typeTokens[0]) != "typeof" || typeTokens[1].Text != "(" || typeTokens[3].Text != ")" {
				errs = append(errs, ParseError{Code: "KQLP0335", Message: "expected to typeof(type)", Span: tokensSpan(part[to:])})
			} else {
				item.Type = lower(typeTokens[2])
			}
		}
		if equal := topLevelToken(expressionTokens, "="); equal >= 0 {
			if equal != 1 || expressionTokens[0].Kind != IdentifierToken {
				errs = append(errs, ParseError{Code: "KQLP0336", Message: "expected name = array expression", Span: tokensSpan(expressionTokens)})
				continue
			}
			item.Name = expressionTokens[0].Text
			expressionTokens = expressionTokens[2:]
		}
		expression, expressionErrs := parseExpressionTokens(expressionTokens)
		item.Expression = expression
		errs = append(errs, expressionErrs...)
		if item.Name == "" {
			if name, ok := expression.(*NameExpression); ok {
				item.Name = name.Name
			}
		}
		spec.Items = append(spec.Items, item)
	}
	if len(spec.Items) == 0 {
		errs = append(errs, ParseError{Code: "KQLP0333", Message: "multi-value expansion requires an expression", Span: tokensSpan(tokens)})
	}
	return spec, errs
}

func parseAs(op Operator, tokens []Token) (Operator, []ParseError) {
	spec := AsSpec{}
	position := 0
	if len(tokens) >= 5 && lower(tokens[0]) == "hint" && tokens[1].Text == "." && lower(tokens[2]) == "materialized" && tokens[3].Text == "=" {
		value := lower(tokens[4])
		if value != "true" && value != "false" {
			op.Body = spec
			return op, []ParseError{{Code: "KQLP0337", Message: "hint.materialized must be true or false", Span: tokens[4].Span}}
		}
		materialized := value == "true"
		spec.Materialized = &materialized
		position = 5
	}
	if position >= len(tokens) || tokens[position].Kind != IdentifierToken || position != len(tokens)-1 {
		op.Body = spec
		return op, []ParseError{{Code: "KQLP0338", Message: "as requires one alias name", Span: op.Span}}
	}
	spec.Name = tokens[position].Text
	op.Body = spec
	return op, nil
}

func parseSampleDistinct(op Operator, tokens []Token) (Operator, []ParseError) {
	of := topLevelKeyword(tokens, "of")
	if of <= 0 || of == len(tokens)-1 {
		op.Body = SampleDistinctSpec{}
		return op, []ParseError{{Code: "KQLP0339", Message: "expected sample-distinct count of column", Span: op.Span}}
	}
	count, errs := parseExpressionTokens(tokens[:of])
	column, columnErrs := parseExpressionTokens(tokens[of+1:])
	if _, ok := column.(*NameExpression); !ok {
		columnErrs = append(columnErrs, ParseError{Code: "KQLP0340", Message: "sample-distinct requires a column name", Span: tokensSpan(tokens[of+1:])})
	}
	op.Body = SampleDistinctSpec{Count: count, Column: column}
	return op, append(errs, columnErrs...)
}

func parseJoin(op Operator, tokens []Token) (Operator, []ParseError) {
	spec := JoinSpec{Kind: "inner"}
	if op.Kind == "join" {
		spec.Kind = "innerunique"
	} else {
		spec.Kind = "leftouter"
	}
	position := 0
	for position+2 < len(tokens) && tokens[position].Kind == IdentifierToken && tokens[position+1].Text == "=" {
		name, value := lower(tokens[position]), lower(tokens[position+2])
		if name == "kind" {
			spec.Kind = value
		}
		position += 3
	}
	conditionAt := topLevelKeyword(tokens[position:], "on")
	conditionKeyword := "on"
	whereAt := topLevelKeyword(tokens[position:], "where")
	if conditionAt < 0 || whereAt >= 0 && whereAt < conditionAt {
		conditionAt = whereAt
		conditionKeyword = "where"
	}
	if conditionAt < 0 {
		op.Body = spec
		return op, []ParseError{{Code: "KQLP0310", Message: "join requires an on condition", Span: op.Span}}
	}
	conditionAt += position
	rightTokens := trimParentheses(tokens[position:conditionAt])
	if len(rightTokens) == 0 {
		op.Body = spec
		return op, []ParseError{{Code: "KQLP0311", Message: "join requires a right input", Span: op.Span}}
	}
	right, errs := parsePipeline(rightTokens)
	spec.Right = right
	conditionTokens := tokens[conditionAt+1:]
	if conditionKeyword == "where" {
		spec.Condition, _ = parseExpressionTokens(conditionTokens)
	} else {
		spec.Conditions, errs = appendExpressions(spec.Conditions, conditionTokens, errs)
	}
	op.Body = spec
	return op, errs
}

func parseUnion(op Operator, tokens []Token) (Operator, []ParseError) {
	spec := UnionSpec{Kind: "outer"}
	var errs []ParseError
	position := 0
	for position+2 < len(tokens) && tokens[position].Kind == IdentifierToken && tokens[position+1].Text == "=" {
		if lower(tokens[position]) == "kind" {
			spec.Kind = lower(tokens[position+2])
		} else {
			errs = append(errs, ParseError{Code: "KQLP0320", Message: "unsupported union parameter " + tokens[position].Text, Span: tokens[position].Span})
		}
		position += 3
	}
	for _, inputTokens := range splitTopLevel(tokens[position:], ",") {
		inputTokens = trimParentheses(inputTokens)
		if len(inputTokens) == 0 {
			errs = append(errs, ParseError{Code: "KQLP0321", Message: "union requires a table expression", Span: op.Span})
			continue
		}
		input, inputErrs := parsePipeline(inputTokens)
		spec.Inputs = append(spec.Inputs, input)
		errs = append(errs, inputErrs...)
	}
	op.Body = spec
	return op, errs
}

func appendExpressions(destination []Expression, tokens []Token, errs []ParseError) ([]Expression, []ParseError) {
	expressions, expressionErrs := parseExpressionList(tokens)
	return append(destination, expressions...), append(errs, expressionErrs...)
}

func trimParentheses(tokens []Token) []Token {
	if len(tokens) < 2 || tokens[0].Text != "(" || tokens[len(tokens)-1].Text != ")" {
		return tokens
	}
	depth := 0
	for i, token := range tokens {
		switch token.Text {
		case "(":
			depth++
		case ")":
			depth--
			if depth == 0 && i != len(tokens)-1 {
				return tokens
			}
		}
	}
	return tokens[1 : len(tokens)-1]
}

func parseSummarize(op Operator, tokens []Token) (Operator, []ParseError) {
	by := topLevelKeyword(tokens, "by")
	aggregateTokens := tokens
	var byTokens []Token
	if by >= 0 {
		aggregateTokens, byTokens = tokens[:by], tokens[by+1:]
	}
	aggregates, errs := parseExpressionList(aggregateTokens)
	groups, groupErrs := parseExpressionList(byTokens)
	errs = append(errs, groupErrs...)
	op.Body = SummarizeSpec{Aggregates: aggregates, By: groups}
	return op, errs
}

func parseTop(op Operator, tokens []Token) (Operator, []ParseError) {
	by := topLevelKeyword(tokens, "by")
	if by <= 0 || by == len(tokens)-1 {
		op.Body = RawSpec{Tokens: tokens}
		return op, []ParseError{{Code: "KQLP0302", Message: "expected 'top count by expression'", Span: op.Span}}
	}
	count, errs := parseExpressionTokens(tokens[:by])
	ordered, orderErrs := parseOrderedList(tokens[by+1:])
	errs = append(errs, orderErrs...)
	spec := TopSpec{Count: count}
	if len(ordered.Expressions) > 0 {
		spec.By = ordered.Expressions[0]
	}
	op.Body = spec
	return op, errs
}

func parseOrderedList(tokens []Token) (SortSpec, []ParseError) {
	var spec SortSpec
	var errs []ParseError
	for _, part := range splitTopLevel(tokens, ",") {
		ordered := OrderedExpression{Direction: "desc", Nulls: "last"}
		end := len(part)
		explicitNulls := false
		if end >= 2 && lower(part[end-2]) == "nulls" && (lower(part[end-1]) == "first" || lower(part[end-1]) == "last") {
			ordered.Nulls = lower(part[end-1])
			explicitNulls = true
			end -= 2
		}
		if end > 0 && (lower(part[end-1]) == "asc" || lower(part[end-1]) == "desc" || lower(part[end-1]) == "granny-asc" || lower(part[end-1]) == "granny-desc") {
			ordered.Direction = lower(part[end-1])
			end--
			if !explicitNulls && (ordered.Direction == "asc" || ordered.Direction == "granny-asc") {
				ordered.Nulls = "first"
			}
		}
		expr, exprErrs := parseExpressionTokens(part[:end])
		errs = append(errs, exprErrs...)
		ordered.Expression = expr
		spec.Expressions = append(spec.Expressions, ordered)
	}
	return spec, errs
}

func parseExpressionList(tokens []Token) ([]Expression, []ParseError) {
	if len(tokens) == 0 {
		return nil, nil
	}
	var expressions []Expression
	var errs []ParseError
	for _, part := range splitTopLevel(tokens, ",") {
		if len(part) == 0 {
			continue
		}
		if equal := topLevelToken(part, "="); equal == 1 && part[0].Kind == IdentifierToken {
			value, valueErrs := parseExpressionTokens(part[2:])
			expressions = append(expressions, &NamedExpression{Name: part[0].Text, Value: value, Span: tokensSpan(part)})
			errs = append(errs, valueErrs...)
			continue
		}
		expr, exprErrs := parseExpressionTokens(part)
		expressions = append(expressions, expr)
		errs = append(errs, exprErrs...)
	}
	return expressions, errs
}

func beginsTabularSource(tokens []Token) bool {
	if len(tokens) == 0 {
		return false
	}
	_, ok := sourceKeywords[lower(tokens[0])]
	return ok
}

func splitTopLevel(tokens []Token, separator string) [][]Token {
	var result [][]Token
	start, depth := 0, 0
	for i, token := range tokens {
		switch token.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 && token.Text == separator {
				result = append(result, tokens[start:i])
				start = i + 1
			}
		}
	}
	result = append(result, tokens[start:])
	return result
}

func topLevelKeyword(tokens []Token, keyword string) int {
	depth := 0
	for i, token := range tokens {
		switch token.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		default:
			if depth == 0 && lower(token) == keyword {
				return i
			}
		}
	}
	return -1
}

func topLevelToken(tokens []Token, text string) int {
	depth := 0
	for i, token := range tokens {
		switch token.Text {
		case "(", "[", "{":
			depth++
		case ")", "]", "}":
			depth--
		default:
			if depth == 0 && token.Text == text {
				return i
			}
		}
	}
	return -1
}

func hasTopLevel(tokens []Token, text string) bool { return topLevelToken(tokens, text) >= 0 }

func tokensSpan(tokens []Token) Span {
	if len(tokens) == 0 {
		return Span{}
	}
	first, last := tokens[0].Span, tokens[len(tokens)-1].Span
	return Span{Offset: first.Offset, Length: last.Offset + last.Length - first.Offset, Line: first.Line, Column: first.Column}
}

func lower(token Token) string { return strings.ToLower(token.Text) }
