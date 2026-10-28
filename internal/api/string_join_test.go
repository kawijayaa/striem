package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestStringJoinsThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	response := localParameterPost(t, handler, "/api/query", map[string]any{
		"query":      `declare query_parameters(values:dynamic,delimiter:string,count:long,date:datetime);print joined=strcat_array(values,delimiter),repeated=strrep("😀",count,delimiter),empty=strcat_array(dynamic([]),delimiter),invalid=strrep("x",0),scalar=strcat_delim(delimiter,date,long(9223372036854775807))`,
		"parameters": map[string]any{"values": []any{json.Number("9223372036854775807"), "é", nil}, "delimiter": "\x00", "count": 2, "date": "2020-01-01T02:00:00+02:00"},
	})
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("query: %d %s %v", response.StatusCode, body, err)
	}
	var result struct{ Rows []map[string]*string }
	if err := json.Unmarshal(body, &result); err != nil || len(result.Rows) != 1 {
		t.Fatalf("response: %s %v", body, err)
	}
	for key, want := range map[string]string{"joined": "9223372036854775807\x00é\x00", "repeated": "😀\x00😀", "empty": "", "scalar": "2020-01-01T00:00:00.0000000Z\x009223372036854775807"} {
		if got := result.Rows[0][key]; got == nil || *got != want {
			t.Fatalf("%s got %v want %q", key, got, want)
		}
	}
	if result.Rows[0]["invalid"] != nil {
		t.Fatal("invalid count lost null")
	}
}
