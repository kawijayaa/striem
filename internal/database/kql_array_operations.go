package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
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

func kqlArraySlice(value, startValue, endValue any) (any, error) {
	elements, ok, err := dynamicArray(value)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	start, startOK := startValue.(int64)
	end, endOK := endValue.(int64)
	if !startOK || !endOK {
		return nil, nil
	}
	count := int64(len(elements))
	// Adding the nonnegative bounded length to a negative index cannot overflow.
	if start < 0 {
		start += count
	}
	if end < 0 {
		end += count
	}
	if start < 0 {
		start = 0
	}
	if end >= count {
		end = count - 1
	}
	if end < 0 || start >= count || start > end {
		return "[]", nil
	}
	var result bytes.Buffer
	result.WriteByte('[')
	// Clamp before making the inclusive end exclusive, including MaxInt64 input.
	for _, element := range elements[int(start) : int(end)+1] {
		if err := appendArrayElement(&result, element); err != nil {
			return nil, err
		}
	}
	result.WriteByte(']')
	return result.String(), nil
}

func kqlZip(values ...any) (any, error) {
	if len(values) < 2 || len(values) > 64 {
		return nil, fmt.Errorf("zip requires 2 to 64 arrays")
	}
	arrays := make([][]json.RawMessage, 0, len(values))
	length, work := 0, maxReplacementWork
	for _, value := range values {
		if text, ok := value.(string); ok {
			work -= len(text)
			if work < 0 {
				return nil, fmt.Errorf("zip work limit exceeded")
			}
		}
		elements, ok, err := dynamicArray(value)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		if len(elements) > length {
			length = len(elements)
		}
		// Count padded cells as well as real elements, before multiplying lengths.
		if length > maxReplacementParts/len(values) {
			return nil, fmt.Errorf("zip cell limit exceeded")
		}
		arrays = append(arrays, elements)
	}
	var result bytes.Buffer
	result.WriteByte('[')
	for i := 0; i < length; i++ {
		var row bytes.Buffer
		row.WriteByte('[')
		for _, array := range arrays {
			element := json.RawMessage("null")
			if i < len(array) {
				element = array[i]
			}
			if err := appendArrayElement(&row, element); err != nil {
				return nil, err
			}
		}
		row.WriteByte(']')
		if err := appendArrayElement(&result, row.Bytes()); err != nil {
			return nil, err
		}
	}
	result.WriteByte(']')
	return result.String(), nil
}

func kqlArraySplit(value, indicesValue any) (any, error) {
	elements, ok, err := dynamicArray(value)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	indices := make([]int64, 0)
	if index, ok := indicesValue.(int64); ok {
		indices = append(indices, index)
	} else {
		rawIndices, ok, err := dynamicArray(indicesValue)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("array_split requires an integer or a dynamic integer array")
		}
		if len(rawIndices) >= maxReplacementParts {
			return nil, fmt.Errorf("array_split segment limit exceeded")
		}
		for _, raw := range rawIndices {
			index, err := strconv.ParseInt(string(raw), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("array_split indices must be signed long integers")
			}
			indices = append(indices, index)
		}
	}
	size := int64(len(elements))
	previous := int64(0)
	for i, index := range indices {
		if index < 0 {
			index += size
		}
		if index < 0 || index > size {
			return nil, fmt.Errorf("array_split out-of-range split points are not yet supported")
		}
		if index < previous {
			return nil, fmt.Errorf("array_split descending split points are not yet supported")
		}
		indices[i] = index
		previous = index
	}
	indices = append(indices, size)
	var result bytes.Buffer
	result.WriteByte('[')
	previous = 0
	for _, index := range indices {
		var segment bytes.Buffer
		segment.WriteByte('[')
		for _, element := range elements[int(previous):int(index)] {
			if err := appendArrayElement(&segment, element); err != nil {
				return nil, err
			}
		}
		segment.WriteByte(']')
		if err := appendArrayElement(&result, segment.Bytes()); err != nil {
			return nil, err
		}
		previous = index
	}
	result.WriteByte(']')
	return result.String(), nil
}

func kqlArraySum(value any) (any, error) {
	elements, ok, err := dynamicArray(value)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	sum := float64(0)
	overflow := false
	for _, element := range elements {
		if len(element) == 0 || element[0] != '-' && (element[0] < '0' || element[0] > '9') {
			return nil, nil
		}
		number, err := strconv.ParseFloat(string(element), 64)
		if err != nil {
			overflow = true
			continue
		}
		sum += number
		if math.IsInf(sum, 0) || math.IsNaN(sum) {
			overflow = true
		}
	}
	// A later nonnumeric element still returns null as documented, even if an
	// earlier numeric element exceeded the current finite-real representation.
	if overflow {
		return nil, fmt.Errorf("array/series sum non-finite results are not yet supported")
	}
	return sum, nil
}
