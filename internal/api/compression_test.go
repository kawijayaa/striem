package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestCompressionThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(text:string);print gzip=gzip_compress_to_base64_string(text),zlib=zlib_compress_to_base64_string(text),roundtrip=gzip_decompress_from_base64_string(gzip_compress_to_base64_string(text)),empty=zlib_decompress_from_base64_string(zlib_compress_to_base64_string("")),invalid=gzip_decompress_from_base64_string("bad")`,
		"parameters": map[string]any{"text": "1234567890qwertyuiop"},
	})
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("query: %d %s %v", response.StatusCode, body, err)
	}
	var result struct{ Rows []map[string]json.RawMessage }
	if err := json.Unmarshal(body, &result); err != nil || len(result.Rows) != 1 {
		t.Fatalf("response: %s %v", body, err)
	}
	for key, want := range map[string]string{
		"gzip":      `"H4sIAAAAAAAA/wEUAOv/MTIzNDU2Nzg5MHF3ZXJ0eXVpb3A6m7f2FAAAAA=="`,
		"zlib":      `"eAEBFADr/zEyMzQ1Njc4OTBxd2VydHl1aW9wOAkGdw=="`,
		"roundtrip": `"1234567890qwertyuiop"`, "empty": `""`, "invalid": "null",
	} {
		if got := string(result.Rows[0][key]); got != want {
			t.Fatalf("%s got %s want %s", key, got, want)
		}
	}
}
