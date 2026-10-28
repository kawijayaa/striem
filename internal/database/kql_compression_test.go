package database

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

func compressedFixture(t *testing.T, data []byte, gz bool) string {
	t.Helper()
	var out bytes.Buffer
	var writer io.WriteCloser
	if gz {
		writer = gzip.NewWriter(&out)
	} else {
		writer = zlib.NewWriter(&out)
	}
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(out.Bytes())
}

func TestCompressionDocumentedExamples(t *testing.T) {
	for _, tc := range []struct {
		gz             bool
		plain, encoded string
	}{
		{true, "1234567890qwertyuiop", "H4sIAAAAAAAA/wEUAOv/MTIzNDU2Nzg5MHF3ZXJ0eXVpb3A6m7f2FAAAAA=="},
		{false, "1234567890qwertyuiop", "eAEBFADr/zEyMzQ1Njc4OTBxd2VydHl1aW9wOAkGdw=="},
	} {
		got, err := compressKQLText(tc.plain, tc.gz)
		if err != nil || got != tc.encoded {
			t.Fatalf("documented compression: %v %v", got, err)
		}
		got, err = decompressKQLText(tc.encoded, tc.gz)
		if err != nil || got != tc.plain {
			t.Fatalf("documented decompression: %v %v", got, err)
		}
	}
	if got, err := kqlZlibDecompress("eJwLSS0uUSguKcrMS1cwNDIGACxqBQ4="); err != nil || got != "Test string 123" {
		t.Fatalf("zlib example: %v %v", got, err)
	}
}

func TestCompressionValidationAndLimits(t *testing.T) {
	for _, gz := range []bool{false, true} {
		for _, plain := range []string{"", "hello\x00é😀", strings.Repeat("a", 65535), strings.Repeat("b", 65536), strings.Repeat("x", maxCompressionTextBytes)} {
			encoded, err := compressKQLText(plain, gz)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decompressKQLText(encoded, gz)
			if err != nil || decoded != plain {
				t.Fatalf("roundtrip %d: %v", len(plain), err)
			}
			// Verify output with the standard decoder independently of the adapter.
			data, _ := base64.StdEncoding.DecodeString(encoded.(string))
			var reader io.ReadCloser
			if gz {
				reader, err = gzip.NewReader(bytes.NewReader(data))
			} else {
				reader, err = zlib.NewReader(bytes.NewReader(data))
			}
			if err != nil {
				t.Fatal(err)
			}
			output, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || string(output) != plain {
				t.Fatal("external decoder disagrees")
			}
		}
		fixture := compressedFixture(t, []byte("hello"), gz)
		wire, _ := base64.StdEncoding.DecodeString(fixture)
		var invalid []any
		invalid = append(invalid, nil, 1, "x0x0x0", fixture+"\n", compressedFixture(t, []byte{255}, gz))
		for n := 0; n < len(wire); n++ {
			invalid = append(invalid, base64.StdEncoding.EncodeToString(wire[:n]))
		}
		for _, offset := range []int{0, 1, 2, len(wire) - 1, len(wire) - 5} {
			copyWire := bytes.Clone(wire)
			copyWire[offset] ^= 0xff
			invalid = append(invalid, base64.StdEncoding.EncodeToString(copyWire))
		}
		invalid = append(invalid, base64.StdEncoding.EncodeToString(append(bytes.Clone(wire), wire...)), base64.StdEncoding.EncodeToString(append(bytes.Clone(wire), 0)))
		if gz {
			for _, flag := range []byte{1, 2, 4, 8, 16, 32, 128} {
				copyWire := bytes.Clone(wire)
				copyWire[3] = flag
				invalid = append(invalid, base64.StdEncoding.EncodeToString(copyWire))
			}
		}
		for _, value := range invalid {
			if got, err := decompressKQLText(value, gz); err != nil || got != nil {
				t.Fatalf("malformed stream accepted: gz=%v %v %v", gz, got, err)
			}
		}
		if got, err := decompressKQLText(compressedFixture(t, []byte("ordinary compressed input"), gz), gz); err != nil || got != "ordinary compressed input" {
			t.Fatal("compressed blocks rejected")
		}
		if got, err := decompressKQLText(compressedFixture(t, bytes.Repeat([]byte{'a'}, maxCompressionTextBytes+1), gz), gz); err == nil || got != nil {
			t.Fatal("decompression output limit missing")
		}
		if got, err := compressKQLText(strings.Repeat("a", maxCompressionTextBytes+1), gz); err == nil || got != nil {
			t.Fatal("encoder limit missing")
		}
		if got, err := decompressKQLText(strings.Repeat("A", base64.StdEncoding.EncodedLen(maxCompressionWireBytes)+4), gz); err == nil || got != nil {
			t.Fatal("wire limit missing")
		}
	}
}

func FuzzCompressionStreams(f *testing.F) {
	f.Add([]byte("hello\x00😀"))
	f.Add([]byte{0, 255})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 65536 {
			return
		}
		for _, gz := range []bool{false, true} {
			got, err := compressKQLText(string(data), gz)
			if err != nil {
				t.Fatal(err)
			}
			if utf8.Valid(data) {
				decoded, err := decompressKQLText(got, gz)
				if err != nil || decoded != string(data) {
					t.Fatal("roundtrip failed")
				}
			} else if got != nil {
				t.Fatal("invalid UTF-8 encoded")
			}
			result, err := decompressKQLText(base64.StdEncoding.EncodeToString(data), gz)
			if err == nil && result != nil {
				if text, ok := result.(string); !ok || !utf8.ValidString(text) || len(text) > maxCompressionTextBytes {
					t.Fatal("invalid decoded result")
				}
			}
		}
	})
}
