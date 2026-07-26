package database

import (
	"database/sql"
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
)

func querySQLiteFunction(t *testing.T, db *sql.DB, query string, arguments ...any) any {
	t.Helper()
	var value any
	if err := db.QueryRow(query, arguments...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func TestKQLParse(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	sixteenCaptures := strings.Repeat("(a)", maxParseCaptures)
	sixteenTypes := strings.TrimSuffix(strings.Repeat("string,", maxParseCaptures), ",")
	sixteenValues := strings.TrimSuffix(strings.Repeat(`"a",`, maxParseCaptures), ",")
	tests := []struct {
		name    string
		pattern any
		types   any
		value   any
		want    any
	}{
		{name: "typed captures", pattern: `^([^:]+):(-?\d+):([0-9.]+)$`, types: "string,long,real", value: "alpha:-42:3.5", want: `["alpha",-42,3.5]`},
		{name: "empty capture list", pattern: `^match$`, types: "", value: "match", want: `[]`},
		{name: "maximum captures", pattern: sixteenCaptures, types: sixteenTypes, value: strings.Repeat("a", maxParseCaptures), want: `[` + sixteenValues + `]`},
		{name: "maximum pattern bytes", pattern: strings.Repeat("a?", maxParsePattern/2), types: "", value: "a", want: `[]`},
		{name: "maximum source bytes", pattern: `^a+$`, types: "", value: strings.Repeat("a", maxParseSource), want: `[]`},
		{name: "no match", pattern: `^(a)$`, types: "string", value: "b"},
		{name: "invalid pattern", pattern: `(`, types: "string", value: "a"},
		{name: "too many captures", pattern: sixteenCaptures + `(a)`, types: sixteenTypes + ",string", value: strings.Repeat("a", maxParseCaptures+1)},
		{name: "pattern too large", pattern: strings.Repeat("a?", maxParsePattern/2) + "a", types: "", value: "a"},
		{name: "descriptor too large", pattern: `^(a)$`, types: strings.Repeat("string", maxParseTypes), value: "a"},
		{name: "source too large", pattern: `^a+$`, types: "", value: strings.Repeat("a", maxParseSource+1)},
		{name: "descriptor count mismatch", pattern: `^(a)(b)$`, types: "string", value: "ab"},
		{name: "unknown descriptor", pattern: `^(1)$`, types: "int", value: "1"},
		{name: "invalid long", pattern: `^(.*)$`, types: "long", value: "1.5"},
		{name: "overflowing long", pattern: `^(.*)$`, types: "long", value: "9223372036854775808"},
		{name: "invalid real", pattern: `^(.*)$`, types: "real", value: "number"},
		{name: "non-finite real", pattern: `^(.*)$`, types: "real", value: "NaN"},
		{name: "null pattern", pattern: nil, types: "", value: ""},
		{name: "blob source", pattern: `^(a)$`, types: "string", value: []byte("a")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got any
			if err := store.DB().QueryRow("SELECT kql_parse(?, ?, ?)", test.pattern, test.types, test.value).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("kql_parse() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestKQLArrayLength(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{name: "empty", value: `[]`, want: int64(0)},
		{name: "values", value: `[1,null,{"nested":[2,3]}]`, want: int64(3)},
		{name: "surrounding whitespace", value: " \n [true, false] \t", want: int64(2)},
		{name: "malformed", value: `[1,`},
		{name: "object", value: `{}`},
		{name: "JSON null", value: `null`},
		{name: "string", value: `"value"`},
		{name: "SQL integer", value: int64(1)},
		{name: "blob", value: []byte(`[]`)},
		{name: "SQL null", value: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got any
			if err := store.DB().QueryRow("SELECT kql_array_length(?)", test.value).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("kql_array_length() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestKQLBagKeys(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{name: "sorted", value: `{"z":1,"a":2,"middle":{"nested":true}}`, want: `["a","middle","z"]`},
		{name: "empty", value: `{}`, want: `[]`},
		{name: "whitespace", value: " \n {\"b\": 1, \"a\": 2} \t", want: `["a","b"]`},
		{name: "malformed", value: `{"a":`},
		{name: "array", value: `[{"a":1}]`},
		{name: "JSON null", value: `null`},
		{name: "scalar", value: `1`},
		{name: "SQL integer", value: int64(1)},
		{name: "blob", value: []byte(`{"a":1}`)},
		{name: "SQL null", value: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got any
			if err := store.DB().QueryRow("SELECT kql_bag_keys(?)", test.value).Scan(&got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("kql_bag_keys() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestKQLToDatetime(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{name: "UTC", value: "2024-01-02T03:04:05Z", want: "2024-01-02T03:04:05.000000000Z"},
		{name: "offset normalized", value: "2024-01-02T03:04:05.123456789+02:30", want: "2024-01-02T00:34:05.123456789Z"},
		{name: "timezone-less T", value: "2024-01-02T03:04:05.25", want: "2024-01-02T03:04:05.250000000Z"},
		{name: "timezone-less space", value: "2024-01-02 03:04:05", want: "2024-01-02T03:04:05.000000000Z"},
		{name: "date only", value: "2024-02-29", want: "2024-02-29T00:00:00.000000000Z"},
		{name: "invalid date", value: "2023-02-29T00:00:00Z"},
		{name: "invalid time", value: "2024-01-02T25:00:00Z"},
		{name: "missing seconds", value: "2024-01-02T03:04Z"},
		{name: "unsafe SQLite syntax", value: "2024-01-02 03:04:05 UTC"},
		{name: "surrounding whitespace", value: " 2024-01-02T03:04:05Z"},
		{name: "SQL integer", value: int64(0)},
		{name: "blob", value: []byte("2024-01-02T03:04:05Z")},
		{name: "SQL null", value: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got sql.NullString
			if err := store.DB().QueryRow("SELECT kql_todatetime(?)", test.value).Scan(&got); err != nil {
				t.Fatal(err)
			}
			var want sql.NullString
			if test.want != nil {
				want = sql.NullString{String: test.want.(string), Valid: true}
			}
			if got != want {
				t.Fatalf("kql_todatetime() = %#v, want %#v", got, want)
			}
		})
	}
}

func TestKQLParseKV(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	schema := `[{"name":"name","type":"string"},{"name":"count","type":"long"},{"name":"score","type":"real"}]`
	maximumSchema := `[ {"name":"name","type":"string"} ]` + strings.Repeat(" ", maxParseKVSchema-len(`[ {"name":"name","type":"string"} ]`))
	maximumSource := "name=" + strings.Repeat("a", maxParseSource-len("name="))
	maximumKeys := make([]map[string]string, maxParseKVKeys)
	maximumValues := make([]string, maxParseKVKeys)
	for index := range maximumKeys {
		maximumKeys[index] = map[string]string{"name": "k" + string(rune('a'+index)), "type": "string"}
		maximumValues[index] = `""`
	}
	maximumKeysJSON, err := json.Marshal(maximumKeys)
	if err != nil {
		t.Fatal(err)
	}
	tooManyKeysJSON, err := json.Marshal(append(maximumKeys, map[string]string{"name": "extra", "type": "string"}))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name          string
		schema        any
		pairDelimiter any
		kvDelimiter   any
		source        any
		want          any
	}{
		{name: "typed values and source order", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: " score = 3.5, name = alice , count = -42 ", want: `["alice",-42,3.5]`},
		{name: "missing values", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: "other=1", want: `["",null,null]`},
		{name: "empty string", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: "name= ", want: `["",null,null]`},
		{name: "first duplicate wins", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: "name=first,name=second,count=1,count=2", want: `["first",1,null]`},
		{name: "invalid first duplicate stays null", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: "count=bad,count=2", want: `["",null,null]`},
		{name: "invalid numerics are null", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: "count=9223372036854775808,score=NaN", want: `["",null,null]`},
		{name: "infinite real is null", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: "score=+Inf", want: `["",null,null]`},
		{name: "split value once", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: "name=a=b", want: `["a=b",null,null]`},
		{name: "multi-character delimiters", schema: schema, pairDelimiter: "||", kvDelimiter: "=>", source: "name=>alice||count=>7", want: `["alice",7,null]`},
		{name: "malformed and undeclared pairs ignored", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: "broken,other=1,name=ok", want: `["ok",null,null]`},
		{name: "maximum keys", schema: string(maximumKeysJSON), pairDelimiter: ",", kvDelimiter: "=", source: "", want: `[` + strings.Join(maximumValues, ",") + `]`},
		{name: "maximum schema bytes", schema: maximumSchema, pairDelimiter: ",", kvDelimiter: "=", source: "name=value", want: `["value"]`},
		{name: "maximum delimiter bytes", schema: `[{"name":"name","type":"string"}]`, pairDelimiter: strings.Repeat("p", maxParseKVDelimiter), kvDelimiter: strings.Repeat("k", maxParseKVDelimiter), source: "name" + strings.Repeat("k", maxParseKVDelimiter) + "value", want: `["value"]`},
		{name: "maximum source bytes", schema: `[{"name":"name","type":"string"}]`, pairDelimiter: ",", kvDelimiter: "=", source: maximumSource, want: `["` + strings.Repeat("a", maxParseSource-len("name=")) + `"]`},
		{name: "malformed schema", schema: `[`, pairDelimiter: ",", kvDelimiter: "=", source: "name=a"},
		{name: "non-array schema", schema: `{}`, pairDelimiter: ",", kvDelimiter: "=", source: "name=a"},
		{name: "empty schema", schema: `[]`, pairDelimiter: ",", kvDelimiter: "=", source: "name=a"},
		{name: "missing schema field", schema: `[{"name":"name"}]`, pairDelimiter: ",", kvDelimiter: "=", source: "name=a"},
		{name: "extra schema field", schema: `[{"name":"name","type":"string","extra":true}]`, pairDelimiter: ",", kvDelimiter: "=", source: "name=a"},
		{name: "empty schema name", schema: `[{"name":"","type":"string"}]`, pairDelimiter: ",", kvDelimiter: "=", source: "=a"},
		{name: "unsupported schema type", schema: `[{"name":"name","type":"bool"}]`, pairDelimiter: ",", kvDelimiter: "=", source: "name=true"},
		{name: "case-insensitive duplicate schema name", schema: `[{"name":"Name","type":"string"},{"name":"name","type":"string"}]`, pairDelimiter: ",", kvDelimiter: "=", source: "Name=a"},
		{name: "too many keys", schema: string(tooManyKeysJSON), pairDelimiter: ",", kvDelimiter: "=", source: ""},
		{name: "schema too large", schema: maximumSchema + " ", pairDelimiter: ",", kvDelimiter: "=", source: "name=a"},
		{name: "empty pair delimiter", schema: schema, pairDelimiter: "", kvDelimiter: "=", source: "name=a"},
		{name: "empty key value delimiter", schema: schema, pairDelimiter: ",", kvDelimiter: "", source: "name=a"},
		{name: "pair delimiter too large", schema: schema, pairDelimiter: strings.Repeat("p", maxParseKVDelimiter+1), kvDelimiter: "=", source: "name=a"},
		{name: "key value delimiter too large", schema: schema, pairDelimiter: ",", kvDelimiter: strings.Repeat("k", maxParseKVDelimiter+1), source: "name=a"},
		{name: "source too large", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: maximumSource + "a"},
		{name: "invalid UTF-8 source", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: string([]byte{0xff})},
		{name: "invalid UTF-8 delimiter", schema: schema, pairDelimiter: string([]byte{0xff}), kvDelimiter: "=", source: "name=a"},
		{name: "null schema", schema: nil, pairDelimiter: ",", kvDelimiter: "=", source: "name=a"},
		{name: "blob schema", schema: []byte(schema), pairDelimiter: ",", kvDelimiter: "=", source: "name=a"},
		{name: "wrong delimiter type", schema: schema, pairDelimiter: int64(1), kvDelimiter: "=", source: "name=a"},
		{name: "null source", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: nil},
		{name: "blob source", schema: schema, pairDelimiter: ",", kvDelimiter: "=", source: []byte("name=a")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := querySQLiteFunction(t, store.DB(), "SELECT kql_parse_kv(?, ?, ?, ?)", test.schema, test.pairDelimiter, test.kvDelimiter, test.source)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("kql_parse_kv() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestKQLBase64DecodeToString(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{name: "ASCII", value: "aGVsbG8=", want: "hello"},
		{name: "Unicode", value: "8J+MjQ==", want: "🌍"},
		{name: "empty", value: "", want: ""},
		{name: "maximum input", value: strings.Repeat("A", maxDecodeInputBytes), want: strings.Repeat("\x00", maxDecodeInputBytes/4*3)},
		{name: "malformed alphabet", value: "!!!!"},
		{name: "unpadded", value: "aGVsbG8"},
		{name: "URL safe", value: "_w=="},
		{name: "noncanonical trailing bits", value: "Zh=="},
		{name: "newline", value: "aGVs\nbG8="},
		{name: "space", value: "aGVs bG8="},
		{name: "invalid UTF-8 result", value: "/w=="},
		{name: "oversized", value: strings.Repeat("A", maxDecodeInputBytes+4)},
		{name: "integer", value: int64(1)},
		{name: "blob", value: []byte("aGVsbG8=")},
		{name: "SQL null", value: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := querySQLiteFunction(t, store.DB(), "SELECT kql_base64_decode_tostring(?)", test.value)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("kql_base64_decode_tostring() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestKQLURLDecode(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{name: "encoded URL", value: "https%3A%2F%2Fexample.com%2Fa%3Fb%3Dc", want: "https://example.com/a?b=c"},
		{name: "Unicode", value: "%E2%98%83", want: "☃"},
		{name: "literal plus preserved", value: "%2B+%20", want: "++ "},
		{name: "one pass", value: "%252F", want: "%2F"},
		{name: "empty", value: "", want: ""},
		{name: "maximum input", value: strings.Repeat("a", maxDecodeInputBytes), want: strings.Repeat("a", maxDecodeInputBytes)},
		{name: "incomplete escape", value: "%"},
		{name: "invalid escape", value: "%G0"},
		{name: "invalid decoded UTF-8", value: "%FF"},
		{name: "invalid source UTF-8", value: string([]byte{0xff})},
		{name: "oversized", value: strings.Repeat("a", maxDecodeInputBytes+1)},
		{name: "integer", value: int64(1)},
		{name: "blob", value: []byte("%2F")},
		{name: "SQL null", value: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := querySQLiteFunction(t, store.DB(), "SELECT kql_url_decode(?)", test.value)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("kql_url_decode() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestKQLBagHasKey(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	deepBag := `1`
	for range maxBagPathDepth {
		deepBag = `{"a":` + deepBag + `}`
	}
	deepPath := "$" + strings.Repeat(".a", maxBagPathDepth)
	maximumBag := `{"a":1}` + strings.Repeat(" ", maxDynamicInputBytes-len(`{"a":1}`))
	maximumKey := strings.Repeat("k", maxBagPathBytes)
	maximumKeyBag := `{"` + maximumKey + `":true}`
	tests := []struct {
		name string
		bag  any
		key  any
		want any
	}{
		{name: "root present", bag: `{"a":1}`, key: "a", want: int64(1)},
		{name: "root missing", bag: `{"a":1}`, key: "b", want: int64(0)},
		{name: "case-sensitive", bag: `{"A":1}`, key: "a", want: int64(0)},
		{name: "JSON null exists", bag: `{"a":null}`, key: "a", want: int64(1)},
		{name: "literal dotted key", bag: `{"a.b":1,"a":{"b":2}}`, key: "a.b", want: int64(1)},
		{name: "root path", bag: `{}`, key: "$", want: int64(1)},
		{name: "dot path", bag: `{"a":{"b":null}}`, key: "$.a.b", want: int64(1)},
		{name: "double quoted path", bag: `{"a.b":{"c d":1}}`, key: `$["a.b"]["c d"]`, want: int64(1)},
		{name: "single quoted path", bag: `{"quote'd":1}`, key: `$['quote\'d']`, want: int64(1)},
		{name: "single quoted path with double quote", bag: `{"quote\"d":1}`, key: `$['quote\"d']`, want: int64(1)},
		{name: "escaped Unicode path", bag: `{"snow☃":1}`, key: `$["snow\u2603"]`, want: int64(1)},
		{name: "missing nested property", bag: `{"a":{}}`, key: "$.a.b", want: int64(0)},
		{name: "non-object intermediate", bag: `{"a":1}`, key: "$.a.b", want: int64(0)},
		{name: "maximum depth", bag: deepBag, key: deepPath, want: int64(1)},
		{name: "maximum path bytes", bag: maximumKeyBag, key: maximumKey, want: int64(1)},
		{name: "maximum bag bytes", bag: maximumBag, key: "a", want: int64(1)},
		{name: "array index unsupported", bag: `{"a":[1]}`, key: "$.a[0]"},
		{name: "wildcard unsupported", bag: `{"a":1}`, key: "$.*"},
		{name: "malformed path", bag: `{"a":1}`, key: "$."},
		{name: "unterminated quoted path", bag: `{"a":1}`, key: `$["a]`},
		{name: "invalid quoted escape", bag: `{"a":1}`, key: `$["\x61"]`},
		{name: "too deep", bag: deepBag, key: deepPath + ".a"},
		{name: "path too large", bag: maximumKeyBag, key: maximumKey + "x"},
		{name: "malformed JSON", bag: `{"a":`, key: "a"},
		{name: "array root", bag: `[{"a":1}]`, key: "a"},
		{name: "scalar root", bag: `1`, key: "a"},
		{name: "JSON null root", bag: `null`, key: "a"},
		{name: "bag too large", bag: maximumBag + " ", key: "a"},
		{name: "blob bag", bag: []byte(`{"a":1}`), key: "a"},
		{name: "SQL null bag", bag: nil, key: "a"},
		{name: "non-text key", bag: `{"1":1}`, key: int64(1)},
		{name: "SQL null key", bag: `{"a":1}`, key: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := querySQLiteFunction(t, store.DB(), "SELECT kql_bag_has_key(?, ?)", test.bag, test.key)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("kql_bag_has_key() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestKQLSetHasElement(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	maximumValues := make([]int, maxSetValues)
	maximumValues[len(maximumValues)-1] = 1
	maximumValuesJSON, err := json.Marshal(maximumValues)
	if err != nil {
		t.Fatal(err)
	}
	tooManyValuesJSON, err := json.Marshal(append(maximumValues, 1))
	if err != nil {
		t.Fatal(err)
	}
	maximumSet := `[1]` + strings.Repeat(" ", maxDynamicInputBytes-len(`[1]`))
	tests := []struct {
		name    string
		set     any
		element any
		want    any
	}{
		{name: "string present", set: `["alpha","beta"]`, element: "beta", want: int64(1)},
		{name: "string absent", set: `["alpha"]`, element: "beta", want: int64(0)},
		{name: "string case-sensitive", set: `["Alpha"]`, element: "alpha", want: int64(0)},
		{name: "integer and real equality", set: `[1.0]`, element: int64(1), want: int64(1)},
		{name: "real and integer equality", set: `[1]`, element: float64(1), want: int64(1)},
		{name: "decimal equality", set: `[0.1]`, element: float64(0.1), want: int64(1)},
		{name: "scientific equality", set: `[1e3]`, element: int64(1000), want: int64(1)},
		{name: "large integer remains exact", set: `[9007199254740993]`, element: float64(9007199254740992), want: int64(0)},
		{name: "true matches one", set: `[true]`, element: int64(1), want: int64(1)},
		{name: "false matches zero", set: `[false]`, element: int64(0), want: int64(1)},
		{name: "SQLite true representation", set: `[true]`, element: true, want: int64(1)},
		{name: "SQLite false representation", set: `[false]`, element: false, want: int64(1)},
		{name: "zero with excessive exponent", set: `[0e999999999999999999999]`, element: int64(0), want: int64(1)},
		{name: "nested values ignored", set: `[{"a":1},[2],3]`, element: int64(3), want: int64(1)},
		{name: "JSON null ignored", set: `[null]`, element: "null", want: int64(0)},
		{name: "maximum values", set: string(maximumValuesJSON), element: int64(1), want: int64(1)},
		{name: "maximum bytes", set: maximumSet, element: int64(1), want: int64(1)},
		{name: "malformed JSON", set: `[1,`, element: int64(1)},
		{name: "object", set: `{"a":1}`, element: int64(1)},
		{name: "scalar", set: `1`, element: int64(1)},
		{name: "JSON null array", set: `null`, element: int64(1)},
		{name: "too many values", set: string(tooManyValuesJSON), element: int64(1)},
		{name: "set too large", set: maximumSet + " ", element: int64(1)},
		{name: "null search value", set: `[null]`, element: nil},
		{name: "NaN search value", set: `[1]`, element: math.NaN()},
		{name: "infinite search value", set: `[1]`, element: math.Inf(1)},
		{name: "oversized string search value", set: `[]`, element: strings.Repeat("a", maxDynamicInputBytes+1)},
		{name: "blob search value", set: `["a"]`, element: []byte("a")},
		{name: "integer set argument", set: int64(1), element: int64(1)},
		{name: "blob set argument", set: []byte(`[1]`), element: int64(1)},
		{name: "SQL null set", set: nil, element: int64(1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := querySQLiteFunction(t, store.DB(), "SELECT kql_set_has_element(?, ?)", test.set, test.element)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("kql_set_has_element() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestKQLIPv4IsPrivate(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tests := []struct {
		name  string
		value any
		want  any
	}{
		{name: "10 lower boundary", value: "10.0.0.0", want: int64(1)},
		{name: "10 upper boundary", value: "10.255.255.255", want: int64(1)},
		{name: "before 10", value: "9.255.255.255", want: int64(0)},
		{name: "after 10", value: "11.0.0.0", want: int64(0)},
		{name: "172 lower boundary", value: "172.16.0.0", want: int64(1)},
		{name: "172 upper boundary", value: "172.31.255.255", want: int64(1)},
		{name: "before 172", value: "172.15.255.255", want: int64(0)},
		{name: "after 172", value: "172.32.0.0", want: int64(0)},
		{name: "192 lower boundary", value: "192.168.0.0", want: int64(1)},
		{name: "192 upper boundary", value: "192.168.255.255", want: int64(1)},
		{name: "before 192", value: "192.167.255.255", want: int64(0)},
		{name: "after 192", value: "192.169.0.0", want: int64(0)},
		{name: "loopback is public", value: "127.0.0.1", want: int64(0)},
		{name: "link local is public", value: "169.254.1.1", want: int64(0)},
		{name: "CIDR classified by address", value: "10.1.2.3/24", want: int64(1)},
		{name: "public CIDR", value: "8.8.8.8/0", want: int64(0)},
		{name: "invalid prefix", value: "10.0.0.1/33"},
		{name: "IPv6", value: "2001:db8::1"},
		{name: "mapped IPv6", value: "::ffff:10.0.0.1"},
		{name: "leading zero", value: "010.0.0.1"},
		{name: "shorthand", value: "10.1"},
		{name: "port", value: "10.0.0.1:80"},
		{name: "leading whitespace", value: " 10.0.0.1"},
		{name: "trailing whitespace", value: "10.0.0.1 "},
		{name: "integer", value: int64(10)},
		{name: "blob", value: []byte("10.0.0.1")},
		{name: "SQL null", value: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv4_is_private(?)", test.value)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("kql_ipv4_is_private() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestKQLIPv4IsInRange(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	maximumRanges := strings.TrimSuffix(strings.Repeat("10.0.0.0/8,", maxIPv4Ranges), ",")
	tooManyRanges := maximumRanges + ",10.0.0.0/8"
	maximumRangeBytes := strings.Repeat(" ", maxIPv4RangeBytes-len("10.0.0.1")) + "10.0.0.1"
	tests := []struct {
		name    string
		address any
		ranges  any
		want    any
	}{
		{name: "exact match", address: "10.0.0.1", ranges: "10.0.0.1", want: int64(1)},
		{name: "exact miss", address: "10.0.0.2", ranges: "10.0.0.1", want: int64(0)},
		{name: "CIDR lower boundary", address: "192.168.1.0", ranges: "192.168.1.0/24", want: int64(1)},
		{name: "CIDR upper boundary", address: "192.168.1.255", ranges: "192.168.1.0/24", want: int64(1)},
		{name: "CIDR miss", address: "192.168.2.0", ranges: "192.168.1.0/24", want: int64(0)},
		{name: "noncanonical CIDR base masked", address: "192.168.1.1", ranges: "192.168.1.255/24", want: int64(1)},
		{name: "slash zero", address: "203.0.113.1", ranges: "0.0.0.0/0", want: int64(1)},
		{name: "slash 32", address: "203.0.113.1", ranges: "203.0.113.1/32", want: int64(1)},
		{name: "comma list", address: "172.16.1.1", ranges: "10.0.0.0/8,172.16.0.0/12,192.168.0.0/16", want: int64(1)},
		{name: "member whitespace", address: "172.16.1.1", ranges: " 10.0.0.0/8 , 172.16.0.0/12 ", want: int64(1)},
		{name: "maximum members", address: "10.1.1.1", ranges: maximumRanges, want: int64(1)},
		{name: "maximum range bytes", address: "10.0.0.1", ranges: maximumRangeBytes, want: int64(1)},
		{name: "invalid later member", address: "10.1.1.1", ranges: "10.0.0.0/8,invalid"},
		{name: "empty member", address: "10.1.1.1", ranges: "10.0.0.0/8,,192.168.0.0/16"},
		{name: "empty list", address: "10.1.1.1", ranges: ""},
		{name: "too many members", address: "10.1.1.1", ranges: tooManyRanges},
		{name: "range bytes exceeded", address: "10.0.0.1", ranges: maximumRangeBytes + " "},
		{name: "invalid address prefix", address: "10.0.0.1/32", ranges: "10.0.0.0/8"},
		{name: "invalid range prefix", address: "10.0.0.1", ranges: "10.0.0.0/33"},
		{name: "IPv6 address", address: "2001:db8::1", ranges: "0.0.0.0/0"},
		{name: "IPv6 range", address: "10.0.0.1", ranges: "::/0"},
		{name: "mapped IPv6 range", address: "10.0.0.1", ranges: "::ffff:10.0.0.0/120"},
		{name: "leading zero address", address: "010.0.0.1", ranges: "10.0.0.0/8"},
		{name: "address whitespace", address: " 10.0.0.1", ranges: "10.0.0.0/8"},
		{name: "integer address", address: int64(10), ranges: "10.0.0.0/8"},
		{name: "blob address", address: []byte("10.0.0.1"), ranges: "10.0.0.0/8"},
		{name: "null address", address: nil, ranges: "10.0.0.0/8"},
		{name: "integer range", address: "10.0.0.1", ranges: int64(10)},
		{name: "blob range", address: "10.0.0.1", ranges: []byte("10.0.0.0/8")},
		{name: "null range", address: "10.0.0.1", ranges: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := querySQLiteFunction(t, store.DB(), "SELECT kql_ipv4_is_in_range(?, ?)", test.address, test.ranges)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("kql_ipv4_is_in_range() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestCollectionAggregatesAreBounded(t *testing.T) {
	store, err := Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var encoded string
	err = store.DB().QueryRow(`WITH RECURSIVE values_(value) AS (
		SELECT 1 UNION ALL SELECT value + 1 FROM values_ WHERE value < 1200
	) SELECT kql_make_list(value, 0) FROM values_`).Scan(&encoded)
	if err != nil {
		t.Fatal(err)
	}
	var values []any
	if err := json.Unmarshal([]byte(encoded), &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != maxCollectionValues {
		t.Fatalf("values = %d, want %d", len(values), maxCollectionValues)
	}
}
