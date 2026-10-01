package database

import (
	"encoding/json"
	"fmt"
	"unicode/utf8"
)

func decodeJSONSearchScalar(raw json.RawMessage) (kqlScalar, bool) {
	if string(raw) == "true" || string(raw) == "false" {
		return kqlScalar{kind: 'b', text: string(raw)}, true
	}
	return decodeJSONScalar(raw)
}

func kqlArrayIndexOf(value, lookup any, kind string, options ...any) (any, error) {
	if len(options) > 3 {
		return nil, fmt.Errorf("array_index_of accepts at most 5 arguments")
	}
	elements, ok, err := dynamicArray(value)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	bounds := [3]int64{0, -1, 1}
	for i, option := range options {
		number, ok := option.(int64)
		if !ok {
			return nil, nil
		}
		bounds[i] = number
	}
	start, length, occurrence := bounds[0], bounds[1], bounds[2]
	if length < -1 || occurrence < 0 {
		return nil, nil
	}
	if occurrence == 0 {
		return nil, fmt.Errorf("array_index_of zero occurrence is not yet supported")
	}
	if lookup == nil {
		return nil, nil
	}
	if kind == "datetime" {
		return nil, fmt.Errorf("array search datetime elements require tagged dynamic types")
	}
	if text, ok := lookup.(string); ok {
		if len(text) > maxReplacementBytes {
			return nil, fmt.Errorf("array search value exceeds %d bytes", maxReplacementBytes)
		}
		if !utf8.ValidString(text) {
			return nil, fmt.Errorf("array search value must be valid UTF-8")
		}
	}
	raw, err := packJSONValue(lookup, kind)
	if err != nil {
		return nil, err
	}
	needle, ok := decodeJSONSearchScalar(raw)
	if !ok {
		return nil, fmt.Errorf("array search lookup requires a supported scalar value")
	}
	count := int64(len(elements))
	if start < 0 {
		start += count
	}
	if start < 0 {
		return nil, nil
	}
	if start >= count {
		return int64(-1), nil
	}
	end := count
	if length >= 0 && length < count-start {
		end = start + length
	}
	for i := start; i < end; i++ {
		element, ok := decodeJSONSearchScalar(elements[i])
		if ok && sqliteScalarEqual(element, needle) {
			occurrence--
			if occurrence == 0 {
				return i, nil
			}
		}
	}
	return int64(-1), nil
}

func kqlSetHasElementTyped(value, lookup any, kind string) (any, error) {
	index, err := kqlArrayIndexOf(value, lookup, kind)
	if err != nil || index == nil {
		return nil, err
	}
	return index.(int64) >= 0, nil
}
