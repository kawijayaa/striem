package database

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"math/bits"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// xxhash64 with seed 0, the algorithm Kusto's hash() documents.
func xxhash64(data []byte) uint64 {
	const (
		p1 uint64 = 11400714785074694791
		p2 uint64 = 14029467366897019727
		p3 uint64 = 1609587929392839161
		p4 uint64 = 9650029242287828579
		p5 uint64 = 2870177450012600261
	)
	round := func(acc, input uint64) uint64 {
		return bits.RotateLeft64(acc+input*p2, 31) * p1
	}
	merge := func(acc, value uint64) uint64 {
		return (acc^round(0, value))*p1 + p4
	}
	n := len(data)
	var h uint64
	i := 0
	if n >= 32 {
		prime1, prime2 := p1, p2 // variables wrap where constant arithmetic overflows
		v1, v2, v3, v4 := prime1+prime2, prime2, uint64(0), -prime1
		for ; i+32 <= n; i += 32 {
			v1 = round(v1, binary.LittleEndian.Uint64(data[i:]))
			v2 = round(v2, binary.LittleEndian.Uint64(data[i+8:]))
			v3 = round(v3, binary.LittleEndian.Uint64(data[i+16:]))
			v4 = round(v4, binary.LittleEndian.Uint64(data[i+24:]))
		}
		h = bits.RotateLeft64(v1, 1) + bits.RotateLeft64(v2, 7) + bits.RotateLeft64(v3, 12) + bits.RotateLeft64(v4, 18)
		h = merge(merge(merge(merge(h, v1), v2), v3), v4)
	} else {
		h = p5
	}
	h += uint64(n)
	for ; i+8 <= n; i += 8 {
		h ^= round(0, binary.LittleEndian.Uint64(data[i:]))
		h = bits.RotateLeft64(h, 27)*p1 + p4
	}
	if i+4 <= n {
		h ^= uint64(binary.LittleEndian.Uint32(data[i:])) * p1
		h = bits.RotateLeft64(h, 23)*p2 + p3
		i += 4
	}
	for ; i < n; i++ {
		h ^= uint64(data[i]) * p5
		h = bits.RotateLeft64(h, 11) * p1
	}
	h ^= h >> 33
	h *= p2
	h ^= h >> 29
	h *= p3
	h ^= h >> 32
	return h
}

// dotnetEpochTicks is the number of 100 ns ticks from 0001-01-01 to 1970.
const dotnetEpochTicks = 621355968000000000

// hashBytes is the byte form Kusto hashes: UTF-8 text, little-endian
// integers and reals, .NET ticks for datetimes and timespans, and .NET
// GUID bytes.
func hashBytes(value any, kind string) ([]byte, bool) {
	if value == nil || sqliteNull(value) {
		return nil, false
	}
	if raw, ok := value.([]byte); ok {
		value = string(raw)
	}
	buffer := make([]byte, 8)
	switch kind {
	case "datetime":
		when, ok := parsedDatetime(value)
		if !ok {
			return nil, false
		}
		ticks := datetimeTicks(when).Int64() + dotnetEpochTicks
		binary.LittleEndian.PutUint64(buffer, uint64(ticks))
		return buffer, true
	case "guid":
		if text, ok := value.(string); ok {
			return guidBytes(strings.ToLower(text))
		}
	case "bool":
		if number, ok := value.(int64); ok {
			if number != 0 {
				return []byte{1}, true
			}
			return []byte{0}, true
		}
	case "int":
		if number, ok := value.(int64); ok {
			small := make([]byte, 4)
			binary.LittleEndian.PutUint32(small, uint32(int32(number)))
			return small, true
		}
	}
	switch v := value.(type) {
	case string:
		return []byte(v), true
	case int64:
		binary.LittleEndian.PutUint64(buffer, uint64(v))
		return buffer, true
	case float64:
		binary.LittleEndian.PutUint64(buffer, math.Float64bits(v))
		return buffer, true
	}
	return nil, false
}

func kqlHash(value any, kind string, modValue any) any {
	data, ok := hashBytes(value, kind)
	if !ok {
		return nil
	}
	hash := int64(xxhash64(data))
	if modValue == nil || sqliteNull(modValue) {
		return hash
	}
	mod, ok := modValue.(int64)
	if !ok || mod <= 0 {
		return nil
	}
	return int64(uint64(hash) % uint64(mod))
}

// kqlHashMany combines the values' hashes. Kusto documents that the
// combination may change and is only comparable within one query.
func kqlHashMany(kinds string, values ...any) any {
	types := strings.Split(kinds, ",")
	if len(types) != len(values) {
		return nil
	}
	combined := make([]byte, 0, 8*len(values))
	for i, value := range values {
		data, ok := hashBytes(value, types[i])
		var hash uint64
		if ok {
			hash = xxhash64(data)
		}
		combined = binary.LittleEndian.AppendUint64(combined, hash)
	}
	return int64(xxhash64(combined))
}

// kqlParseCommandLine splits a Windows command line as CommandLineToArgvW
// does: the program name ends at the first space outside quotes, and later
// arguments follow the backslash-and-quote escaping rules.
func kqlParseCommandLine(commandValue, parserValue any) (any, error) {
	command, ok := textArgument(commandValue)
	parser, okParser := textArgument(parserValue)
	if !ok || !okParser || sqliteNull(commandValue) || !strings.EqualFold(parser, "windows") || len(command) > maxReplacementBytes {
		return nil, nil
	}
	args := []string{}
	i := 0
	// The program name: quotes only group, without escapes.
	var program strings.Builder
	quoted := false
	for ; i < len(command); i++ {
		c := command[i]
		if c == '"' {
			quoted = !quoted
			continue
		}
		if !quoted && (c == ' ' || c == '\t') {
			break
		}
		program.WriteByte(c)
	}
	args = append(args, program.String())
	for {
		for i < len(command) && (command[i] == ' ' || command[i] == '\t') {
			i++
		}
		if i >= len(command) {
			break
		}
		var arg strings.Builder
		quoted = false
		for i < len(command) {
			c := command[i]
			if !quoted && (c == ' ' || c == '\t') {
				break
			}
			if c == '\\' {
				backslashes := 0
				for i < len(command) && command[i] == '\\' {
					backslashes++
					i++
				}
				if i < len(command) && command[i] == '"' {
					arg.WriteString(strings.Repeat(`\`, backslashes/2))
					if backslashes%2 == 1 {
						arg.WriteByte('"')
						i++
					}
				} else {
					arg.WriteString(strings.Repeat(`\`, backslashes))
				}
				continue
			}
			if c == '"' {
				// Inside quotes, "" is a literal quote.
				if quoted && i+1 < len(command) && command[i+1] == '"' {
					arg.WriteByte('"')
					i += 2
					continue
				}
				quoted = !quoted
				i++
				continue
			}
			arg.WriteByte(c)
			i++
		}
		args = append(args, arg.String())
	}
	encoded, err := json.Marshal(args)
	return string(encoded), err
}

var schemePattern = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9+.-]+)://`)

// kqlParsePath splits a Windows, UNC, POSIX or file:// path into Kusto's
// parse_path parts.
func kqlParsePath(value any) (any, error) {
	path, ok := textArgument(value)
	if !ok || sqliteNull(value) || len(path) > maxReplacementBytes {
		return nil, nil
	}
	parts := struct {
		Scheme                  string
		RootPath                string
		DirectoryPath           string
		DirectoryName           string
		Filename                string
		Extension               string
		AlternateDataStreamName string
	}{}
	if match := schemePattern.FindStringSubmatch(path); match != nil {
		parts.Scheme, path = match[1], path[len(match[0]):]
	}
	if len(path) >= 2 && path[1] == ':' && (path[0] >= 'A' && path[0] <= 'Z' || path[0] >= 'a' && path[0] <= 'z') {
		parts.RootPath = path[:2]
	}
	separator := strings.LastIndexAny(path, `\/`)
	filename := path
	if separator >= 0 {
		parts.DirectoryPath, filename = path[:separator], path[separator+1:]
		directory := parts.DirectoryPath
		parts.DirectoryName = directory[strings.LastIndexAny(directory, `\/`)+1:]
	}
	// file.txt:stream names an NTFS alternate data stream.
	if colon := strings.Index(filename, ":"); colon >= 0 && !(separator < 0 && parts.RootPath != "" && colon == 1) {
		filename, parts.AlternateDataStreamName = filename[:colon], filename[colon+1:]
	}
	parts.Filename = filename
	if dot := strings.LastIndex(filename, "."); dot >= 0 {
		parts.Extension = filename[dot+1:]
	}
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(parts); err != nil {
		return nil, err
	}
	return strings.TrimSuffix(buffer.String(), "\n"), nil
}

func kqlRegexQuote(value any) any {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) {
		return nil
	}
	return regexp.QuoteMeta(text)
}

// kqlIndexOfRegex returns the zero-based character index of the given
// non-overlapping occurrence of a regex within [start, start+length).
func kqlIndexOfRegex(values ...any) (any, error) {
	if len(values) != 5 {
		return nil, nil
	}
	text, ok := textArgument(values[0])
	pattern, okPattern := textArgument(values[1])
	start, okStart := values[2].(int64)
	length, okLength := values[3].(int64)
	occurrence, okOccurrence := values[4].(int64)
	if !ok || !okPattern || sqliteNull(values[0]) || !okStart || !okLength || !okOccurrence || start < 0 || length < -1 || occurrence < 0 {
		return nil, nil
	}
	if len(pattern) > 512 || len(text) > maxReplacementBytes {
		return nil, nil
	}
	expression, err := regexp.Compile(pattern)
	if err != nil {
		return nil, nil
	}
	runes := []rune(text)
	if start > int64(len(runes)) {
		return int64(-1), nil
	}
	end := int64(len(runes))
	if length >= 0 && start+length < end {
		end = start + length
	}
	window := string(runes[start:end])
	if occurrence == 0 {
		occurrence = 1
	}
	matches := expression.FindAllStringIndex(window, int(occurrence))
	if int64(len(matches)) < occurrence {
		return int64(-1), nil
	}
	return start + int64(utf8.RuneCountInString(window[:matches[occurrence-1][0]])), nil
}

// kqlHasAnyIndex returns the index of the first lookup term the source
// has (whole-term, case-insensitive), or -1.
func kqlHasAnyIndex(sourceValue, lookupsValue any) any {
	source, ok := textArgument(sourceValue)
	lookupsText, okLookups := textArgument(lookupsValue)
	if !ok || !okLookups || sqliteNull(lookupsValue) {
		return nil
	}
	if sqliteNull(sourceValue) {
		source = ""
	}
	var lookups []any
	if json.Unmarshal([]byte(lookupsText), &lookups) != nil || len(lookups) > 1024 {
		return nil
	}
	lower := strings.ToLower(source)
	for i, lookup := range lookups {
		term := ""
		switch v := lookup.(type) {
		case string:
			term = v
		case float64:
			term = strconv.FormatFloat(v, 'f', -1, 64)
		case bool:
			term = strconv.FormatBool(v)
		default:
			continue
		}
		if term != "" && hasTerm(lower, strings.ToLower(term)) {
			return int64(i)
		}
	}
	return int64(-1)
}

// hasTerm reports a term bounded by non-alphanumeric characters.
func hasTerm(source, term string) bool {
	for offset := 0; offset <= len(source)-len(term); {
		index := strings.Index(source[offset:], term)
		if index < 0 {
			return false
		}
		start, end := offset+index, offset+index+len(term)
		before := start == 0 || !isAlphaNumeric(source[start-1])
		after := end == len(source) || !isAlphaNumeric(source[end])
		if before && after {
			return true
		}
		offset = start + 1
	}
	return false
}

// jsonPathValue follows a JSONPath of $, .name, ['name'] and [index]
// steps.
func jsonPathValue(document any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "$") {
		return nil, false
	}
	current := document
	rest := path[1:]
	for rest != "" {
		switch {
		case rest[0] == '.':
			rest = rest[1:]
			end := strings.IndexAny(rest, ".[")
			if end < 0 {
				end = len(rest)
			}
			name := rest[:end]
			rest = rest[end:]
			object, ok := current.(map[string]any)
			if !ok || name == "" {
				return nil, false
			}
			if current, ok = object[name]; !ok {
				return nil, false
			}
		case rest[0] == '[':
			end := strings.Index(rest, "]")
			if end < 0 {
				return nil, false
			}
			key := strings.TrimSpace(rest[1:end])
			rest = rest[end+1:]
			if len(key) >= 2 && (key[0] == '\'' || key[0] == '"') && key[len(key)-1] == key[0] {
				object, ok := current.(map[string]any)
				if !ok {
					return nil, false
				}
				if current, ok = object[key[1:len(key)-1]]; !ok {
					return nil, false
				}
				continue
			}
			index, err := strconv.Atoi(key)
			array, ok := current.([]any)
			if err != nil || !ok {
				return nil, false
			}
			if index < 0 {
				index += len(array)
			}
			if index < 0 || index >= len(array) {
				return nil, false
			}
			current = array[index]
		default:
			return nil, false
		}
	}
	return current, true
}

// kqlExtractJSON implements extract_json/extractjson: the value at a
// JSONPath, converted to the requested kind (text by default).
func kqlExtractJSON(pathValue, documentValue, kindValue any) any {
	path, ok := textArgument(pathValue)
	text, okText := textArgument(documentValue)
	kind, _ := textArgument(kindValue)
	if !ok || !okText || sqliteNull(documentValue) || len(text) > maxReplacementBytes {
		return nil
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var document any
	if decoder.Decode(&document) != nil {
		return nil
	}
	value, ok := jsonPathValue(document, path)
	if !ok || value == nil {
		return nil
	}
	switch kind {
	case "long", "int":
		switch v := value.(type) {
		case json.Number:
			if number, err := v.Int64(); err == nil {
				return number
			}
		case string:
			if number, err := strconv.ParseInt(v, 10, 64); err == nil {
				return number
			}
		}
		return nil
	case "real", "double":
		switch v := value.(type) {
		case json.Number:
			if number, err := v.Float64(); err == nil {
				return number
			}
		case string:
			if number, err := strconv.ParseFloat(v, 64); err == nil {
				return number
			}
		}
		return nil
	case "bool":
		switch v := value.(type) {
		case bool:
			return boolNumber(v)
		case string:
			if parsed, err := strconv.ParseBool(v); err == nil {
				return boolNumber(parsed)
			}
		}
		return nil
	case "datetime":
		if v, ok := value.(string); ok {
			return kqlToDatetime(v)
		}
		return nil
	}
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case bool:
		return strconv.FormatBool(v)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return string(encoded)
}

func boolNumber(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

// kqlDynamicToJSON writes dynamic values as JSON with object keys sorted,
// so equal values have equal text.
func kqlDynamicToJSON(value any) (any, error) {
	text, ok := textArgument(value)
	if !ok || sqliteNull(value) || len(text) > maxReplacementBytes {
		return nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var document any
	if decoder.Decode(&document) != nil {
		encoded, err := json.Marshal(text)
		return string(encoded), err
	}
	var buffer bytes.Buffer
	writeSortedJSON(&buffer, document)
	return buffer.String(), nil
}

func writeSortedJSON(buffer *bytes.Buffer, value any) {
	switch v := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		buffer.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				buffer.WriteByte(',')
			}
			encoded, _ := json.Marshal(key)
			buffer.Write(encoded)
			buffer.WriteByte(':')
			writeSortedJSON(buffer, v[key])
		}
		buffer.WriteByte('}')
	case []any:
		buffer.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				buffer.WriteByte(',')
			}
			writeSortedJSON(buffer, item)
		}
		buffer.WriteByte(']')
	default:
		encoded, _ := json.Marshal(v)
		buffer.Write(encoded)
	}
}

