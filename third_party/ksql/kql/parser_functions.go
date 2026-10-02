package kql

func parseFunctionDefinition(tokens []Token, closeParameters int) (*FunctionDefinition, []ParseError) {
	function := &FunctionDefinition{Span: tokensSpan(tokens)}
	fail := func(message string, span Span) (*FunctionDefinition, []ParseError) {
		if span.Line == 0 {
			span = function.Span
		}
		return function, []ParseError{{Code: "KQLP0110", Message: message, Span: span}}
	}
	openBody := closeParameters + 1
	if matchingClose(tokens, openBody) != len(tokens)-1 {
		return fail("function requires one balanced body", function.Span)
	}
	// Bound recursive declaration parsing before descending into local functions.
	depth := 0
	for _, token := range tokens[openBody:] {
		if token.Text == "{" {
			depth++
			if depth > 64 {
				return fail("function declaration nesting exceeds 64 levels", token.Span)
			}
		} else if token.Text == "}" {
			depth--
		}
	}
	var errs []ParseError
	parameterTokens := tokens[1:closeParameters]
	if len(parameterTokens) > 0 {
		for _, part := range splitTopLevel(parameterTokens, ",") {
			colon := topLevelToken(part, ":")
			if colon < 1 || colon+1 >= len(part) {
				return fail("function parameter requires a name and type", tokensSpan(part))
			}
			name, nameErrors := parseExpressionTokens(part[:colon])
			parameterName, ok := name.(*NameExpression)
			simpleName := colon == 1 && part[0].Kind == IdentifierToken || colon == 3 && part[0].Text == "[" && part[1].Kind == StringToken && part[2].Text == "]"
			if !simpleName || !ok || len(nameErrors) > 0 {
				return fail("function parameter requires an identifier", tokensSpan(part))
			}
			parameter := FunctionParameter{Name: parameterName.Name, Span: tokensSpan(part)}
			typeTokens := part[colon+1:]
			if equal := topLevelToken(typeTokens, "="); equal >= 0 {
				var defaultErrors []ParseError
				parameter.Default, defaultErrors = parseExpressionTokens(typeTokens[equal+1:])
				errs = append(errs, defaultErrors...)
				typeTokens = typeTokens[:equal]
			}
			if len(typeTokens) == 1 && typeTokens[0].Kind == IdentifierToken {
				parameter.Type = typeTokens[0].Text
			} else if len(typeTokens) > 1 && typeTokens[0].Text == "(" && matchingClose(typeTokens, 0) == len(typeTokens)-1 {
				parameter.Type = "tabular"
				schema := typeTokens[1 : len(typeTokens)-1]
				if len(schema) == 1 && schema[0].Text == "*" {
					parameter.Wildcard = true
				} else {
					for _, column := range splitTopLevel(schema, ",") {
						colon := topLevelToken(column, ":")
						if colon < 1 || colon+2 != len(column) || column[colon+1].Kind != IdentifierToken {
							return fail("tabular parameter requires column name:type pairs or (*)", parameter.Span)
						}
						name, errors := parseExpressionTokens(column[:colon])
						identifier, valid := name.(*NameExpression)
						if !valid || len(errors) != 0 || !(colon == 1 && column[0].Kind == IdentifierToken || colon == 3 && column[0].Text == "[" && column[1].Kind == StringToken && column[2].Text == "]") {
							return fail("tabular parameter column requires an identifier", parameter.Span)
						}
						parameter.Columns = append(parameter.Columns, ColumnDefinition{Name: identifier.Name, Type: lower(column[colon+1])})
					}
				}
			} else {
				return fail("invalid function parameter type", parameter.Span)
			}
			function.Parameters = append(function.Parameters, parameter)
		}
	}
	body := tokens[openBody+1 : len(tokens)-1]
	parts := splitTopLevel(body, ";")
	if len(parts) > 0 && len(parts[len(parts)-1]) == 0 {
		parts = parts[:len(parts)-1]
	}
	if len(parts) == 0 || len(parts[len(parts)-1]) == 0 {
		return fail("function body requires a result expression", function.Span)
	}
	for _, part := range parts[:len(parts)-1] {
		if len(part) == 0 || lower(part[0]) != "let" {
			return fail("function body statements must be let declarations", tokensSpan(part))
		}
		statement, statementErrors := parseLet(part)
		errs = append(errs, statementErrors...)
		function.Lets = append(function.Lets, statement.(*LetStatement))
	}
	result := parts[len(parts)-1]
	if lower(result[0]) == "let" {
		return fail("function body requires a result after local declarations", tokensSpan(result))
	}
	if hasTopLevel(result, "|") || beginsTabularSource(result) {
		var resultErrors []ParseError
		function.Pipeline, resultErrors = parsePipeline(result)
		errs = append(errs, resultErrors...)
	} else {
		var resultErrors []ParseError
		function.Result, resultErrors = parseExpressionTokens(result)
		errs = append(errs, resultErrors...)
	}
	return function, errs
}
