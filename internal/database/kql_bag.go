package database

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const maxBagBytes = 4 << 20
const maxBagProperties = 1048576

// bagAggregate selects the first value seen for each key. KQL deliberately
// leaves the choice unspecified when different rows contain the same key.
type bagAggregate struct {
	values map[string]json.RawMessage
	bytes  int
}

func (bag *bagAggregate) Step(value any, maximum int64) error {
	if maximum < 1 || maximum > maxBagProperties {
		return fmt.Errorf("make_bag maxSize must be an integer from 1 to %d", maxBagProperties)
	}
	if int64(len(bag.values)) >= maximum {
		return nil
	}
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return nil
	}
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "{") {
		return nil
	}
	if len(text) > maxBagBytes {
		return fmt.Errorf("make_bag input exceeds %d bytes", maxBagBytes)
	}
	var properties map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &properties); err != nil {
		return nil
	}
	// Stable selection when maxSize is reached within one object. Bag ordering
	// is not part of KQL semantics.
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if int64(len(bag.values)) >= maximum {
			break
		}
		if _, exists := bag.values[key]; exists {
			continue
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, properties[key]); err != nil {
			return err
		}
		encodedKey, _ := json.Marshal(key)
		extra := len(encodedKey) + 1 + compact.Len()
		if len(bag.values) > 0 {
			extra++
		}
		if bag.bytes+extra+2 > maxBagBytes {
			return fmt.Errorf("make_bag result exceeds %d bytes", maxBagBytes)
		}
		if bag.values == nil {
			bag.values = make(map[string]json.RawMessage)
		}
		bag.values[key] = append(json.RawMessage(nil), compact.Bytes()...)
		bag.bytes += extra
	}
	return nil
}

func (bag *bagAggregate) Done() (string, error) {
	if len(bag.values) == 0 {
		return "{}", nil
	}
	encoded, err := json.Marshal(bag.values)
	if len(encoded) > maxBagBytes {
		return "", fmt.Errorf("make_bag result exceeds %d bytes", maxBagBytes)
	}
	return string(encoded), err
}
