package database

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

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
