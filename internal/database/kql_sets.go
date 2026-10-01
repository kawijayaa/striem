package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
)

const maxSetKeyDepth = 64
const maxSetStorageBytes = 2 * maxReplacementBytes

func setValueKey(raw json.RawMessage, work *int) (string, error) {
	*work -= len(raw)
	if *work < 0 {
		return "", fmt.Errorf("set comparison work limit exceeded")
	}
	// Scalar arrays are common; avoid constructing a decoder per element.
	if len(raw) > 0 && raw[0] != '[' && raw[0] != '{' {
		key := "z"
		if string(raw) != "null" {
			scalar, ok := decodeJSONSearchScalar(raw)
			if !ok {
				return "", fmt.Errorf("set scalar representation is not yet supported")
			}
			switch scalar.kind {
			case 's':
				key = "s" + scalar.text
			case 'b':
				if scalar.text == "true" {
					key = "b1"
				} else {
					key = "b0"
				}
			case 'n':
				sign := "+"
				if scalar.number.negative {
					sign = "-"
				}
				key = "n" + sign + scalar.number.digits + ":" + strconv.FormatInt(scalar.number.scale, 10)
			}
		}
		*work -= len(key) + 1
		if *work < 0 {
			return "", fmt.Errorf("set comparison work limit exceeded")
		}
		return key, nil
	}

	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	nodes := maxReplacementParts
	key, err := readSetKey(decoder, 0, work, &nodes)
	if err != nil {
		return "", err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return "", fmt.Errorf("invalid set JSON value")
	}
	return key, nil
}

func frameSetKey(buffer *bytes.Buffer, key string) error {
	header := strconv.Itoa(len(key))
	if len(header)+1+len(key) > maxSetStorageBytes-buffer.Len()-1 {
		return fmt.Errorf("set comparison key limit exceeded")
	}
	buffer.WriteString(header)
	buffer.WriteByte(':')
	buffer.WriteString(key)
	return nil
}

func readSetKey(decoder *json.Decoder, depth int, work, nodes *int) (string, error) {
	if depth > maxSetKeyDepth {
		return "", fmt.Errorf("set value nesting limit exceeded")
	}
	*nodes--
	if *nodes < 0 {
		return "", fmt.Errorf("set value node limit exceeded")
	}
	*work--
	if *work < 0 {
		return "", fmt.Errorf("set comparison work limit exceeded")
	}
	token, err := decoder.Token()
	if err != nil {
		return "", err
	}
	var key string
	switch value := token.(type) {
	case nil:
		key = "z"
	case bool:
		if value {
			key = "b1"
		} else {
			key = "b0"
		}
	case string:
		key = "s" + value
	case json.Number:
		number, ok := normalizeNumber(string(value))
		if !ok {
			return "", fmt.Errorf("set numeric representation is not yet supported")
		}
		sign := "+"
		if number.negative {
			sign = "-"
		}
		key = "n" + sign + number.digits + ":" + strconv.FormatInt(number.scale, 10)
	case json.Delim:
		var buffer bytes.Buffer
		switch value {
		case '[':
			buffer.WriteString("a[")
			for decoder.More() {
				child, err := readSetKey(decoder, depth+1, work, nodes)
				if err != nil {
					return "", err
				}
				if err := frameSetKey(&buffer, child); err != nil {
					return "", err
				}
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim(']') {
				return "", fmt.Errorf("invalid set array")
			}
			buffer.WriteByte(']')
		case '{':
			properties := make(map[string]string)
			names := make([]string, 0)
			for decoder.More() {
				token, err := decoder.Token()
				if err != nil {
					return "", err
				}
				name, ok := token.(string)
				if !ok {
					return "", fmt.Errorf("invalid set object key")
				}
				if _, exists := properties[name]; exists {
					return "", fmt.Errorf("set object duplicate keys are not yet supported")
				}
				child, err := readSetKey(decoder, depth+1, work, nodes)
				if err != nil {
					return "", err
				}
				properties[name] = child
				names = append(names, name)
			}
			closing, err := decoder.Token()
			if err != nil || closing != json.Delim('}') {
				return "", fmt.Errorf("invalid set object")
			}
			sort.Strings(names)
			buffer.WriteString("o{")
			for _, name := range names {
				if err := frameSetKey(&buffer, name); err != nil {
					return "", err
				}
				if err := frameSetKey(&buffer, properties[name]); err != nil {
					return "", err
				}
			}
			buffer.WriteByte('}')
		default:
			return "", fmt.Errorf("invalid set container")
		}
		key = buffer.String()
	default:
		return "", fmt.Errorf("unsupported set value")
	}
	*work -= len(key)
	if *work < 0 {
		return "", fmt.Errorf("set comparison work limit exceeded")
	}
	return key, nil
}

type dynamicSet struct {
	values map[string]json.RawMessage
	order  []string
	size   int
}

func (set *dynamicSet) add(key string, raw json.RawMessage) error {
	if _, exists := set.values[key]; exists {
		return nil
	}
	if len(set.values) == maxReplacementParts {
		return fmt.Errorf("set distinct value limit exceeded")
	}
	size := len(key) + len(raw)
	if size > maxSetStorageBytes-set.size {
		return fmt.Errorf("set storage limit exceeded")
	}
	set.values[key] = raw
	set.order = append(set.order, key)
	set.size += size
	return nil
}

func kqlSetOperation(operation string, values ...any) (any, error) {
	if len(values) < 2 || len(values) > 64 {
		return nil, fmt.Errorf("%s requires 2 to 64 arrays", operation)
	}
	if operation != "set_union" && operation != "set_intersect" && operation != "set_difference" {
		return nil, fmt.Errorf("unsupported set operation")
	}
	work := maxReplacementWork
	var result dynamicSet
	for i, value := range values {
		current, ok, err := readDynamicSet(value, &work)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		if i == 0 {
			result = current
			continue
		}
		switch operation {
		case "set_union":
			for _, key := range current.order {
				if err := result.add(key, current.values[key]); err != nil {
					return nil, err
				}
			}
		case "set_intersect":
			for key, raw := range result.values {
				if _, exists := current.values[key]; !exists {
					delete(result.values, key)
					result.size -= len(key) + len(raw)
				}
			}
		case "set_difference":
			for key := range current.values {
				if raw, exists := result.values[key]; exists {
					delete(result.values, key)
					result.size -= len(key) + len(raw)
				}
			}
		}
	}
	var encoded bytes.Buffer
	encoded.WriteByte('[')
	for _, key := range result.order {
		if raw, exists := result.values[key]; exists {
			if err := appendArrayElement(&encoded, raw); err != nil {
				return nil, err
			}
		}
	}
	encoded.WriteByte(']')
	return encoded.String(), nil
}

func readDynamicSet(value any, work *int) (dynamicSet, bool, error) {
	if text, ok := value.(string); ok {
		*work -= len(text)
		if *work < 0 {
			return dynamicSet{}, false, fmt.Errorf("set input work limit exceeded")
		}
	}
	elements, ok, err := dynamicArray(value)
	if err != nil || !ok {
		return dynamicSet{}, false, err
	}
	current := dynamicSet{values: make(map[string]json.RawMessage)}
	for _, element := range elements {
		key, err := setValueKey(element, work)
		if err != nil {
			return dynamicSet{}, false, err
		}
		if err := current.add(key, element); err != nil {
			return dynamicSet{}, false, err
		}
	}
	return current, true, nil
}

func kqlSetEquals(leftValue, rightValue any) (any, error) {
	work := maxReplacementWork
	left, ok, err := readDynamicSet(leftValue, &work)
	if err != nil || !ok {
		return nil, err
	}
	right, ok, err := readDynamicSet(rightValue, &work)
	if err != nil || !ok {
		return nil, err
	}
	if len(left.values) != len(right.values) {
		return false, nil
	}
	for key := range left.values {
		if _, exists := right.values[key]; !exists {
			return false, nil
		}
	}
	return true, nil
}

func kqlJaccardIndex(leftValue, rightValue any) (any, error) {
	work := maxReplacementWork
	left, ok, err := readDynamicSet(leftValue, &work)
	if err != nil || !ok {
		return nil, err
	}
	right, ok, err := readDynamicSet(rightValue, &work)
	if err != nil || !ok {
		return nil, err
	}
	intersection := 0
	for key := range left.values {
		if _, exists := right.values[key]; exists {
			intersection++
		}
	}
	// Cardinalities are bounded by the decoder and distinct-element limits.
	union := len(left.values) + len(right.values) - intersection
	if union == 0 {
		return nil, fmt.Errorf("jaccard_index of two empty sets is not yet supported")
	}
	return float64(intersection) / float64(union), nil
}
