package database

import (
	"encoding/base64"
	"strconv"
	"strings"
	"unicode/utf8"
)

func kqlBase64EncodeToString(value any) any {
	text, ok := value.(string)
	if !ok || len(text) > maxDecodeInputBytes || !utf8.ValidString(text) {
		return nil
	}
	return base64.StdEncoding.EncodeToString([]byte(text))
}

func decodeKQLBase64(value any) ([]byte, bool) {
	text, ok := value.(string)
	if !ok || len(text) > base64.StdEncoding.EncodedLen(maxDecodeInputBytes) || strings.ContainsAny(text, "\r\n") {
		return nil, false
	}
	data, err := base64.StdEncoding.Strict().DecodeString(text)
	if err != nil || len(data) > maxDecodeInputBytes || base64.StdEncoding.EncodeToString(data) != text {
		return nil, false
	}
	return data, true
}

func kqlBase64DecodeToArray(value any) any {
	data, ok := decodeKQLBase64(value)
	if !ok {
		return nil
	}
	// A []byte marshals as a base64 string in encoding/json. Emit numeric JSON
	// directly instead, preserving the full unsigned byte domain.
	result := make([]byte, 0, len(data)*4+2)
	result = append(result, '[')
	for i, b := range data {
		if i > 0 {
			result = append(result, ',')
		}
		result = strconv.AppendUint(result, uint64(b), 10)
	}
	result = append(result, ']')
	return string(result)
}

func kqlBase64EncodeFromArray(value any) (any, error) {
	values, valid, err := dynamicArray(value)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, nil
	}
	data := make([]byte, len(values))
	for i, raw := range values {
		number, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 8)
		if err != nil {
			return nil, nil
		}
		data[i] = byte(number)
	}
	return base64.StdEncoding.EncodeToString(data), nil
}
