package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"unicode/utf8"
)

func extractionGroups(expression *regexp.Regexp, value any) ([]int, error) {
	if sqliteNull(value) {
		groups := make([]int, expression.NumSubexp())
		for i := range groups {
			groups[i] = i + 1
		}
		return groups, nil
	}
	elements, valid, err := dynamicArray(value)
	if err != nil {
		return nil, err
	}
	if !valid || len(elements) == 0 {
		return nil, fmt.Errorf("extract_all capture groups must be a non-empty array")
	}
	groups := make([]int, len(elements))
	for i, element := range elements {
		var name string
		if json.Unmarshal(element, &name) == nil {
			groups[i] = expression.SubexpIndex(name)
		} else {
			var index int64
			if json.Unmarshal(element, &index) != nil || index < 1 || index > int64(expression.NumSubexp()) {
				return nil, fmt.Errorf("extract_all capture index must be from 1 to %d", expression.NumSubexp())
			}
			groups[i] = int(index)
		}
		if groups[i] < 1 {
			return nil, fmt.Errorf("extract_all unknown capture group %q", name)
		}
	}
	return groups, nil
}

func kqlExtractAll(patternValue, groupsValue, sourceValue any) (any, error) {
	if sqliteNull(patternValue) || sqliteNull(sourceValue) {
		return nil, nil
	}
	pattern, patternOK := patternValue.(string)
	source, sourceOK := sourceValue.(string)
	if !patternOK || !sourceOK {
		return nil, fmt.Errorf("extract_all regex and source must be strings")
	}
	if len(pattern) > 512 || len(source) > maxReplacementBytes {
		return nil, fmt.Errorf("extract_all input size limit exceeded")
	}
	if !utf8.ValidString(pattern) || !utf8.ValidString(source) {
		return nil, fmt.Errorf("extract_all inputs must be valid UTF-8")
	}
	expression, err := compileRegexp(pattern)
	if err != nil {
		return nil, err
	}
	if expression.NumSubexp() < 1 || expression.NumSubexp() > maxParseCaptures {
		return nil, fmt.Errorf("extract_all regex requires between 1 and 16 capture groups")
	}
	groups, err := extractionGroups(expression, groupsValue)
	if err != nil {
		return nil, err
	}
	maximum := (1 << 20) / (2 * (expression.NumSubexp() + 1))
	if maximum > maxReplacementParts/len(groups) {
		maximum = maxReplacementParts / len(groups)
	}
	matches := expression.FindAllStringSubmatchIndex(source, maximum+1)
	if len(matches) > maximum {
		return nil, fmt.Errorf("extract_all match or capture limit exceeded")
	}
	if len(matches) == 0 {
		return nil, nil
	}
	var result bytes.Buffer
	result.WriteByte('[')
	for i, match := range matches {
		if i != 0 {
			result.WriteByte(',')
		}
		if len(groups) > 1 {
			result.WriteByte('[')
		}
		for j, group := range groups {
			if j != 0 {
				result.WriteByte(',')
			}
			value := ""
			if begin := match[group*2]; begin >= 0 {
				value = source[begin:match[group*2+1]]
			}
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, err
			}
			// Reserve both closing brackets before growing the result buffer.
			if len(encoded) > maxReplacementBytes-result.Len()-2 {
				return nil, fmt.Errorf("extract_all output exceeds %d bytes", maxReplacementBytes)
			}
			result.Write(encoded)
		}
		if len(groups) > 1 {
			result.WriteByte(']')
		}
	}
	result.WriteByte(']')
	return result.String(), nil
}
