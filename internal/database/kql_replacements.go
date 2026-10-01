package database

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

const maxReplacementParts = 65536
const maxReplacementWork = 64 << 20

// Earlier lookups reserve their matches in the original text. Applying
// replacements sequentially would incorrectly search newly inserted text.
type replacementPart struct {
	start, end int
	rewrite    *string
	locked     bool
}

func replacementArray(value any) ([]json.RawMessage, error) {
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return nil, fmt.Errorf("replace_strings requires dynamic arrays")
	}
	if len(text) > maxReplacementBytes {
		return nil, fmt.Errorf("replace_strings array exceeds %d bytes", maxReplacementBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return nil, fmt.Errorf("replace_strings requires dynamic arrays")
	}
	var values []json.RawMessage
	for decoder.More() {
		if len(values) == maxReplacementParts {
			return nil, fmt.Errorf("replace_strings array has too many elements")
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("replace_strings requires valid dynamic arrays")
		}
		values = append(values, value)
	}
	if _, err := decoder.Token(); err != nil {
		return nil, fmt.Errorf("replace_strings requires valid dynamic arrays")
	}
	// Reject trailing JSON/text instead of accepting a valid prefix.
	if _, err := decoder.Token(); err == nil {
		return nil, fmt.Errorf("replace_strings requires valid dynamic arrays")
	} else if err != io.EOF {
		return nil, fmt.Errorf("replace_strings requires valid dynamic arrays")
	}
	return values, nil
}

func kqlReplaceStrings(value, lookups, rewrites any) (any, error) {
	text, ok := value.(string)
	if !ok {
		return nil, nil
	}
	if len(text) > maxReplacementBytes {
		return nil, fmt.Errorf("replace_strings input exceeds %d bytes", maxReplacementBytes)
	}
	searches, err := replacementArray(lookups)
	if err != nil {
		return nil, err
	}
	replacements, err := replacementArray(rewrites)
	if err != nil {
		return nil, err
	}
	if len(searches) != len(replacements) {
		return nil, fmt.Errorf("replace_strings arrays must have equal lengths")
	}
	parts := []replacementPart{{start: 0, end: len(text)}}
	work := maxReplacementWork
	for i, raw := range searches {
		var search string
		// Only JSON strings are lookup values. Decoding null into a Go string
		// succeeds, so require its JSON type before decoding.
		if len(raw) == 0 || raw[0] != '"' {
			continue
		}
		if err := json.Unmarshal(raw, &search); err != nil {
			return nil, err
		}
		if search == "" {
			continue
		}
		var rewrite *string
		if len(replacements[i]) > 0 && replacements[i][0] == '"' {
			var decoded string
			if err := json.Unmarshal(replacements[i], &decoded); err != nil {
				return nil, err
			}
			rewrite = &decoded
		}
		next := make([]replacementPart, 0, len(parts))
		for _, part := range parts {
			work--
			if !part.locked {
				work -= part.end - part.start
			}
			if work < 0 {
				return nil, fmt.Errorf("replace_strings work limit exceeded")
			}
			if part.locked {
				next = append(next, part)
				continue
			}
			start := part.start
			for start < part.end {
				index := strings.Index(text[start:part.end], search)
				if index < 0 {
					break
				}
				index += start
				if index > start {
					next = append(next, replacementPart{start: start, end: index})
				}
				next = append(next, replacementPart{start: index, end: index + len(search), rewrite: rewrite, locked: true})
				if len(next) > maxReplacementParts {
					return nil, fmt.Errorf("replace_strings match limit exceeded")
				}
				start = index + len(search)
			}
			if start < part.end {
				next = append(next, replacementPart{start: start, end: part.end})
			}
			if len(next) > maxReplacementParts {
				return nil, fmt.Errorf("replace_strings match limit exceeded")
			}
		}
		parts = next
	}
	size := 0
	for _, part := range parts {
		length := part.end - part.start
		if part.rewrite != nil {
			length = len(*part.rewrite)
		}
		if length > maxReplacementBytes-size {
			return nil, fmt.Errorf("replace_strings result exceeds %d bytes", maxReplacementBytes)
		}
		size += length
	}
	var result strings.Builder
	result.Grow(size)
	for _, part := range parts {
		if part.rewrite != nil {
			result.WriteString(*part.rewrite)
		} else {
			result.WriteString(text[part.start:part.end])
		}
	}
	return result.String(), nil
}
