package kql

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// DecodeString decodes a single KQL string token, including verbatim and
// obfuscated forms. Both quote styles enclose strings, never Go characters.
func DecodeString(value string) (string, error) {
	if len(value) > 1 && (value[0] == 'h' || value[0] == 'H') {
		value = value[1:]
	}
	verbatim := strings.HasPrefix(value, "@")
	if verbatim {
		value = value[1:]
	}
	if len(value) >= 6 && ((strings.HasPrefix(value, "```") && strings.HasSuffix(value, "```")) || (strings.HasPrefix(value, "~~~") && strings.HasSuffix(value, "~~~"))) {
		return value[3 : len(value)-3], nil
	}
	if len(value) < 2 || (value[0] != '\'' && value[0] != '"') || value[len(value)-1] != value[0] {
		return "", fmt.Errorf("invalid string literal")
	}
	quote := value[0]
	text := value[1 : len(value)-1]
	if verbatim {
		return strings.ReplaceAll(text, string([]byte{quote, quote}), string(quote)), nil
	}
	var result strings.Builder
	result.Grow(len(text))
	for len(text) > 0 {
		if text[0] != '\\' {
			r, n := utf8.DecodeRuneInString(text)
			if r == utf8.RuneError && n == 1 {
				return "", fmt.Errorf("invalid UTF-8 in string literal")
			}
			result.WriteRune(r)
			text = text[n:]
			continue
		}
		if len(text) < 2 {
			return "", fmt.Errorf("incomplete string escape")
		}
		escaped := text[1]
		text = text[2:]
		switch escaped {
		case '\\', '\'', '"':
			result.WriteByte(escaped)
		case 'a':
			result.WriteByte('\a')
		case 'b':
			result.WriteByte('\b')
		case 'f':
			result.WriteByte('\f')
		case 'n':
			result.WriteByte('\n')
		case 'r':
			result.WriteByte('\r')
		case 't':
			result.WriteByte('\t')
		case 'v':
			result.WriteByte('\v')
		case 'x', 'u', 'U':
			digits := 2
			if escaped == 'u' {
				digits = 4
			} else if escaped == 'U' {
				digits = 8
			}
			if len(text) < digits {
				return "", fmt.Errorf("incomplete hexadecimal string escape")
			}
			number, err := strconv.ParseUint(text[:digits], 16, 32)
			if err != nil {
				return "", fmt.Errorf("invalid hexadecimal string escape")
			}
			text = text[digits:]
			r := rune(number)
			if escaped == 'u' && r >= 0xD800 && r <= 0xDBFF && len(text) >= 6 && strings.HasPrefix(text, `\u`) {
				second, err := strconv.ParseUint(text[2:6], 16, 16)
				if err == nil && second >= 0xDC00 && second <= 0xDFFF {
					r = utf16.DecodeRune(r, rune(second))
					text = text[6:]
				}
			}
			if !utf8.ValidRune(r) {
				return "", fmt.Errorf("invalid Unicode string escape")
			}
			result.WriteRune(r)
		default:
			if escaped < '0' || escaped > '7' {
				return "", fmt.Errorf("invalid string escape")
			}
			number := int(escaped - '0')
			for i := 1; i < 3 && len(text) > 0 && text[0] >= '0' && text[0] <= '7'; i++ {
				if i == 2 && escaped > '3' {
					break
				}
				number = number*8 + int(text[0]-'0')
				text = text[1:]
			}
			result.WriteRune(rune(number))
		}
	}
	return result.String(), nil
}

// StringValue decodes all adjacent tokens making up one string expression.
func StringValue(literal *LiteralExpression) (string, error) {
	parts := literal.Parts
	if len(parts) == 0 {
		return DecodeString(literal.Text)
	}
	var result strings.Builder
	for _, part := range parts {
		decoded, err := DecodeString(part)
		if err != nil {
			return "", err
		}
		result.WriteString(decoded)
	}
	return result.String(), nil
}
