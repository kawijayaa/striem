package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

func kqlParseJSON(value any) (any, error) {
	if value == nil {
		return nil, nil
	}
	text, ok := value.(string)
	if !ok {
		return nil, fmt.Errorf("parse_json requires a string or dynamic input")
	}
	if len(text) > maxReplacementBytes {
		return nil, fmt.Errorf("parse_json input exceeds %d bytes", maxReplacementBytes)
	}
	if !utf8.ValidString(text) {
		return nil, fmt.Errorf("parse_json input must be valid UTF-8")
	}
	if json.Valid([]byte(text)) {
		var compact bytes.Buffer
		if err := json.Compact(&compact, []byte(text)); err != nil {
			return nil, err
		}
		return compact.String(), nil
	}
	// Invalid JSON is a dynamic string, represented as valid JSON text so
	// downstream property access can return null instead of a SQLite JSON error.
	encoded, err := json.Marshal(text)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxReplacementBytes {
		return nil, fmt.Errorf("parse_json encoded string exceeds %d bytes", maxReplacementBytes)
	}
	return string(encoded), nil
}
