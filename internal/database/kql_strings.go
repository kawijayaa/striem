package database

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// KQL indexes characters from zero; SQLite substr indexes from one and its
// string functions stop at embedded NUL characters.
func kqlSubstring(value any, offset any, length ...any) any {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	start, ok := offset.(int64)
	if !ok {
		return nil
	}
	if start < 0 {
		start += int64(utf8.RuneCountInString(text))
		if start < 0 {
			start = 0
		}
	}
	size := int64(-1)
	if len(length) > 0 {
		size, ok = length[0].(int64)
		if !ok {
			return nil
		}
		if size <= 0 {
			return ""
		}
	}
	begin := len(text)
	end := len(text)
	position := int64(0)
	remaining := size
	for index := range text {
		if position == start {
			begin = index
		}
		if position >= start && size >= 0 {
			if remaining == 0 {
				end = index
				break
			}
			remaining--
		}
		position++
	}
	return text[begin:end]
}

func kqlStringLength(value any) any {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	return int64(utf8.RuneCountInString(text))
}

// Bound expanded results before allocating. SQLite's replace treats a search
// string beginning with NUL as empty; Go's byte matching preserves that lookup.
const maxReplacementBytes = 4 << 20

func kqlReplaceString(value, lookup, rewrite any) (any, error) {
	text, ok := value.(string)
	if !ok {
		return nil, nil
	}
	search, ok := lookup.(string)
	if !ok {
		return nil, nil
	}
	replacement, ok := rewrite.(string)
	if !ok {
		return nil, nil
	}
	// An empty lookup leaves the original text unchanged.
	if search == "" {
		return text, nil
	}
	count := strings.Count(text, search)
	if count == 0 {
		return text, nil
	}
	// Subtraction cannot overflow: non-overlapping matches consume at most text.
	remaining := len(text) - count*len(search)
	if remaining > maxReplacementBytes || len(replacement) > (maxReplacementBytes-remaining)/count {
		return nil, fmt.Errorf("replace_string result exceeds %d bytes", maxReplacementBytes)
	}
	return strings.ReplaceAll(text, search, replacement), nil
}

// Go's Unicode simple case tables convert complete strings, including text
// following embedded NUL. SQLite's built-in case conversion is ASCII-only.
func kqlCase(value any, upper bool) any {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	if upper {
		return strings.ToUpper(text)
	}
	return strings.ToLower(text)
}
