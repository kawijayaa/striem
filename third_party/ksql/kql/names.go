package kql

import "strconv"

// declarationName recognizes a single identifier or a bracketed string name.
// Keep quoted names encoded, so dots inside them are never parsed as paths.
// Microsoft also permits adjacent string tokens inside bracketed names.
func declarationName(tokens []Token) (string, bool) {
	if len(tokens) == 1 && tokens[0].Kind == IdentifierToken {
		return tokens[0].Text, true
	}
	if len(tokens) < 3 || tokens[0].Text != "[" || tokens[len(tokens)-1].Text != "]" {
		return "", false
	}
	parts := make([]string, 0, len(tokens)-2)
	for _, token := range tokens[1 : len(tokens)-1] {
		if token.Kind != StringToken {
			return "", false
		}
		parts = append(parts, token.Text)
	}
	name, err := StringValue(&LiteralExpression{Parts: parts})
	if err != nil {
		return "", false
	}
	if len(parts) == 1 {
		return parts[0], true
	}
	return strconv.Quote(name), true
}
