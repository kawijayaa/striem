package database

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/kawijayaa/ksql"
)

// maxCollectSize is Kusto's default and maximum make_list/make_set size;
// maxCollectBytes bounds one group's result.
const (
	maxCollectSize  = 1 << 20
	maxCollectBytes = 8 << 20
)

// collectAggregate implements make_list, make_set and their _if and
// with_nulls forms. Elements of dynamic arrays are added individually, as
// Kusto does; nulls are skipped except by make_list_with_nulls.
type collectAggregate struct {
	distinct, nulls bool
	limit           int64
	values          []any
	seen            map[string]struct{}
	bytes           int
}

func collectElement(value any, kind string) (any, bool) {
	if value == nil || sqliteNull(value) {
		return nil, false
	}
	if bytes, ok := value.([]byte); ok {
		value = string(bytes)
	}
	switch kind {
	case "bool":
		if number, ok := value.(int64); ok {
			return number != 0, true
		}
	case "timespan":
		if ticks, ok := value.(int64); ok {
			return ksql.FormatTimespan(ticks), true
		}
	case "dynamic":
		if text, ok := value.(string); ok {
			var decoded any
			if json.Unmarshal([]byte(text), &decoded) == nil {
				return decoded, true
			}
		}
	}
	return value, true
}

func (a *collectAggregate) add(value any) error {
	if a.limit > 0 && int64(len(a.values)) >= a.limit {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if a.distinct {
		key := string(encoded)
		if _, exists := a.seen[key]; exists {
			return nil
		}
		a.seen[key] = struct{}{}
	}
	if a.bytes+len(encoded) > maxCollectBytes {
		return fmt.Errorf("collected values exceed %d bytes", maxCollectBytes)
	}
	a.bytes += len(encoded) + 1
	a.values = append(a.values, value)
	return nil
}

func (a *collectAggregate) Step(value any, kind string, limitValue any) error {
	if a.limit == 0 {
		a.limit = maxCollectSize
		if limit, ok := limitValue.(int64); ok && limit > 0 && limit < maxCollectSize {
			a.limit = limit
		}
		if a.distinct && a.seen == nil {
			a.seen = map[string]struct{}{}
		}
	}
	element, ok := collectElement(value, kind)
	if !ok {
		if a.nulls {
			return a.add(nil)
		}
		return nil
	}
	if array, isArray := element.([]any); isArray && kind == "dynamic" {
		for _, item := range array {
			if item == nil && !a.nulls {
				continue
			}
			if err := a.add(item); err != nil {
				return err
			}
		}
		return nil
	}
	return a.add(element)
}

func (a *collectAggregate) Done() (string, error) {
	if a.values == nil {
		return "[]", nil
	}
	var builder strings.Builder
	encoder := json.NewEncoder(&builder)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(a.values); err != nil {
		return "", err
	}
	return strings.TrimSuffix(builder.String(), "\n"), nil
}
