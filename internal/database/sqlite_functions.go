package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kawijayaa/striem/internal/eventtime"
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
		if err := connection.RegisterFunc("kql_parse", kqlParse, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_array_length", kqlArrayLength, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_bag_keys", kqlBagKeys, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_todatetime", kqlToDatetime, true); err != nil {
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
	maxParseCaptures    = 16
	maxParsePattern     = 2 << 10
	maxParseSource      = 64 << 10
	maxParseTypes       = maxParseCaptures*len("string") + maxParseCaptures - 1
)

var isoDatetimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})?)?$`)

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
			if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, `"`) {
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

func kqlParse(patternValue, typesValue, sourceValue any) any {
	pattern, patternOK := patternValue.(string)
	types, typesOK := typesValue.(string)
	source, sourceOK := sourceValue.(string)
	if !patternOK || !typesOK || !sourceOK || len(pattern) > maxParsePattern || len(types) > maxParseTypes || len(source) > maxParseSource {
		return nil
	}

	expression, err := regexp.Compile(pattern)
	if err != nil || expression.NumSubexp() > maxParseCaptures {
		return nil
	}
	descriptors := make([]string, 0)
	if types != "" {
		descriptors = strings.Split(types, ",")
	}
	if len(descriptors) != expression.NumSubexp() {
		return nil
	}
	for _, descriptor := range descriptors {
		if descriptor != "string" && descriptor != "long" && descriptor != "real" {
			return nil
		}
	}

	matches := expression.FindStringSubmatch(source)
	if matches == nil {
		return nil
	}
	values := make([]any, len(descriptors))
	for index, descriptor := range descriptors {
		capture := matches[index+1]
		switch descriptor {
		case "string":
			values[index] = capture
		case "long":
			value, err := strconv.ParseInt(capture, 10, 64)
			if err != nil {
				return nil
			}
			values[index] = value
		case "real":
			value, err := strconv.ParseFloat(capture, 64)
			if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
				return nil
			}
			values[index] = value
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil
	}
	return string(encoded)
}

func kqlArrayLength(value any) any {
	text, ok := value.(string)
	if !ok || !strings.HasPrefix(strings.TrimSpace(text), "[") {
		return nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal([]byte(text), &values); err != nil {
		return nil
	}
	return int64(len(values))
}

func kqlBagKeys(value any) any {
	text, ok := value.(string)
	if !ok || !strings.HasPrefix(strings.TrimSpace(text), "{") {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal([]byte(text), &bag); err != nil {
		return nil
	}
	keys := make([]string, 0, len(bag))
	for key := range bag {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	encoded, err := json.Marshal(keys)
	if err != nil {
		return nil
	}
	return string(encoded)
}

func kqlToDatetime(value any) any {
	text, ok := value.(string)
	if !ok || !isoDatetimePattern.MatchString(text) {
		return nil
	}

	var parsed time.Time
	var err error
	switch {
	case len(text) == len("2006-01-02"):
		parsed, err = time.ParseInLocation("2006-01-02", text, time.UTC)
	case strings.HasSuffix(text, "Z") || text[len(text)-6] == '+' || text[len(text)-6] == '-':
		parsed, err = time.Parse(time.RFC3339Nano, strings.Replace(text, " ", "T", 1))
	default:
		parsed, err = time.ParseInLocation("2006-01-02T15:04:05", strings.Replace(text, " ", "T", 1), time.UTC)
	}
	if err != nil {
		return nil
	}
	return eventtime.Format(parsed)
}
