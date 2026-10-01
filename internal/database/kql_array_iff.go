package database

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type arrayBranch struct {
	scalar   json.RawMessage
	elements []json.RawMessage
	array    bool
}

func prepareArrayBranch(value any, kind string) (arrayBranch, error) {
	raw, err := packJSONValue(value, kind)
	if err != nil {
		return arrayBranch{}, err
	}
	if kind == "dynamic" && len(raw) > 0 && raw[0] == '[' {
		elements, ok, err := dynamicArray(string(raw))
		if err != nil {
			return arrayBranch{}, err
		}
		if !ok {
			return arrayBranch{}, fmt.Errorf("array_iff branch must contain valid JSON")
		}
		return arrayBranch{elements: elements, array: true}, nil
	}
	return arrayBranch{scalar: raw}, nil
}

func (branch arrayBranch) at(index int) json.RawMessage {
	if !branch.array {
		return branch.scalar
	}
	if index >= len(branch.elements) {
		return json.RawMessage("null")
	}
	return branch.elements[index]
}

func arrayCondition(raw json.RawMessage) (bool, bool) {
	switch string(raw) {
	case "true":
		return true, true
	case "false":
		return false, true
	}
	if len(raw) == 0 || raw[0] != '-' && (raw[0] < '0' || raw[0] > '9') {
		return false, false
	}
	// JSON numbers are already validated by the array decoder. Check the
	// mantissa directly: exponent size cannot overflow or allocate a number.
	for _, digit := range raw {
		if digit == 'e' || digit == 'E' {
			break
		}
		if digit >= '1' && digit <= '9' {
			return true, true
		}
	}
	return false, true
}

func kqlArrayIff(value, whenTrue, whenFalse any, trueKind, falseKind string) (any, error) {
	conditions, ok, err := dynamicArray(value)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	trueBranch, err := prepareArrayBranch(whenTrue, trueKind)
	if err != nil {
		return nil, err
	}
	falseBranch, err := prepareArrayBranch(whenFalse, falseKind)
	if err != nil {
		return nil, err
	}
	var result bytes.Buffer
	result.WriteByte('[')
	for i, condition := range conditions {
		truth, valid := arrayCondition(condition)
		element := json.RawMessage("null")
		if valid {
			if truth {
				element = trueBranch.at(i)
			} else {
				element = falseBranch.at(i)
			}
		}
		if err := appendArrayElement(&result, element); err != nil {
			return nil, err
		}
	}
	result.WriteByte(']')
	return result.String(), nil
}
