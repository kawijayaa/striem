package database

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxUnicodeCodepoints = 65536

func kqlCodepointsFromString(value any) (any, error) {
	text, ok := value.(string)
	if !ok {
		return nil, nil
	}
	if len(text) > 4*maxUnicodeCodepoints {
		return nil, fmt.Errorf("Unicode input exceeds byte limit")
	}
	if !utf8.ValidString(text) {
		return nil, nil
	}
	result := make([]byte, 0, len(text)+2)
	result = append(result, '[')
	count := 0
	for _, point := range text {
		if count == maxUnicodeCodepoints {
			return nil, fmt.Errorf("Unicode codepoint limit exceeded")
		}
		if count > 0 {
			result = append(result, ',')
		}
		result = strconv.AppendInt(result, int64(point), 10)
		count++
	}
	result = append(result, ']')
	return string(result), nil
}

func kqlCodepointsToString(value any) (any, error) {
	elements, valid, err := dynamicArray(value)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, nil
	}
	if len(elements) > maxUnicodeCodepoints {
		return nil, fmt.Errorf("Unicode codepoint limit exceeded")
	}
	var result strings.Builder
	for _, element := range elements {
		point, err := strconv.ParseInt(strings.TrimSpace(string(element)), 10, 32)
		if err != nil || !utf8.ValidRune(rune(point)) {
			return nil, nil
		}
		result.WriteRune(rune(point))
	}
	return result.String(), nil
}
