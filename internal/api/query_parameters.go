package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

type localParameterValues map[string]json.RawMessage

// Keep duplicate names visible instead of silently taking the last JSON value.
// Values stay raw so long integers and dynamic JSON never pass through float64.
func (values *localParameterValues) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*values = nil
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return errors.New("parameters must be an object")
	}
	result := make(localParameterValues)
	for decoder.More() {
		if len(result) >= 1024 {
			return errors.New("too many parameters")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		name, ok := token.(string)
		if !ok {
			return errors.New("invalid parameter name")
		}
		if _, exists := result[name]; exists {
			return errors.New("duplicate parameter name")
		}
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			return err
		}
		result[name] = raw
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return errors.New("invalid parameters object")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing parameter data")
	}
	*values = result
	return nil
}

func localQueryParameters(values localParameterValues) map[string]any {
	result := make(map[string]any, len(values))
	for name, value := range values {
		result[name] = value
	}
	return result
}
