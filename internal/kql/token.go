package kql

import "fmt"

type tokenKind int

const (
	tokenEOF tokenKind = iota
	tokenIdentifier
	tokenString
	tokenNumber
	tokenPipe
	tokenComma
	tokenSemicolon
	tokenDot
	tokenLeftParen
	tokenRightParen
	tokenLeftBracket
	tokenRightBracket
	tokenAssign
	tokenEqual
	tokenNotEqual
	tokenLess
	tokenLessEqual
	tokenGreater
	tokenGreaterEqual
	tokenPlus
	tokenMinus
	tokenStar
	tokenSlash
	tokenPercent
)

type token struct {
	Kind   tokenKind
	Text   string
	Offset int
	End    int
	Line   int
	Column int
}

type Error struct {
	Message string `json:"message"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.Line, e.Column, e.Message)
}

func errorAt(tok token, format string, args ...any) error {
	return &Error{Message: fmt.Sprintf(format, args...), Line: tok.Line, Column: tok.Column}
}
