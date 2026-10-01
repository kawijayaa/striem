package database

import "unicode/utf8"

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
