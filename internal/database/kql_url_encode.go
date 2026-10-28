package database

import "unicode/utf8"

func encodeKQLURL(value any, component bool) any {
	text, ok := value.(string)
	if !ok || len(text) > maxDecodeInputBytes || !utf8.ValidString(text) {
		return nil
	}
	const hex = "0123456789abcdef"
	result := make([]byte, 0, len(text))
	for i := 0; i < len(text); i++ {
		b := text[i]
		switch {
		case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9', b == '-', b == '_', b == '.', b == '!', b == '*', b == '(', b == ')':
			result = append(result, b)
		case b == ' ' && !component:
			result = append(result, '+')
		default:
			result = append(result, '%', hex[b>>4], hex[b&15])
		}
	}
	return string(result)
}

func kqlURLEncode(value any) any          { return encodeKQLURL(value, false) }
func kqlURLEncodeComponent(value any) any { return encodeKQLURL(value, true) }
