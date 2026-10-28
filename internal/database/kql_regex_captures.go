package database

import (
	"encoding/json"
	"fmt"
)

const maxRegexCaptureGroups = 256

// kqlRegexCaptures returns the first match's capture groups as a JSON array,
// with group one at index zero, JSON null for groups that did not participate
// and SQL null when the value does not match. It backs parse and parse-where.
func kqlRegexCaptures(valueValue, patternValue any) (any, error) {
	value, valueOK := valueValue.(string)
	pattern, patternOK := patternValue.(string)
	if bytes, ok := valueValue.([]byte); ok {
		value, valueOK = string(bytes), true
	}
	if !valueOK || !patternOK {
		return nil, nil
	}
	if len(pattern) > maxParsePattern {
		return nil, fmt.Errorf("regular expression exceeds %d bytes", maxParsePattern)
	}
	expression, err := compileRegexp(pattern)
	if err != nil {
		return nil, err
	}
	if expression.NumSubexp() > maxRegexCaptureGroups {
		return nil, fmt.Errorf("regular expression exceeds %d capture groups", maxRegexCaptureGroups)
	}
	match := expression.FindStringSubmatchIndex(value)
	if match == nil {
		return nil, nil
	}
	groups := make([]any, 0, expression.NumSubexp())
	for i := 2; i < len(match); i += 2 {
		if match[i] < 0 {
			groups = append(groups, nil)
		} else {
			groups = append(groups, value[match[i]:match[i+1]])
		}
	}
	encoded, err := json.Marshal(groups)
	if err != nil {
		return nil, err
	}
	return string(encoded), nil
}
