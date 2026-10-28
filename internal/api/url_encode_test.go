package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestURLEncodeThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(text:string);print form=url_encode(text),component=url_encode_component(text),roundtrip=url_decode(url_encode_component(text))`,
		"parameters": map[string]any{"text": "a+b é😀"},
	})
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("query: %d %s %v", response.StatusCode, body, err)
	}
	var result struct{ Rows []map[string]string }
	if err := json.Unmarshal(body, &result); err != nil || len(result.Rows) != 1 {
		t.Fatalf("response: %s %v", body, err)
	}
	for key, want := range map[string]string{"form": "a%2bb+%c3%a9%f0%9f%98%80", "component": "a%2bb%20%c3%a9%f0%9f%98%80", "roundtrip": "a+b é😀"} {
		if got := result.Rows[0][key]; got != want {
			t.Fatalf("%s got %s want %s", key, got, want)
		}
	}
}
