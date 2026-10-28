package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	lru "github.com/hashicorp/golang-lru"
	"github.com/kawijayaa/ksql/runtime"
	"github.com/kawijayaa/striem/internal/eventtime"
	"github.com/mattn/go-sqlite3"
)

const sqliteDriverName = "striem_sqlite3"

func init() {
	sql.Register(sqliteDriverName, &sqlite3.SQLiteDriver{ConnectHook: func(connection *sqlite3.SQLiteConn) error {
		for _, pragma := range []string{
			"PRAGMA foreign_keys = ON",
			"PRAGMA busy_timeout = 5000",
			"PRAGMA synchronous = NORMAL",
			"PRAGMA cache_size = -131072",
			"PRAGMA temp_store = MEMORY",
			"PRAGMA mmap_size = 268435456",
		} {
			if _, err := connection.Exec(pragma, nil); err != nil {
				return fmt.Errorf("configure SQLite connection: %w", err)
			}
		}
		if err := registerKQLCompatibility(connection); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_has", kqlHas, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_regex", kqlRegex, true); err != nil {
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
		if err := connection.RegisterFunc("kql_parse_kv", kqlParseKV, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_base64_decode_tostring", kqlBase64DecodeToString, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_url_decode", kqlURLDecode, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_bag_has_key", kqlBagHasKey, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_set_has_element", kqlSetHasElement, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_ipv4_is_private", kqlIPv4IsPrivate, true); err != nil {
			return err
		}
		if err := connection.RegisterFunc("kql_ipv4_is_in_range", kqlIPv4IsInRange, true); err != nil {
			return err
		}
		for name, function := range map[string]any{
			"kql_regex_captures":                   kqlRegexCaptures,
			"kql_key_values":                       kqlKeyValues,
			"kql_toguid":                           kqlToGUID,
			"kql_math":                             kqlMath,
			"kql_round":                            kqlRound,
			"kql_array_sort_asc":                   kqlArraySort(false),
			"kql_array_sort_desc":                  kqlArraySort(true),
			"kql_bin_at":                           kqlBinAt,
			"kql_bin_at_datetime":                  kqlBinAtDatetime,
			"kql_datetime_add_ticks":               kqlDatetimeAddTicks,
			"kql_datetime_diff_ticks":              kqlDatetimeDiffTicks,
			"kql_totimespan":                       kqlToTimespan,
			"kql_dayofweek":                        kqlDayOfWeek,
			"kql_format_datetime":                  kqlFormatDatetime,
			"kql_format_timespan":                  kqlFormatTimespan,
			"kql_make_timespan":                    kqlMakeTimespan,
			"kql_number_class":                     kqlNumberClass,
			"kql_max_of":                           kqlExtremeOf(true),
			"kql_min_of":                           kqlExtremeOf(false),
			"kql_strcmp":                           kqlStrcmp,
			"kql_isascii":                          kqlIsASCII,
			"kql_isutf8":                           kqlIsUTF8,
			"kql_parse_ipv4":                       kqlParseIPv4,
			"kql_parse_ipv4_mask":                  kqlParseIPv4Mask,
			"kql_format_ipv4":                      kqlFormatIPv4,
			"kql_format_ipv4_mask":                 kqlFormatIPv4Mask,
			"kql_ipv4_netmask_suffix":              kqlIPv4NetmaskSuffix,
			"kql_ipv4_compare":                     kqlIPv4Compare,
			"kql_ipv4_is_match":                    kqlIPv4IsMatch,
			"kql_ipv4_range_to_cidr_list":          kqlIPv4RangeToCIDRList,
			"kql_parse_ipv6":                       kqlParseIPv6,
			"kql_parse_ipv6_mask":                  kqlParseIPv6Mask,
			"kql_ipv6_compare":                     kqlIPv6Compare,
			"kql_ipv6_is_match":                    kqlIPv6IsMatch,
			"kql_make_datetime":                    kqlMakeDatetime,
			"kql_unixtime_seconds_todatetime":      kqlUnixSeconds,
			"kql_unixtime_milliseconds_todatetime": kqlUnixMilliseconds,
			"kql_unixtime_microseconds_todatetime": kqlUnixMicroseconds,
			"kql_unixtime_nanoseconds_todatetime":  kqlUnixNanoseconds,
			"kql_datetime_part":                    kqlDatetimePart,
			"kql_datetime_diff":                    kqlDatetimeDiff,
			"kql_datetime_add":                     kqlDatetimeAdd,
			"kql_countof":                          kqlCountOf,
			"kql_strcat_delim":                     kqlStrcatDelim,
			"kql_join_scalar_text":                 kqlJoinScalarText,
			"kql_strcat_array":                     kqlStrcatArray,
			"kql_strrep":                           kqlStrrep,
			"kql_translate":                        kqlTranslate,
			"kql_reverse":                          kqlReverse,
			"kql_tohex":                            kqlToHex,
			"kql_binary_and":                       kqlBinaryAnd,
			"kql_binary_or":                        kqlBinaryOr,
			"kql_binary_xor":                       kqlBinaryXor,
			"kql_binary_not":                       kqlBinaryNot,
			"kql_binary_shift_left":                kqlBinaryShiftLeft,
			"kql_binary_shift_right":               kqlBinaryShiftRight,
			"kql_bitset_count_ones":                kqlBitsetCountOnes,
			"kql_scalar_range":                     kqlScalarRange,
			"kql_codepoints_from_string":           kqlCodepointsFromString,
			"kql_codepoints_to_string":             kqlCodepointsToString,
			"kql_hash_md5":                         kqlHashMD5,
			"kql_hash_sha1":                        kqlHashSHA1,
			"kql_hash_sha256":                      kqlHashSHA256,
			"kql_url_encode":                       kqlURLEncode,
			"kql_url_encode_component":             kqlURLEncodeComponent,
			"kql_gzip_compress":                    kqlGzipCompress,
			"kql_gzip_decompress":                  kqlGzipDecompress,
			"kql_zlib_compress":                    kqlZlibCompress,
			"kql_zlib_decompress":                  kqlZlibDecompress,
			"kql_base64_encode_tostring":           kqlBase64EncodeToString,
			"kql_base64_decode_toarray":            kqlBase64DecodeToArray,
			"kql_base64_encode_fromarray":          kqlBase64EncodeFromArray,
			"kql_ipv4_is_in_any_range":             kqlIPv4IsInAnyRange,
			"kql_ipv6_is_in_any_range":             kqlIPv6IsInAnyRange,
			"kql_ipv6_is_in_range":                 kqlIPv6IsInRange,
		} {
			if err := connection.RegisterFunc(name, function, true); err != nil {
				return err
			}
		}
		// rand is volatile: SQLite must evaluate it for every row.
		if err := connection.RegisterFunc("kql_rand", kqlRand, false); err != nil {
			return err
		}
		for name, function := range sqlMathFunctions() {
			if err := connection.RegisterFunc(name, function, true); err != nil {
				return err
			}
		}
		for _, operation := range []string{"and", "or", "xor"} {
			operation := operation
			if err := connection.RegisterAggregator("kql_binary_all_"+operation, func() *bitwiseAggregate { return &bitwiseAggregate{operation: operation} }, true); err != nil {
				return err
			}
		}

		if err := connection.RegisterAggregator("kql_make_list", func() *kqlCollection { return &kqlCollection{} }, true); err != nil {
			return err
		}
		if err := connection.RegisterAggregator("kql_make_set", func() *kqlCollection { return &kqlCollection{distinct: true, seen: make(map[string]struct{})} }, true); err != nil {
			return err
		}
		if err := connection.RegisterAggregator("kql_make_set_value", func() *kqlValueCollection {
			return &kqlValueCollection{collection: kqlCollection{distinct: true, seen: make(map[string]struct{})}}
		}, true); err != nil {
			return err
		}
		return nil
	}})
}

const (
	maxCollectionValues  = 1000
	maxCollectionBytes   = 1 << 20
	maxParseCaptures     = 16
	maxParsePattern      = 2 << 10
	maxParseSource       = 64 << 10
	maxParseTypes        = maxParseCaptures*len("string") + maxParseCaptures - 1
	maxParseKVSchema     = 2 << 10
	maxParseKVKeys       = 16
	maxParseKVDelimiter  = 16
	maxDecodeInputBytes  = 64 << 10
	maxDynamicInputBytes = 1 << 20
	maxBagPathBytes      = 1 << 10
	maxBagPathDepth      = 32
	maxSetValues         = 1000
	maxIPv4AddressBytes  = len("255.255.255.255/32")
	maxIPv4RangeBytes    = 4 << 10
	maxIPv4Ranges        = 128
	regexpCacheEntries   = 256
)

// The SQLite driver passes SQL NULL to generic UDF arguments as []byte(nil).
func sqliteNull(value any) bool {
	if value == nil {
		return true
	}
	bytes, ok := value.([]byte)
	return ok && bytes == nil
}

var isoDatetimePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}(?:[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d{1,9})?)?(?:Z|[+-](?:[01]\d|2[0-3]):[0-5]\d)?)?$`)

var regexpCache = func() *lru.Cache {
	cache, err := lru.New(regexpCacheEntries)
	if err != nil {
		panic(err)
	}
	return cache
}()

var privateIPv4Prefixes = [...]netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
}

type kqlCollection struct {
	values   []any
	seen     map[string]struct{}
	distinct bool
	bytes    int
}

type kqlValueCollection struct {
	collection kqlCollection
}

func (collection *kqlValueCollection) Step(value any) error {
	dynamic := int64(0)
	if text, ok := value.(string); ok {
		trimmed := strings.TrimSpace(text)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.HasPrefix(trimmed, `"`) {
			dynamic = 1
		}
	}
	return collection.collection.Step(value, dynamic)
}

func (collection *kqlValueCollection) Done() (string, error) {
	return collection.collection.Done()
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
	if collection.values == nil {
		// An empty group still produces an empty dynamic array.
		return "[]", nil
	}
	encoded, err := json.Marshal(collection.values)
	return string(encoded), err
}

func kqlHas(valueValue, termValue any, caseSensitive int64) bool {
	value, _ := textArgument(valueValue)
	term, _ := textArgument(termValue)
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

func kqlRegex(patternValue, valueValue any) (bool, error) {
	pattern, patternOK := patternValue.(string)
	value, valueOK := valueValue.(string)
	if bytes, ok := patternValue.([]byte); ok {
		pattern, patternOK = string(bytes), true
	}
	if bytes, ok := valueValue.([]byte); ok {
		value, valueOK = string(bytes), true
	}
	if !patternOK || !valueOK {
		return false, nil
	}
	if len(pattern) > 512 {
		return false, fmt.Errorf("regular expression exceeds 512 characters")
	}
	if matched, recognized := matchKSQLTermRegex(pattern, value); recognized {
		return matched, nil
	}
	expression, err := compileRegexp(pattern)
	if err != nil {
		return false, err
	}
	return expression.MatchString(value), nil
}

func compileRegexp(pattern string) (*regexp.Regexp, error) {
	if cached, ok := regexpCache.Get(pattern); ok {
		return cached.(*regexp.Regexp), nil
	}
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexpCache.Add(pattern, expression)
	return expression, nil
}

func matchKSQLTermRegex(pattern, value string) (bool, bool) {
	caseInsensitive := strings.HasPrefix(pattern, "(?i)")
	if caseInsensitive {
		pattern = strings.TrimPrefix(pattern, "(?i)")
	}

	leftBoundaries := [...]string{`(^|[^A-Za-z0-9])`, `(^|[^a-za-z0-9])`}
	rightBoundaries := [...]string{`([^A-Za-z0-9]|$)`, `([^a-za-z0-9]|$)`}
	for index := range leftBoundaries {
		left := leftBoundaries[index]
		right := rightBoundaries[index]
		if !strings.HasPrefix(pattern, left) {
			continue
		}
		body := strings.TrimPrefix(pattern, left)
		mode := byte('p')
		switch {
		case strings.HasPrefix(body, `[[:alnum:]]*`) && strings.HasSuffix(body, right):
			mode = 's'
			body = strings.TrimSuffix(strings.TrimPrefix(body, `[[:alnum:]]*`), right)
		case strings.HasSuffix(body, right):
			mode = 'w'
			body = strings.TrimSuffix(body, right)
		case strings.HasSuffix(body, `[[:alnum:]]*`):
			body = strings.TrimSuffix(body, `[[:alnum:]]*`)
		default:
			continue
		}
		term, ok := decodeRegexpLiteral(body)
		if !ok || term == "" {
			return false, false
		}
		return matchTerm(value, term, mode, caseInsensitive), true
	}
	return false, false
}

func decodeRegexpLiteral(pattern string) (string, bool) {
	const metacharacters = `\.+*?()|[]{}^$`
	var literal strings.Builder
	literal.Grow(len(pattern))
	for offset := 0; offset < len(pattern); offset++ {
		character := pattern[offset]
		if character == '\\' {
			offset++
			if offset >= len(pattern) || !strings.ContainsRune(metacharacters, rune(pattern[offset])) {
				return "", false
			}
			literal.WriteByte(pattern[offset])
			continue
		}
		if strings.ContainsRune(metacharacters, rune(character)) {
			return "", false
		}
		literal.WriteByte(character)
	}
	return literal.String(), true
}

func matchTerm(value, term string, mode byte, caseInsensitive bool) bool {
	if caseInsensitive {
		valueRunes, termRunes := []rune(value), []rune(term)
		for start := 0; start+len(termRunes) <= len(valueRunes); start++ {
			end := start + len(termRunes)
			if equalFoldRunes(valueRunes[start:end], termRunes) &&
				(mode == 's' || asciiTokenBoundaryRunes(valueRunes, start, -1)) &&
				(mode == 'p' || asciiTokenBoundaryRunes(valueRunes, end, 1)) {
				return true
			}
		}
		return false
	}
	for offset := 0; offset <= len(value)-len(term); {
		index := strings.Index(value[offset:], term)
		if index < 0 {
			return false
		}
		start := offset + index
		end := start + len(term)
		if (mode == 's' || asciiTokenBoundary(value, start, -1)) &&
			(mode == 'p' || asciiTokenBoundary(value, end, 1)) {
			return true
		}
		offset = start + 1
	}
	return false
}

func equalFoldRunes(left, right []rune) bool {
	if len(left) != len(right) {
		return false
	}
	for index, character := range left {
		if character == right[index] {
			continue
		}
		folded := unicode.SimpleFold(character)
		for folded != character && folded != right[index] {
			folded = unicode.SimpleFold(folded)
		}
		if folded != right[index] {
			return false
		}
	}
	return true
}

func asciiTokenBoundary(value string, offset, direction int) bool {
	if direction < 0 {
		return offset <= 0 || !isASCIIAlphaNumeric(value[offset-1])
	}
	return offset >= len(value) || !isASCIIAlphaNumeric(value[offset])
}

func asciiTokenBoundaryRunes(value []rune, offset, direction int) bool {
	if direction < 0 {
		return offset <= 0 || value[offset-1] > unicode.MaxASCII || !isASCIIAlphaNumeric(byte(value[offset-1]))
	}
	return offset >= len(value) || value[offset] > unicode.MaxASCII || !isASCIIAlphaNumeric(byte(value[offset]))
}

func isASCIIAlphaNumeric(character byte) bool {
	return character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z' || character >= '0' && character <= '9'
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

// kqlExtract treats a null source as empty text, matching tostring(null).
func kqlExtract(patternValue, captureValue, valueValue any) (any, error) {
	pattern, patternOK := textArgument(patternValue)
	value, valueOK := textArgument(valueValue)
	capture, captureOK := captureValue.(int64)
	if !patternOK || !valueOK || !captureOK {
		return nil, nil
	}
	if len(pattern) > 512 {
		return "", fmt.Errorf("extract pattern exceeds 512 characters")
	}
	expression, err := compileRegexp(pattern)
	if err != nil {
		return "", err
	}
	matches := expression.FindStringSubmatch(value)
	if capture < 0 || int(capture) >= len(matches) {
		return "", nil
	}
	return matches[capture], nil
}

func kqlParse(patternValue, typesValue, sourceValue any) any {
	pattern, patternOK := patternValue.(string)
	types, typesOK := typesValue.(string)
	source, sourceOK := sourceValue.(string)
	if !patternOK || !typesOK || !sourceOK || len(pattern) > maxParsePattern || len(types) > maxParseTypes || len(source) > maxParseSource {
		return nil
	}

	expression, err := compileRegexp(pattern)
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
	if !ok {
		return nil
	}
	if len(text) > 35 || strings.HasPrefix(text, "0000-") || !isoDatetimePattern.MatchString(text) {
		if parsed, ok := parseKQLDatetimeFormats(text); ok {
			return eventtime.Format(parsed)
		}
		return nil
	}

	var parsed time.Time
	var err error
	if len(text) == 10 {
		parsed, err = time.ParseInLocation("2006-01-02", text, time.UTC)
	} else {
		normalized := strings.Replace(text, " ", "T", 1)
		if len(normalized) == 16 || normalized[16] != ':' {
			normalized = normalized[:16] + ":00" + normalized[16:]
		}
		if strings.HasSuffix(normalized, "Z") || normalized[len(normalized)-6] == '+' || normalized[len(normalized)-6] == '-' {
			parsed, err = time.Parse(time.RFC3339Nano, normalized)
		} else {
			parsed, err = time.ParseInLocation("2006-01-02T15:04:05", normalized, time.UTC)
		}
	}
	parsed = parsed.UTC()
	if err != nil || parsed.Year() < 1 || parsed.Year() > 9999 {
		return nil
	}

	return eventtime.Format(parsed)
}

func kqlParseKV(schemaValue, pairDelimiterValue, kvDelimiterValue, sourceValue any) any {
	schemaJSON, schemaOK := schemaValue.(string)
	pairDelimiter, pairOK := pairDelimiterValue.(string)
	kvDelimiter, kvOK := kvDelimiterValue.(string)
	source, sourceOK := sourceValue.(string)
	if !schemaOK || !pairOK || !kvOK || !sourceOK ||
		len(schemaJSON) > maxParseKVSchema || len(source) > maxParseSource ||
		len(pairDelimiter) < 1 || len(pairDelimiter) > maxParseKVDelimiter ||
		len(kvDelimiter) < 1 || len(kvDelimiter) > maxParseKVDelimiter ||
		!utf8.ValidString(schemaJSON) || !utf8.ValidString(pairDelimiter) ||
		!utf8.ValidString(kvDelimiter) || !utf8.ValidString(source) {
		return nil
	}

	var encodedSchema []map[string]json.RawMessage
	if err := json.Unmarshal([]byte(schemaJSON), &encodedSchema); err != nil || len(encodedSchema) < 1 || len(encodedSchema) > maxParseKVKeys {
		return nil
	}
	type schemaItem struct {
		name     string
		typeName string
	}
	schema := make([]schemaItem, len(encodedSchema))
	indexes := make(map[string]int, len(encodedSchema))
	seenNames := make(map[string]struct{}, len(encodedSchema))
	values := make([]any, len(encodedSchema))
	for index, encoded := range encodedSchema {
		if len(encoded) != 2 {
			return nil
		}
		var name, typeName string
		if err := json.Unmarshal(encoded["name"], &name); err != nil || name == "" {
			return nil
		}
		if err := json.Unmarshal(encoded["type"], &typeName); err != nil || (typeName != "string" && typeName != "long" && typeName != "real") {
			return nil
		}
		foldedName := strings.ToLower(name)
		if _, exists := seenNames[foldedName]; exists {
			return nil
		}
		seenNames[foldedName] = struct{}{}
		schema[index] = schemaItem{name: name, typeName: typeName}
		indexes[name] = index
		if typeName == "string" {
			values[index] = ""
		}
	}

	seenValues := make([]bool, len(schema))
	for _, pair := range strings.Split(source, pairDelimiter) {
		key, text, found := strings.Cut(pair, kvDelimiter)
		if !found {
			continue
		}
		index, declared := indexes[strings.TrimSpace(key)]
		if !declared || seenValues[index] {
			continue
		}
		seenValues[index] = true
		text = strings.TrimSpace(text)
		switch schema[index].typeName {
		case "string":
			values[index] = text
		case "long":
			if value, err := strconv.ParseInt(text, 10, 64); err == nil {
				values[index] = value
			}
		case "real":
			if value, err := strconv.ParseFloat(text, 64); err == nil && !math.IsInf(value, 0) && !math.IsNaN(value) {
				values[index] = value
			}
		}
	}
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil
	}
	return string(encoded)
}

func kqlBase64DecodeToString(value any) any {
	decoded, ok := decodeKQLBase64(value)
	if !ok || !utf8.Valid(decoded) {
		return nil
	}
	return string(decoded)
}

func kqlURLDecode(value any) any {
	text, ok := value.(string)
	if !ok || len(text) > 3*maxDecodeInputBytes || !utf8.ValidString(text) {
		return nil
	}
	decoded, err := url.PathUnescape(text)
	if err != nil || len(decoded) > maxDecodeInputBytes || !utf8.ValidString(decoded) {
		return nil
	}
	return decoded
}

func kqlBagHasKey(bagValue, keyValue any) any {
	bagText, bagOK := bagValue.(string)
	key, keyOK := keyValue.(string)
	if !bagOK || !keyOK || len(bagText) > maxDynamicInputBytes || len(key) > maxBagPathBytes ||
		!utf8.ValidString(bagText) || !utf8.ValidString(key) {
		return nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal([]byte(bagText), &bag); err != nil || bag == nil {
		return nil
	}
	if !strings.HasPrefix(key, "$") {
		_, exists := bag[key]
		return exists
	}
	path, ok := parseBagPath(key)
	if !ok {
		return nil
	}
	if len(path) == 0 {
		return true
	}
	current := bag
	for index, property := range path {
		encoded, exists := current[property]
		if !exists {
			return false
		}
		if index == len(path)-1 {
			return true
		}
		// Unmarshal merges into an existing map, which would retain ancestor
		// properties and incorrectly find them in the nested object.
		current = nil
		if err := json.Unmarshal(encoded, &current); err != nil || current == nil {
			return false
		}
	}
	return false
}

func parseBagPath(value string) ([]string, bool) {
	if value == "$" {
		return []string{}, true
	}
	if !strings.HasPrefix(value, "$") {
		return nil, false
	}
	properties := make([]string, 0, 4)
	for offset := 1; offset < len(value); {
		if len(properties) >= maxBagPathDepth {
			return nil, false
		}
		switch value[offset] {
		case '.':
			offset++
			start := offset
			if offset >= len(value) || !isBagIdentifierStart(value[offset]) {
				return nil, false
			}
			offset++
			for offset < len(value) && isBagIdentifierContinue(value[offset]) {
				offset++
			}
			properties = append(properties, value[start:offset])
		case '[':
			offset++
			if offset >= len(value) || value[offset] != '\'' && value[offset] != '"' {
				return nil, false
			}
			quote := value[offset]
			offset++
			start := offset
			escaped := false
			for offset < len(value) {
				if !escaped && value[offset] == quote {
					break
				}
				if !escaped && value[offset] == '\\' {
					escaped = true
				} else {
					escaped = false
				}
				offset++
			}
			if offset >= len(value) {
				return nil, false
			}
			encoded := value[start:offset]
			offset++
			if offset >= len(value) || value[offset] != ']' {
				return nil, false
			}
			offset++
			property, ok := decodeBagQuotedProperty(encoded, quote)
			if !ok {
				return nil, false
			}
			properties = append(properties, property)
		default:
			return nil, false
		}
	}
	return properties, len(properties) > 0
}

func isBagIdentifierStart(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z' || value == '_'
}

func isBagIdentifierContinue(value byte) bool {
	return isBagIdentifierStart(value) || value >= '0' && value <= '9'
}

func decodeBagQuotedProperty(value string, quote byte) (string, bool) {
	if quote == '\'' {
		var encoded strings.Builder
		encoded.Grow(len(value))
		for offset := 0; offset < len(value); offset++ {
			switch value[offset] {
			case '"':
				encoded.WriteString(`\"`)
			case '\\':
				if offset+1 >= len(value) {
					return "", false
				}
				offset++
				switch value[offset] {
				case '\'':
					encoded.WriteByte('\'')
				case '"':
					encoded.WriteString(`\"`)
				default:
					encoded.WriteByte('\\')
					encoded.WriteByte(value[offset])
				}
			default:
				encoded.WriteByte(value[offset])
			}
		}
		value = encoded.String()
	}
	var decoded string
	if json.Unmarshal([]byte(`"`+value+`"`), &decoded) != nil {
		return "", false
	}
	return decoded, true
}

func kqlSetHasElement(setValue, elementValue any) any {
	setText, ok := setValue.(string)
	if !ok || len(setText) > maxDynamicInputBytes || !utf8.ValidString(setText) {
		return nil
	}
	element, ok := sqliteScalar(elementValue)
	if !ok {
		return nil
	}
	var values []json.RawMessage
	if err := json.Unmarshal([]byte(setText), &values); err != nil || values == nil || len(values) > maxSetValues {
		return nil
	}
	for _, encoded := range values {
		value, scalar := decodeJSONScalar(encoded)
		if scalar && sqliteScalarEqual(value, element) {
			return true
		}
	}
	return false
}

type kqlScalar struct {
	kind   byte
	text   string
	number normalizedNumber
}

type normalizedNumber struct {
	negative bool
	digits   string
	scale    int64
}

func sqliteScalar(value any) (kqlScalar, bool) {
	switch typed := value.(type) {
	case string:
		if len(typed) > maxDynamicInputBytes || !utf8.ValidString(typed) {
			return kqlScalar{}, false
		}
		return kqlScalar{kind: 's', text: typed}, true
	case int64:
		number, _ := normalizeNumber(strconv.FormatInt(typed, 10))
		return kqlScalar{kind: 'n', number: number}, true
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return kqlScalar{}, false
		}
		number, ok := normalizeNumber(strconv.FormatFloat(typed, 'g', -1, 64))
		return kqlScalar{kind: 'n', number: number}, ok
	default:
		return kqlScalar{}, false
	}
}

func decodeJSONScalar(value json.RawMessage) (kqlScalar, bool) {
	if len(value) == 0 {
		return kqlScalar{}, false
	}
	switch value[0] {
	case '"':
		var text string
		if json.Unmarshal(value, &text) != nil {
			return kqlScalar{}, false
		}
		return kqlScalar{kind: 's', text: text}, true
	case 't':
		number, _ := normalizeNumber("1")
		return kqlScalar{kind: 'n', number: number}, true
	case 'f':
		number, _ := normalizeNumber("0")
		return kqlScalar{kind: 'n', number: number}, true
	case 'n', '{', '[':
		return kqlScalar{}, false
	default:
		number, ok := normalizeNumber(string(value))
		return kqlScalar{kind: 'n', number: number}, ok
	}
}

func sqliteScalarEqual(left, right kqlScalar) bool {
	if left.kind != right.kind {
		return false
	}
	if left.kind == 's' || left.kind == 'b' {
		return left.text == right.text
	}
	return left.number == right.number
}

func normalizeNumber(value string) (normalizedNumber, bool) {
	negative := false
	if strings.HasPrefix(value, "-") {
		negative = true
		value = value[1:]
	} else if strings.HasPrefix(value, "+") {
		value = value[1:]
	}
	exponent := int64(0)
	if index := strings.IndexAny(value, "eE"); index >= 0 {
		mantissa := strings.Trim(value[:index], "0.")
		if mantissa == "" {
			return normalizedNumber{digits: "0"}, true
		}
		parsed, err := strconv.ParseInt(value[index+1:], 10, 64)
		if err != nil {
			return normalizedNumber{}, false
		}
		exponent = parsed
		value = value[:index]
	}
	fractional := int64(0)
	if index := strings.IndexByte(value, '.'); index >= 0 {
		fractional = int64(len(value) - index - 1)
		value = value[:index] + value[index+1:]
	}
	value = strings.TrimLeft(value, "0")
	if value == "" {
		return normalizedNumber{digits: "0"}, true
	}
	if exponent < math.MinInt64+fractional {
		return normalizedNumber{}, false
	}
	scale := exponent - fractional
	trailing := len(value) - len(strings.TrimRight(value, "0"))
	if trailing > 0 {
		if scale > math.MaxInt64-int64(trailing) {
			return normalizedNumber{}, false
		}
		value = value[:len(value)-trailing]
		scale += int64(trailing)
	}
	return normalizedNumber{negative: negative, digits: value, scale: scale}, true
}

func kqlIPv4IsPrivate(value any) any {
	text, ok := value.(string)
	if !ok {
		return nil
	}
	address, ok := parseIPv4Address(text, true)
	if !ok {
		return nil
	}
	for _, prefix := range privateIPv4Prefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func kqlIPv4IsInRange(addressValue, rangeValue any) any {
	addressText, addressOK := addressValue.(string)
	rangeText, rangeOK := rangeValue.(string)
	if !addressOK || !rangeOK || len(rangeText) > maxIPv4RangeBytes || !utf8.ValidString(rangeText) {
		return nil
	}
	address, ok := parseIPv4Address(addressText, false)
	if !ok {
		return nil
	}
	members := strings.Split(rangeText, ",")
	if len(members) < 1 || len(members) > maxIPv4Ranges {
		return nil
	}
	matched := false
	for _, member := range members {
		prefix, ok := parseIPv4Range(strings.TrimSpace(member))
		if !ok {
			return nil
		}
		if prefix.Contains(address) {
			matched = true
		}
	}
	return matched
}

func parseIPv4Address(value string, allowPrefix bool) (netip.Addr, bool) {
	if len(value) < 1 || len(value) > maxIPv4AddressBytes || !utf8.ValidString(value) {
		return netip.Addr{}, false
	}
	if strings.Contains(value, "/") {
		if !allowPrefix {
			return netip.Addr{}, false
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is4() || prefix.Addr().Is4In6() {
			return netip.Addr{}, false
		}
		return prefix.Addr(), true
	}
	address, err := netip.ParseAddr(value)
	if err != nil || !address.Is4() || address.Is4In6() {
		return netip.Addr{}, false
	}
	return address, true
}

func parseIPv4Range(value string) (netip.Prefix, bool) {
	if value == "" || len(value) > maxIPv4AddressBytes {
		return netip.Prefix{}, false
	}
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is4() || prefix.Addr().Is4In6() {
			return netip.Prefix{}, false
		}
		return prefix.Masked(), true
	}
	address, ok := parseIPv4Address(value, false)
	if !ok {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(address, 32), true
}

// textArgument reads SQLite text, treating SQL null ([]byte(nil)) as empty.
func textArgument(value any) (string, bool) {
	switch text := value.(type) {
	case string:
		return text, true
	case []byte:
		return string(text), true
	}
	return "", false
}

func kqlKeyValues(valueValue, optionsValue any) (any, error) {
	value, valueOK := textArgument(valueValue)
	options, optionsOK := textArgument(optionsValue)
	if !valueOK || !optionsOK {
		return nil, nil
	}
	return runtime.KeyValuesJSON(value, options)
}
