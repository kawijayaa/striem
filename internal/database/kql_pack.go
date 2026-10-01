package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func packJSONValue(value any, kind string) (json.RawMessage, error) {
	if value == nil {
		return json.RawMessage("null"), nil
	}
	if text, ok := value.(string); ok && len(text) > maxReplacementBytes {
		return nil, fmt.Errorf("pack input exceeds %d bytes", maxReplacementBytes)
	}
	if kind == "bool" {
		if number, ok := value.(int64); ok {
			value = number != 0
		}
	}
	if kind == "string" {
		value = kqlScalarText(value, kind)
	}
	if kind == "dynamic" {
		if text, ok := value.(string); ok && json.Valid([]byte(text)) {
			var compact bytes.Buffer
			if err := json.Compact(&compact, []byte(text)); err != nil {
				return nil, err
			}
			return compact.Bytes(), nil
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxReplacementBytes {
		return nil, fmt.Errorf("pack encoded value exceeds %d bytes", maxReplacementBytes)
	}
	return encoded, nil
}

func kqlPackArray(types string, values ...any) (string, error) {
	kinds := strings.Split(types, ",")
	if len(values) == 0 || len(values) > 64 || len(kinds) != len(values) {
		return "", fmt.Errorf("pack_array requires 1 to 64 typed values")
	}
	packed := make([]json.RawMessage, len(values))
	size := 2
	for i, value := range values {
		encoded, err := packJSONValue(value, kinds[i])
		if err != nil {
			return "", err
		}
		extra := len(encoded)
		if i > 0 {
			extra++
		}
		if extra > maxReplacementBytes-size {
			return "", fmt.Errorf("pack_array result exceeds %d bytes", maxReplacementBytes)
		}
		size += extra
		packed[i] = encoded
	}
	encoded, err := json.Marshal(packed)
	if len(encoded) > maxReplacementBytes {
		return "", fmt.Errorf("pack result exceeds %d bytes", maxReplacementBytes)
	}
	return string(encoded), err
}

func kqlBagPack(types string, values ...any) (string, error) {
	kinds := strings.Split(types, ",")
	if len(values) == 0 || len(values)%2 != 0 || len(values) > 128 || len(kinds) != len(values)/2 {
		return "", fmt.Errorf("bag_pack requires 1 to 64 typed key/value pairs")
	}
	packed := make(map[string]json.RawMessage, len(kinds))
	size := 2
	for i := 0; i < len(values); i += 2 {
		key, ok := values[i].(string)
		if !ok {
			return "", fmt.Errorf("bag_pack keys must be strings")
		}
		if len(key) > maxReplacementBytes {
			return "", fmt.Errorf("bag_pack key exceeds %d bytes", maxReplacementBytes)
		}
		if _, exists := packed[key]; exists {
			return "", fmt.Errorf("bag_pack duplicate keys are not yet supported")
		}
		encoded, err := packJSONValue(values[i+1], kinds[i/2])
		if err != nil {
			return "", err
		}
		encodedKey, _ := json.Marshal(key)
		extra := len(encodedKey) + 1 + len(encoded)
		if i > 0 {
			extra++
		}
		if extra > maxReplacementBytes-size {
			return "", fmt.Errorf("bag_pack result exceeds %d bytes", maxReplacementBytes)
		}
		size += extra
		packed[key] = encoded
	}
	encoded, err := json.Marshal(packed)
	if len(encoded) > maxReplacementBytes {
		return "", fmt.Errorf("pack result exceeds %d bytes", maxReplacementBytes)
	}
	return string(encoded), err
}
