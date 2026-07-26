package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattn/go-sqlite3"
)

const sqliteDriverName = "striem_sqlite3"

func init() {
	sql.Register(sqliteDriverName, &sqlite3.SQLiteDriver{ConnectHook: func(connection *sqlite3.SQLiteConn) error {
		if err := connection.RegisterFunc("kql_has", kqlHas, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_split", kqlSplit, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_extract", kqlExtract, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_trim", kqlTrim, true); err != nil {
			return err
		}
		if err := connection.RegisterAggregator("kql_make_list", func() *kqlCollection { return &kqlCollection{} }, true); err != nil {
			return err
		}
		if err := connection.RegisterAggregator("kql_make_set", func() *kqlCollection { return &kqlCollection{distinct: true, seen: make(map[string]struct{})} }, true); err != nil {
			return err
		}
		return nil
	}})
}

const (
	maxCollectionValues = 1000
	maxCollectionBytes  = 1 << 20
)

type kqlCollection struct {
	values   []any
	seen     map[string]struct{}
	distinct bool
	bytes    int
}

func (collection *kqlCollection) Step(value any, dynamic int64) error {
	if value == nil || len(collection.values) >= maxCollectionValues {
		return nil
	}
	if bytes, ok := value.([]byte); ok {
		if bytes == nil {
			return nil
		}
		value = string(bytes)
	}
	if dynamic != 0 {
		if text, ok := value.(string); ok {
			trimmed := strings.TrimSpace(text)
			if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
				var decoded any
				if json.Unmarshal([]byte(trimmed), &decoded) == nil {
					value = decoded
				}
			}
		}
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if collection.bytes+len(encoded) > maxCollectionBytes {
		return nil
	}
	if collection.distinct {
		key := string(encoded)
		if _, exists := collection.seen[key]; exists {
			return nil
		}
		collection.seen[key] = struct{}{}
	}
	collection.values = append(collection.values, value)
	collection.bytes += len(encoded)
	return nil
}

func (collection *kqlCollection) Done() (string, error) {
	encoded, err := json.Marshal(collection.values)
	return string(encoded), err
}

func kqlHas(value, term string, caseSensitive int64) bool {
	if caseSensitive == 0 {
		value, term = strings.ToLower(value), strings.ToLower(term)
	}
	if term == "" {
		return false
	}
	for offset := 0; offset <= len(value)-len(term); {
		index := strings.Index(value[offset:], term)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(term)
		if tokenBoundary(value, start, -1) && tokenBoundary(value, end, 1) {
			return true
		}
		offset = start + 1
	}
	return false
}

func tokenBoundary(value string, offset, direction int) bool {
	if offset <= 0 && direction < 0 || offset >= len(value) && direction > 0 {
		return true
	}
	var character rune
	if direction < 0 {
		character, _ = utf8.DecodeLastRuneInString(value[:offset])
	} else {
		character, _ = utf8.DecodeRuneInString(value[offset:])
	}
	return !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_'
}

func kqlSplit(value, delimiter string) (string, error) {
	if delimiter == "" {
		return "", fmt.Errorf("split delimiter cannot be empty")
	}
	encoded, err := json.Marshal(strings.Split(value, delimiter))
	return string(encoded), err
}

func kqlExtract(pattern string, capture int64, value string) (string, error) {
	if len(pattern) > 512 {
		return "", fmt.Errorf("extract pattern exceeds 512 characters")
	}
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	matches := expression.FindStringSubmatch(value)
	if capture < 0 || int(capture) >= len(matches) {
		return "", nil
	}
	return matches[capture], nil
}

func kqlTrim(pattern, value string) (string, error) {
	if len(pattern) > 512 {
		return "", fmt.Errorf("trim pattern exceeds 512 characters")
	}
	expression, err := regexp.Compile("^(?:" + pattern + ")+|(?:" + pattern + ")+$")
	if err != nil {
		return "", err
	}
	return expression.ReplaceAllString(value, ""), nil
}
