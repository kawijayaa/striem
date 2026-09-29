package kql

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

var hyphenatedKeywords = map[string]struct{}{
	"assert-schema": {}, "external-data": {}, "graph-mark-components": {}, "graph-match": {},
	"graph-shortest-paths": {}, "graph-to-table": {}, "graph-where-edges": {},
	"graph-where-nodes": {}, "inline-external-table": {}, "macro-expand": {},
	"make-graph": {}, "make-series": {}, "materialized-view-combine": {}, "mv-apply": {}, "mv-expand": {},
	"parse-kv": {}, "parse-where": {}, "project-away": {}, "project-by-names": {},
	"project-keep": {}, "project-rename": {}, "project-reorder": {},
	"sample-distinct": {}, "top-hitters": {}, "top-nested": {},
}

var wordOperators = map[string]struct{}{
	"!between": {}, "!contains": {}, "!contains_cs": {}, "!containscs": {},
	"!endswith": {}, "!endswith_cs": {}, "!has": {}, "!has_cs": {},
	"!hasprefix": {}, "!hasprefix_cs": {}, "!hassuffix": {},
	"!hassuffix_cs": {}, "!in": {}, "!in~": {}, "!startswith": {},
	"!startswith_cs": {},
}

var symbolicTokens = []string{
	"<-[", "-->", "<--", "-[", "]->", "]-", "->", "<-", "--",
	"!in~", "!~", "=~", "==", "!=", "<>", "<=", ">=", "..",
}

// Lex tokenizes source using KQL's contextual-keyword model.
func Lex(source string) ([]Token, []LexError) {
	l := lexer{source: source, line: 1, column: 1}
	for l.offset < len(source) {
		l.skipTrivia()
		if l.offset >= len(source) {
			break
		}
		l.scanToken()
	}
	l.tokens = append(l.tokens, Token{Kind: EOFToken, Span: l.span(l.offset, l.line, l.column)})
	return l.tokens, l.errors
}

type lexer struct {
	source       string
	offset       int
	line, column int
	tokens       []Token
	errors       []LexError
}

func (l *lexer) scanToken() {
	start, line, column := l.offset, l.line, l.column

	if l.source[l.offset] == '#' && (l.offset == 0 || l.source[l.offset-1] == '\n') {
		for l.offset < len(l.source) && l.source[l.offset] != '\n' {
			l.advanceRune()
		}
		l.emit(DirectiveToken, start, line, column)
		return
	}

	if l.scanString(start, line, column) {
		return
	}

	r, _ := utf8.DecodeRuneInString(l.source[l.offset:])
	if isIdentifierStart(r) {
		l.scanIdentifier(start, line, column)
		return
	}
	if unicode.IsDigit(r) || (r == '.' && l.peekDigit(1)) {
		l.scanNumber(start, line, column)
		return
	}

	for _, text := range symbolicTokens {
		if strings.HasPrefix(l.source[l.offset:], text) {
			l.advanceBytes(len(text))
			l.emit(OperatorToken, start, line, column)
			return
		}
	}

	if r == '!' {
		end := l.offset + 1
		for end < len(l.source) {
			next, size := utf8.DecodeRuneInString(l.source[end:])
			if !isIdentifierContinue(next) && next != '~' {
				break
			}
			end += size
		}
		candidate := strings.ToLower(l.source[l.offset:end])
		if _, ok := wordOperators[candidate]; ok {
			l.advanceBytes(end - l.offset)
			l.emit(OperatorToken, start, line, column)
			return
		}
	}

	const punctuation = "()[]{},.;|:@"
	const operators = "+-*/%=<>!"
	if strings.ContainsRune(punctuation, r) {
		l.advanceRune()
		l.emit(PunctuationToken, start, line, column)
		return
	}
	if strings.ContainsRune(operators, r) {
		l.advanceRune()
		l.emit(OperatorToken, start, line, column)
		return
	}

	l.advanceRune()
	span := l.span(start, line, column)
	l.tokens = append(l.tokens, Token{Kind: InvalidToken, Text: l.source[start:l.offset], Span: span})
	l.errors = append(l.errors, LexError{Message: "invalid character", Span: span})
}

func (l *lexer) scanIdentifier(start, line, column int) {
	l.advanceRune()
	for l.offset < len(l.source) {
		r, _ := utf8.DecodeRuneInString(l.source[l.offset:])
		if !isIdentifierContinue(r) {
			break
		}
		l.advanceRune()
	}
	if l.offset < len(l.source) && l.source[l.offset] == '~' && strings.EqualFold(l.source[start:l.offset], "in") {
		l.advanceRune()
	}

	// Hyphens are part of a token only for grammar-defined compound keywords.
	if l.offset < len(l.source) && l.source[l.offset] == '-' {
		end := l.offset
		for end < len(l.source) {
			r, size := utf8.DecodeRuneInString(l.source[end:])
			if !isIdentifierContinue(r) && r != '-' {
				break
			}
			end += size
		}
		candidate := strings.ToLower(l.source[start:end])
		if _, ok := hyphenatedKeywords[candidate]; ok {
			l.advanceBytes(end - l.offset)
		}
	}

	l.emit(IdentifierToken, start, line, column)
}

func (l *lexer) scanNumber(start, line, column int) {
	if strings.HasPrefix(strings.ToLower(l.source[l.offset:]), "0x") {
		l.advanceBytes(2)
		for l.offset < len(l.source) && isHex(l.source[l.offset]) {
			l.advanceBytes(1)
		}
		l.emit(NumberToken, start, line, column)
		return
	}

	seenDot := false
	for l.offset < len(l.source) {
		c := l.source[l.offset]
		switch {
		case c >= '0' && c <= '9':
			l.advanceBytes(1)
		case c == '.' && !seenDot && !strings.HasPrefix(l.source[l.offset:], ".."):
			seenDot = true
			l.advanceBytes(1)
		default:
			goto exponent
		}
	}

exponent:
	if l.offset < len(l.source) && (l.source[l.offset] == 'e' || l.source[l.offset] == 'E') {
		l.advanceBytes(1)
		if l.offset < len(l.source) && (l.source[l.offset] == '+' || l.source[l.offset] == '-') {
			l.advanceBytes(1)
		}
		for l.offset < len(l.source) && l.source[l.offset] >= '0' && l.source[l.offset] <= '9' {
			l.advanceBytes(1)
		}
	}
	// Timespan suffixes (d, h, m, s, ms, microsecond, tick, and aliases).
	for l.offset < len(l.source) {
		r, _ := utf8.DecodeRuneInString(l.source[l.offset:])
		if !unicode.IsLetter(r) {
			break
		}
		l.advanceRune()
	}
	l.emit(NumberToken, start, line, column)
}

func (l *lexer) scanString(start, line, column int) bool {
	prefix := l.offset
	if l.source[prefix] == 'h' || l.source[prefix] == 'H' {
		prefix++
	}
	verbatim := false
	if prefix < len(l.source) && l.source[prefix] == '@' {
		verbatim = true
		prefix++
	}
	if prefix >= len(l.source) {
		return false
	}

	if strings.HasPrefix(l.source[prefix:], "```") || strings.HasPrefix(l.source[prefix:], "~~~") {
		delimiter := l.source[prefix : prefix+3]
		l.advanceBytes(prefix - l.offset + 3)
		end := strings.Index(l.source[l.offset:], delimiter)
		if end < 0 {
			l.advanceBytes(len(l.source) - l.offset)
			span := l.span(start, line, column)
			l.emit(StringToken, start, line, column)
			l.errors = append(l.errors, LexError{Message: "unterminated multiline string", Span: span})
			return true
		}
		l.advanceBytes(end + 3)
		l.emit(StringToken, start, line, column)
		return true
	}

	quote := l.source[prefix]
	if quote != '\'' && quote != '"' {
		return false
	}
	l.advanceBytes(prefix - l.offset + 1)
	terminated := false
	for l.offset < len(l.source) {
		c := l.source[l.offset]
		if c == quote {
			l.advanceBytes(1)
			if verbatim && l.offset < len(l.source) && l.source[l.offset] == quote {
				l.advanceBytes(1)
				continue
			}
			terminated = true
			break
		}
		if c == '\\' && !verbatim {
			l.advanceBytes(1)
			if l.offset < len(l.source) {
				l.advanceRune()
			}
			continue
		}
		if c == '\n' || c == '\r' {
			break
		}
		l.advanceRune()
	}
	l.emit(StringToken, start, line, column)
	if !terminated {
		l.errors = append(l.errors, LexError{Message: "unterminated string", Span: l.span(start, line, column)})
	}
	return true
}

func (l *lexer) skipTrivia() {
	for l.offset < len(l.source) {
		r, _ := utf8.DecodeRuneInString(l.source[l.offset:])
		if unicode.IsSpace(r) || (l.offset == 0 && r == '\ufeff') {
			l.advanceRune()
			continue
		}
		if strings.HasPrefix(l.source[l.offset:], "//") {
			for l.offset < len(l.source) && l.source[l.offset] != '\n' {
				l.advanceRune()
			}
			continue
		}
		break
	}
}

func (l *lexer) emit(kind TokenKind, start, line, column int) {
	l.tokens = append(l.tokens, Token{Kind: kind, Text: l.source[start:l.offset], Span: l.span(start, line, column)})
}

func (l *lexer) span(start, line, column int) Span {
	return Span{Offset: start, Length: l.offset - start, Line: line, Column: column}
}

func (l *lexer) advanceRune() {
	r, size := utf8.DecodeRuneInString(l.source[l.offset:])
	l.offset += size
	if r == '\n' {
		l.line++
		l.column = 1
	} else {
		l.column++
	}
}

func (l *lexer) advanceBytes(n int) {
	end := l.offset + n
	for l.offset < end {
		l.advanceRune()
	}
}

func (l *lexer) peekDigit(relative int) bool {
	pos := l.offset + relative
	return pos < len(l.source) && l.source[pos] >= '0' && l.source[pos] <= '9'
}

func isIdentifierStart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r)
}

func isIdentifierContinue(r rune) bool {
	return isIdentifierStart(r) || unicode.IsDigit(r)
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}
