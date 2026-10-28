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
	// Microsoft's toguid example accepts hyphens in non-standard positions,
	// so any hyphenation of exactly 32 hex digits converts.
	if strings.Contains(text, "-") {
		if strings.HasPrefix(text, "-") || strings.HasSuffix(text, "-") || strings.Contains(text, "--") {
			return nil
		}
		text = strings.ReplaceAll(text, "-", "")
	}
	if len(text) != 32 {
		return nil
	}
	if _, err := hex.DecodeString(text); err != nil {
		return nil
	}
	text = strings.ToLower(text)
	return text[:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:]
}
