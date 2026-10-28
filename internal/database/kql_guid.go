package database

import (
	"encoding/hex"
	"strings"
)

// kqlToGUID converts the 32-digit, hyphenated, braced and parenthesized GUID
// spellings to lowercase hyphenated text, returning null on failure.
func kqlToGUID(value any) any {
	text, ok := value.(string)
	if !ok || len(text) > 38 {
		return nil
	}
	text = strings.TrimSpace(text)
	if len(text) == 38 && ((text[0] == '{' && text[37] == '}') || (text[0] == '(' && text[37] == ')')) {
		text = text[1:37]
	}
	switch len(text) {
	case 36:
		if text[8] != '-' || text[13] != '-' || text[18] != '-' || text[23] != '-' {
			return nil
		}
		text = text[:8] + text[9:13] + text[14:18] + text[19:23] + text[24:]
	case 32:
	default:
		return nil
	}
	if _, err := hex.DecodeString(text); err != nil {
		return nil
	}
	text = strings.ToLower(text)
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:]
}
