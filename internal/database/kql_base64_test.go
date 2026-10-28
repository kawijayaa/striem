package database

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBase64ByteConversions(t *testing.T) {
	for _, data := range [][]byte{{}, []byte("Kusto"), {0, 255, 128, 1}, bytes.Repeat([]byte{255}, maxDecodeInputBytes)} {
		encoded := base64.StdEncoding.EncodeToString(data)
		got := kqlBase64DecodeToArray(encoded)
		var numbers []int
		if got == nil || json.Unmarshal([]byte(got.(string)), &numbers) != nil {
			t.Fatalf("array decoding failed for %d bytes", len(data))
		}
		if len(numbers) != len(data) {
			t.Fatal("wrong array length")
		}
		for i, b := range data {
			if numbers[i] != int(b) {
				t.Fatal("byte corruption")
			}
		}
		result, err := kqlBase64EncodeFromArray(got)
		if err != nil || result != encoded {
			t.Fatalf("array round trip: %v %v", result, err)
		}
		if utf8.Valid(data) {
			if got := kqlBase64DecodeToString(encoded); got != string(data) {
				t.Fatal("text round trip failed")
			}
		} else if kqlBase64DecodeToString(encoded) != nil {
			t.Fatal("invalid UTF-8 accepted as text")
		}
	}
	for _, bad := range []any{nil, 1, "!", "Zg", "Zh==", "Zg===", "Zg==\n", " Zg==", "_w==", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, maxDecodeInputBytes+1))} {
		if kqlBase64DecodeToArray(bad) != nil || kqlBase64DecodeToString(bad) != nil {
			t.Fatalf("accepted invalid input %T", bad)
		}
	}
	for _, bad := range []any{nil, "null", "{}", `[null]`, `[true]`, `["1"]`, `[-1]`, `[256]`, `[1.0]`, `[1e0]`, `[[1]]`} {
		if got, err := kqlBase64EncodeFromArray(bad); err != nil || got != nil {
			t.Fatalf("invalid byte array: %v %v %v", bad, got, err)
		}
	}
	for _, input := range []any{nil, 1, string([]byte{255}), strings.Repeat("a", maxDecodeInputBytes+1)} {
		if kqlBase64EncodeToString(input) != nil {
			t.Fatal("invalid text accepted")
		}
	}
	if got, err := kqlBase64EncodeFromArray("[" + strings.Repeat("0,", 65536) + "0]"); err == nil || got != nil {
		t.Fatal("array budget not enforced")
	}
	store, err := Open(t.TempDir() + "/base64.db")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if got := querySQLiteFunction(t, store.DB(), "SELECT kql_base64_decode_toarray(?)", "S3VzdG8="); got != "[75,117,115,116,111]" {
		t.Fatalf("documented example: %v", got)
	}
}

func FuzzBase64Conversions(f *testing.F) {
	f.Add([]byte("Kusto"))
	f.Add([]byte{0, 255, 128})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > maxDecodeInputBytes {
			return
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		array := kqlBase64DecodeToArray(encoded)
		if array == nil {
			t.Fatal("valid base64 rejected")
		}
		got, err := kqlBase64EncodeFromArray(array)
		if err != nil || got != encoded {
			t.Fatalf("round trip: %v %v", got, err)
		}
		if utf8.Valid(data) {
			if kqlBase64EncodeToString(string(data)) != encoded || kqlBase64DecodeToString(encoded) != string(data) {
				t.Fatal("UTF-8 round trip")
			}
		}
		if got := kqlBase64DecodeToArray(string(data)); got != nil {
			if _, err := kqlBase64EncodeFromArray(got); err != nil {
				t.Fatal(fmt.Sprint(err))
			}
		}
	})
}
