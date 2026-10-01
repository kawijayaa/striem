package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

func bagObject(value any) (map[string]json.RawMessage, bool, error) {
	text, ok := value.(string)
	if !ok {
		return nil, false, nil
	}
	if len(text) > maxBagBytes {
		return nil, false, fmt.Errorf("bag input exceeds %d bytes", maxBagBytes)
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &properties); err != nil || properties == nil {
		return nil, false, nil
	}
	if len(properties) > maxReplacementParts {
		return nil, false, fmt.Errorf("bag property limit exceeded")
	}
	return properties, true, nil
}

func kqlBagMerge(values ...any) (any, error) {
	if len(values) < 2 || len(values) > 64 {
		return nil, fmt.Errorf("bag_merge requires 2 to 64 bags")
	}
	merged := make(map[string]json.RawMessage)
	size := 2
	for _, value := range values {
		properties, ok, err := bagObject(value)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		for key, raw := range properties {
			if _, exists := merged[key]; exists {
				continue
			}
			if len(merged) == maxReplacementParts {
				return nil, fmt.Errorf("bag_merge property limit exceeded")
			}
			var compact bytes.Buffer
			if err := json.Compact(&compact, raw); err != nil {
				return nil, err
			}
			encodedKey, _ := json.Marshal(key)
			extra := len(encodedKey) + 1 + compact.Len()
			if len(merged) > 0 {
				extra++
			}
			if extra > maxBagBytes-size {
				return nil, fmt.Errorf("bag_merge result exceeds %d bytes", maxBagBytes)
			}
			size += extra
			merged[key] = append(json.RawMessage(nil), compact.Bytes()...)
		}
	}
	return encodeBag(merged)
}

func encodeBag(bag map[string]json.RawMessage) (any, error) {
	encoded, err := json.Marshal(bag)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxBagBytes {
		return nil, fmt.Errorf("bag result exceeds %d bytes", maxBagBytes)
	}
	return string(encoded), nil
}

type bagRemoval struct {
	remove   bool
	children map[string]*bagRemoval
}

func kqlBagRemoveKeys(value, keysValue any) (any, error) {
	bag, ok, err := bagObject(value)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	keysText, ok := keysValue.(string)
	if !ok {
		return nil, fmt.Errorf("bag_remove_keys keys must be a dynamic string array")
	}
	if len(keysText) > maxBagBytes {
		return nil, fmt.Errorf("bag_remove_keys keys exceed %d bytes", maxBagBytes)
	}
	var keys []json.RawMessage
	if err := json.Unmarshal([]byte(keysText), &keys); err != nil || !strings.HasPrefix(strings.TrimSpace(keysText), "[") {
		return nil, fmt.Errorf("bag_remove_keys keys must be a dynamic string array")
	}
	if len(keys) > maxReplacementParts {
		return nil, fmt.Errorf("bag_remove_keys key limit exceeded")
	}
	root := &bagRemoval{}
	nodes := 0
	for _, raw := range keys {
		if len(raw) == 0 || raw[0] != '"' {
			return nil, fmt.Errorf("bag_remove_keys keys must be strings")
		}
		var key string
		if err := json.Unmarshal(raw, &key); err != nil {
			return nil, err
		}
		if len(key) > maxBagPathBytes {
			return nil, fmt.Errorf("bag_remove_keys path limit exceeded")
		}
		path := []string{key}
		if strings.HasPrefix(key, "$") {
			var valid bool
			path, valid = parseBagPath(key)
			if !valid || len(path) == 0 {
				return nil, fmt.Errorf("bag_remove_keys unsupported JSON path")
			}
		}
		current := root
		for _, property := range path {
			if current.remove {
				break
			}
			if current.children == nil {
				current.children = make(map[string]*bagRemoval)
			}
			child := current.children[property]
			if child == nil {
				if nodes == maxReplacementParts {
					return nil, fmt.Errorf("bag_remove_keys path node limit exceeded")
				}
				child = &bagRemoval{}
				current.children[property] = child
				nodes++
			}
			current = child
		}
		current.remove = true
	}
	work := maxReplacementWork
	if _, err := removeBagProperties(bag, root, &work); err != nil {
		return nil, err
	}
	return encodeBag(bag)
}

func removeBagProperties(bag map[string]json.RawMessage, node *bagRemoval, work *int) (bool, error) {
	changed := false
	for key, child := range node.children {
		*work--
		if *work < 0 {
			return false, fmt.Errorf("bag_remove_keys work limit exceeded")
		}
		raw, exists := bag[key]
		if !exists {
			continue
		}
		if child.remove {
			delete(bag, key)
			changed = true
			continue
		}
		*work -= len(raw)
		if *work < 0 {
			return false, fmt.Errorf("bag_remove_keys work limit exceeded")
		}
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(raw, &nested); err != nil || nested == nil {
			continue
		}
		altered, err := removeBagProperties(nested, child, work)
		if err != nil {
			return false, err
		}
		if !altered {
			continue
		}
		encoded, err := json.Marshal(nested)
		if err != nil {
			return false, err
		}
		*work -= len(encoded)
		if *work < 0 {
			return false, fmt.Errorf("bag_remove_keys work limit exceeded")
		}
		if len(encoded) > maxBagBytes {
			return false, fmt.Errorf("bag_remove_keys result exceeds %d bytes", maxBagBytes)
		}
		bag[key] = encoded
		changed = true
	}
	return changed, nil
}
