package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Decode elements without converting JSON numbers to floating point or
// allocating an unbounded slice for arrays containing many tiny elements.
func dynamicArray(value any) ([]json.RawMessage, bool, error) {
	text, ok := value.(string)
	if !ok {
		return nil, false, nil
	}
	if len(text) > maxReplacementBytes {
		return nil, false, fmt.Errorf("array input exceeds %d bytes", maxReplacementBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('[') {
		return nil, false, nil
	}
	elements := make([]json.RawMessage, 0)
	for decoder.More() {
		if len(elements) == maxReplacementParts {
			return nil, false, fmt.Errorf("array element limit exceeded")
		}
		var element json.RawMessage
		if err := decoder.Decode(&element); err != nil {
			return nil, false, nil
		}
		elements = append(elements, element)
	}
	token, err = decoder.Token()
	if err != nil || token != json.Delim(']') {
		return nil, false, nil
	}
	if _, err := decoder.Token(); err != io.EOF {
		return nil, false, nil
	}
	return elements, true, nil
}

func appendArrayElement(result *bytes.Buffer, element json.RawMessage) error {
	var compact bytes.Buffer
	if err := json.Compact(&compact, element); err != nil {
		return err
	}
	extra := compact.Len()
	if result.Len() > 1 {
		extra++
	}
	// Reserve the closing bracket before growing the output.
	if extra > maxReplacementBytes-result.Len()-1 {
		return fmt.Errorf("array result exceeds %d bytes", maxReplacementBytes)
	}
	if result.Len() > 1 {
		result.WriteByte(',')
	}
	result.Write(compact.Bytes())
	return nil
}

func kqlArrayConcat(values ...any) (any, error) {
	if len(values) < 1 || len(values) > 64 {
		return nil, fmt.Errorf("array_concat requires 1 to 64 arrays")
	}
	var result bytes.Buffer
	result.WriteByte('[')
	count, work := 0, maxReplacementWork
	for _, value := range values {
		if text, ok := value.(string); ok {
			work -= len(text)
			if work < 0 {
				return nil, fmt.Errorf("array_concat work limit exceeded")
			}
		}
		elements, ok, err := dynamicArray(value)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		if len(elements) > maxReplacementParts-count {
			return nil, fmt.Errorf("array_concat element limit exceeded")
		}
		count += len(elements)
		for _, element := range elements {
			if err := appendArrayElement(&result, element); err != nil {
				return nil, err
			}
		}
	}
	result.WriteByte(']')
	return result.String(), nil
}

func kqlArrayReverse(value any) (any, error) {
	elements, ok, err := dynamicArray(value)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	var result bytes.Buffer
	result.WriteByte('[')
	for i := len(elements) - 1; i >= 0; i-- {
		if err := appendArrayElement(&result, elements[i]); err != nil {
			return nil, err
		}
	}
	result.WriteByte(']')
	return result.String(), nil
}
