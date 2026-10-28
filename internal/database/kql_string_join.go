package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

func kqlStrcatDelim(delimiterValue any, values ...any) (any, error) {
	delimiter, ok := delimiterValue.(string)
	if !ok {
		return nil, nil
	}
	if len(delimiter) > maxCharacterStringBytes {
		return nil, fmt.Errorf("delimiter exceeds byte limit")
	}
	if !utf8.ValidString(delimiter) {
		return nil, nil
	}
	if len(values) < 2 || len(values) > 64 {
		return nil, fmt.Errorf("scalar join requires 2 to 64 values")
	}
	var output strings.Builder
	for i, value := range values {
		if value == nil {
			value = ""
		}
		if blob, ok := value.([]byte); ok && blob == nil {
			value = ""
		}
		text, ok := value.(string)
		if !ok || !utf8.ValidString(text) {
			return nil, nil
		}
		if i > 0 {
			if len(delimiter) > maxCharacterStringBytes-output.Len() {
				return nil, fmt.Errorf("scalar join output exceeds byte limit")
			}
			output.WriteString(delimiter)
		}
		if len(text) > maxCharacterStringBytes-output.Len() {
			return nil, fmt.Errorf("scalar join output exceeds byte limit")
		}
		output.WriteString(text)
	}
	return output.String(), nil
}

func kqlStrcatArray(value, delimiterValue any) (any, error) {
	delimiter, ok := delimiterValue.(string)
	if !ok {
		return nil, nil
	}
	if len(delimiter) > maxCharacterStringBytes {
		return nil, fmt.Errorf("array delimiter exceeds byte limit")
	}
	if !utf8.ValidString(delimiter) {
		return nil, nil
	}
	elements, valid, err := dynamicArray(value)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, nil
	}
	var output strings.Builder
	for i, raw := range elements {
		var text string
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) > 0 && trimmed[0] == '"' {
			if err := json.Unmarshal(trimmed, &text); err != nil {
				return nil, nil
			}
		} else if string(trimmed) != "null" {
			// Preserve numeric token digits and compact structured values without
			// converting them through binary64.
			var compact bytes.Buffer
			if json.Compact(&compact, trimmed) != nil {
				return nil, nil
			}
			text = compact.String()
		}
		if i > 0 {
			if len(delimiter) > maxCharacterStringBytes-output.Len() {
				return nil, fmt.Errorf("array concatenation output exceeds byte limit")
			}
			output.WriteString(delimiter)
		}
		if len(text) > maxCharacterStringBytes-output.Len() {
			return nil, fmt.Errorf("array concatenation output exceeds byte limit")
		}
		output.WriteString(text)
	}
	return output.String(), nil
}

func kqlStrrep(value, countValue, delimiterValue any) (any, error) {
	text, ok := value.(string)
	delimiter, valid := delimiterValue.(string)
	count, countOK := countValue.(int64)
	if !ok || !valid || !countOK || count < 1 || count > 67108864 {
		return nil, nil
	}
	if len(text) > maxCharacterStringBytes || len(delimiter) > maxCharacterStringBytes {
		return nil, fmt.Errorf("string replication input exceeds byte limit")
	}
	if !utf8.ValidString(text) || !utf8.ValidString(delimiter) {
		return nil, nil
	}
	if count > 1024 {
		count = 1024
	}
	// Both multiplicands are bounded before this check. Count is at most 1024.
	size := int64(len(text))*count + int64(len(delimiter))*(count-1)
	if size > maxCharacterStringBytes {
		return nil, fmt.Errorf("string replication output exceeds byte limit")
	}
	if len(text) == 0 && len(delimiter) == 0 {
		return "", nil
	}
	var output strings.Builder
	output.Grow(int(size))
	for i := int64(0); i < count; i++ {
		if i > 0 {
			output.WriteString(delimiter)
		}
		output.WriteString(text)
	}
	return output.String(), nil
}
