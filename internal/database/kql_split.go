package database

import (
	"encoding/json"
	"fmt"
	"strings"
)

func kqlSplit(value, delimiter any, requested ...any) (any, error) {
	text, ok := value.(string)
	if !ok {
		return nil, nil
	}
	separator, ok := delimiter.(string)
	if !ok {
		return nil, nil
	}
	if separator == "" {
		return nil, fmt.Errorf("split delimiter cannot be empty")
	}
	if len(text) > maxReplacementBytes || len(separator) > maxReplacementBytes {
		return nil, fmt.Errorf("split input exceeds %d bytes", maxReplacementBytes)
	}
	if len(requested) > 1 {
		return nil, fmt.Errorf("split accepts at most 3 arguments")
	}
	var values []string
	if len(requested) == 1 {
		index, ok := requested[0].(int64)
		if !ok {
			return nil, nil
		}
		if index < 0 {
			return "[]", nil
		}
		remaining := text
		for position := int64(0); ; position++ {
			part, tail, found := strings.Cut(remaining, separator)
			if position == index {
				values = []string{part}
				break
			}
			if !found {
				return "[]", nil
			}
			remaining = tail
		}
	} else {
		values = strings.SplitN(text, separator, maxReplacementParts+1)
		if len(values) > maxReplacementParts {
			return nil, fmt.Errorf("split element limit exceeded")
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxReplacementBytes {
		return nil, fmt.Errorf("split result exceeds %d bytes", maxReplacementBytes)
	}
	return string(encoded), nil
}
