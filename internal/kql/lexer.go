package kql

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

func lex(input string) ([]token, error) {
	tokens := make([]token, 0, 32)
	offset, line, column := 0, 1, 1

	for offset < len(input) {
		r, size := utf8.DecodeRuneInString(input[offset:])
		if unicode.IsSpace(r) {
			if r == '\n' {
				line, column = line+1, 1
			} else {
				column++
			}
			offset += size
			continue
		}
		if r == '/' && offset+1 < len(input) && input[offset+1] == '/' {
			for offset < len(input) && input[offset] != '\n' {
				offset++
				column++
			}
			continue
		}

		start, startColumn := offset, column
		switch r {
		case '|':
			tokens = append(tokens, makeToken(tokenPipe, "|", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case '+':
			tokens = append(tokens, makeToken(tokenPlus, "+", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case '-':
			tokens = append(tokens, makeToken(tokenMinus, "-", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case '*':
			tokens = append(tokens, makeToken(tokenStar, "*", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case '/':
			tokens = append(tokens, makeToken(tokenSlash, "/", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case '%':
			tokens = append(tokens, makeToken(tokenPercent, "%", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case ',':
			tokens = append(tokens, makeToken(tokenComma, ",", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case ';':
			tokens = append(tokens, makeToken(tokenSemicolon, ";", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case '.':
			tokens = append(tokens, makeToken(tokenDot, ".", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case '(':
			tokens = append(tokens, makeToken(tokenLeftParen, "(", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case ')':
			tokens = append(tokens, makeToken(tokenRightParen, ")", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case '[':
			tokens = append(tokens, makeToken(tokenLeftBracket, "[", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case ']':
			tokens = append(tokens, makeToken(tokenRightBracket, "]", start, start+1, line, startColumn))
			offset, column = offset+1, column+1
		case '=':
			kind, width := tokenAssign, 1
			if offset+1 < len(input) && input[offset+1] == '=' {
				kind, width = tokenEqual, 2
			}
			tokens = append(tokens, makeToken(kind, input[start:start+width], start, start+width, line, startColumn))
			offset, column = offset+width, column+width
		case '!':
			if offset+1 >= len(input) || input[offset+1] != '=' {
				return nil, &Error{Message: "expected !=", Line: line, Column: column}
			}
			tokens = append(tokens, makeToken(tokenNotEqual, "!=", start, start+2, line, startColumn))
			offset, column = offset+2, column+2
		case '<', '>':
			kind, width := tokenLess, 1
			if r == '>' {
				kind = tokenGreater
			}
			if offset+1 < len(input) && input[offset+1] == '=' {
				width = 2
				if r == '<' {
					kind = tokenLessEqual
				} else {
					kind = tokenGreaterEqual
				}
			}
			tokens = append(tokens, makeToken(kind, input[start:start+width], start, start+width, line, startColumn))
			offset, column = offset+width, column+width
		case '\'', '"':
			quote := byte(r)
			offset++
			column++
			var value strings.Builder
			closed := false
			for offset < len(input) {
				current := input[offset]
				if current == quote {
					if offset+1 < len(input) && input[offset+1] == quote {
						value.WriteByte(quote)
						offset, column = offset+2, column+2
						continue
					}
					offset++
					column++
					closed = true
					break
				}
				if current == '\\' && offset+1 < len(input) {
					next := input[offset+1]
					switch next {
					case 'n':
						value.WriteByte('\n')
					case 'r':
						value.WriteByte('\r')
					case 't':
						value.WriteByte('\t')
					case '\\', '\'', '"':
						value.WriteByte(next)
					default:
						value.WriteByte(next)
					}
					offset, column = offset+2, column+2
					continue
				}
				if current == '\n' {
					return nil, &Error{Message: "unterminated string", Line: line, Column: startColumn}
				}
				value.WriteByte(current)
				offset++
				column++
			}
			if !closed {
				return nil, &Error{Message: "unterminated string", Line: line, Column: startColumn}
			}
			tokens = append(tokens, makeToken(tokenString, value.String(), start, offset, line, startColumn))
		default:
			if unicode.IsLetter(r) || r == '_' {
				offset += size
				column++
				for offset < len(input) {
					next, nextSize := utf8.DecodeRuneInString(input[offset:])
					if !unicode.IsLetter(next) && !unicode.IsDigit(next) && next != '_' {
						break
					}
					offset += nextSize
					column++
				}
				tokens = append(tokens, makeToken(tokenIdentifier, input[start:offset], start, offset, line, startColumn))
				continue
			}
			if unicode.IsDigit(r) {
				offset += size
				column++
				dot := false
				for offset < len(input) {
					next := input[offset]
					if next == '.' && !dot {
						dot = true
						offset, column = offset+1, column+1
						continue
					}
					if next < '0' || next > '9' {
						break
					}
					offset, column = offset+1, column+1
				}
				tokens = append(tokens, makeToken(tokenNumber, input[start:offset], start, offset, line, startColumn))
				continue
			}
			return nil, &Error{Message: fmt.Sprintf("unexpected character %q", r), Line: line, Column: column}
		}
	}

	tokens = append(tokens, token{Kind: tokenEOF, Offset: len(input), End: len(input), Line: line, Column: column})
	return tokens, nil
}

func makeToken(kind tokenKind, text string, start, end, line, column int) token {
	return token{Kind: kind, Text: text, Offset: start, End: end, Line: line, Column: column}
}
