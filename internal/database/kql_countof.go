package database

import (
	"fmt"
	"unicode/utf8"
)

func kqlCountOf(sourceValue, searchValue, kindValue any) (any, error) {
	source, ok := sourceValue.(string)
	search, valid := searchValue.(string)
	kind, kindOK := kindValue.(string)
	if !ok || !valid || !kindOK {
		return nil, nil
	}
	if len(source) > maxCharacterStringBytes || len(search) > maxParseSource {
		return nil, fmt.Errorf("countof input exceeds byte limit")
	}
	if !utf8.ValidString(source) || !utf8.ValidString(search) {
		return nil, nil
	}
	switch kind {
	case "normal":
		if len(search) == 0 {
			return int64(0), nil
		}
		if len(search) > len(source) {
			return int64(0), nil
		}
		// KMP counts overlaps without rescanning long repeated prefixes.
		prefix := make([]int, len(search))
		for i, j := 1, 0; i < len(search); i++ {
			for j > 0 && search[i] != search[j] {
				j = prefix[j-1]
			}
			if search[i] == search[j] {
				j++
			}
			prefix[i] = j
		}
		count := int64(0)
		for i, j := 0, 0; i < len(source); i++ {
			for j > 0 && source[i] != search[j] {
				j = prefix[j-1]
			}
			if source[i] == search[j] {
				j++
			}
			if j == len(search) {
				count++
				j = prefix[j-1]
			}
		}
		return count, nil
	case "regex":
		if len(source) > maxParseSource || len(search) > maxParsePattern {
			return nil, fmt.Errorf("countof regex input exceeds byte limit")
		}
		expression, err := compileRegexp(search)
		if err != nil {
			return nil, fmt.Errorf("invalid countof regular expression: %w", err)
		}
		// The source-byte bound limits this allocation to 65,537 matches, including
		// zero-width matches. FindAll preserves whole-source anchor semantics.
		return int64(len(expression.FindAllStringIndex(source, -1))), nil
	default:
		return nil, fmt.Errorf("countof kind must be normal or regex")
	}
}
