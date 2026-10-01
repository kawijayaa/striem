package database

import (
	"bytes"
	"encoding/json"
	"fmt"
)

func kqlArrayRotate(value, countValue any, right bool) (any, error) {
	elements, ok, err := dynamicArray(value)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	count, ok := countValue.(int64)
	if !ok {
		return nil, nil
	}
	size := int64(len(elements))
	if size == 0 {
		return "[]", nil
	}
	// Reduce before changing direction so MinInt64 never needs negating.
	offset := count % size
	if offset < 0 {
		offset += size
	}
	if right && offset != 0 {
		offset = size - offset
	}
	var result bytes.Buffer
	result.WriteByte('[')
	for i := int64(0); i < size; i++ {
		if err := appendArrayElement(&result, elements[(i+offset)%size]); err != nil {
			return nil, err
		}
	}
	result.WriteByte(']')
	return result.String(), nil
}

func kqlArrayShift(value, countValue, padding any, kind string, right bool) (any, error) {
	elements, ok, err := dynamicArray(value)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	count, ok := countValue.(int64)
	if !ok {
		return nil, nil
	}
	size := int64(len(elements))
	if size == 0 {
		return "[]", nil
	}
	// Clamp before negation/addition, including huge counts and MinInt64.
	if count > size {
		count = size
	}
	if count < -size {
		count = -size
	}
	if right {
		count = -count
	}
	var fill json.RawMessage
	if kind == "__array_default" {
		if count != 0 {
			for _, element := range elements {
				if len(element) > 0 && element[0] == '"' {
					return nil, fmt.Errorf("array shift default padding for string elements is not yet supported; provide an explicit default value")
				}
			}
		}
		fill = json.RawMessage("null")
	} else {
		fill, err = packJSONValue(padding, kind)
		if err != nil {
			return nil, err
		}
	}
	var result bytes.Buffer
	result.WriteByte('[')
	for i := int64(0); i < size; i++ {
		source := i + count
		element := fill
		if source >= 0 && source < size {
			element = elements[source]
		}
		if err := appendArrayElement(&result, element); err != nil {
			return nil, err
		}
	}
	result.WriteByte(']')
	return result.String(), nil
}
