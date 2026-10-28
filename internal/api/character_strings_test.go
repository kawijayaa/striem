package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestCharacterStringsThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(text:string,date:datetime);print translated=translate("😀é","x☃",text),reversed=reverse(text),dated=reverse(date),empty=reverse(""),deleted=translate("😀é","",text)`,
		"parameters": map[string]any{"text": "😀é\x00", "date": "2017-10-15T14:00:00+02:00"},
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
	for key, want := range map[string]string{"translated": "x☃\x00", "reversed": "\x00é😀", "dated": "Z0000000.00:00:21T51-01-7102", "empty": "", "deleted": "\x00"} {
		if got := result.Rows[0][key]; got != want {
			t.Fatalf("%s got %q want %q", key, got, want)
		}
	}
}
