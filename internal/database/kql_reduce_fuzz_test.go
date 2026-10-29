package database

import (
	"strconv"
	"strings"
	"testing"
)

// The reduce aggregate, parse_version, gettype_at, parse_user_agent and
// punycode must not panic on arbitrary text, and punycode decoding inverts
// encoding.
func FuzzForensicUDFs(f *testing.F) {
	for _, seed := range []string{"fooZ1", "a -on- b", "1.2.3.4", `{"a":[1,"x"]}`, "", "Z", "\xff\xfe", "académie-française", "艺术", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"} {
		f.Add(seed, "Z")
	}
	f.Fuzz(func(t *testing.T, text, characters string) {
		for _, source := range []bool{false, true} {
			aggregate := &reduceAggregate{source: source}
			for _, value := range strings.Split(text, "\n") {
				if err := aggregate.Step(value, 0.3, characters); err != nil {
					return
				}
			}
			if _, err := aggregate.Done(); err != nil {
				t.Fatal(err)
			}
		}
		kqlParseVersion(text)
		kqlGetType(text)
		kqlGetTypeAt(text, "a", int64(-1), characters)
		if _, err := kqlParseUserAgent(text, `["browser","os","device"]`); err != nil {
			t.Fatalf("parse_user_agent(%q): %v", text, err)
		}
		kqlPunycodeDomainFromString(text)
		kqlPunycodeDomainToString(text)
		if encoded, ok := punycodeEncode(text); ok {
			if decoded, ok := punycodeDecode(encoded); !ok || decoded != text {
				t.Fatalf("punycode round trip of %q: %q -> %q", text, encoded, decoded)
			}
		}
	})
}

// Distinct values whose terms exceed the frequency table cap fail instead
// of growing memory without bound.
func TestReduceTermCap(t *testing.T) {
	aggregate := &reduceAggregate{}
	value := strings.Repeat("a ", 200)
	for i := 0; i < 21000; i++ {
		if err := aggregate.Step(value+strconv.Itoa(i), 0.1, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := aggregate.Done(); err == nil || !strings.Contains(err.Error(), "terms") {
		t.Fatalf("want the term cap, got %v", err)
	}
}
