package kql

import "fmt"

// TokenKind identifies a lexical token category. KQL keywords are contextual,
// so they intentionally remain Identifier tokens and are interpreted by the
// parser according to position.
type TokenKind uint8

const (
	InvalidToken TokenKind = iota
	EOFToken
	IdentifierToken
	NumberToken
	StringToken
	DirectiveToken
	PunctuationToken
	OperatorToken
)

// Span identifies a byte range and its one-based source position.
type Span struct {
	Offset int
	Length int
	Line   int
	Column int
}

func (s Span) String() string {
	return fmt.Sprintf("%d:%d", s.Line, s.Column)
}

// Token is a lossless KQL lexical token. Text refers to the original spelling.
type Token struct {
	Kind TokenKind
	Text string
	Span Span
}

// LexError describes an invalid lexical sequence.
type LexError struct {
	Message string
	Span    Span
}

func (e LexError) Error() string {
	return fmt.Sprintf("%s at %s", e.Message, e.Span)
}
