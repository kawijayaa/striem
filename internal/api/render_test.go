package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestRenderAnnotationThroughAPI(t *testing.T) {
	handler := New(testStore(t), slog.New(slog.NewTextHandler(io.Discard, nil))).Handler()
	for _, tc := range []struct{ query, visualization string }{
		{`range n from 1 to 3 step 1 | extend s="a" | render columnchart with (kind=stacked, series=s, title="Counts")`, `{"type":"columnchart","properties":{"kind":"stacked","series":["s"],"title":"Counts"}}`},
		{`range n from 1 to 3 step 1`, ``},
	} {
		response := localParameterPost(t, handler, "/api/query", map[string]any{"query": tc.query})
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != 200 {
			t.Fatalf("%s: %d %s %v", tc.query, response.StatusCode, body, err)
		}
		var result struct {
			RowCount      int
			Visualization json.RawMessage
		}
		if err := json.Unmarshal(body, &result); err != nil || result.RowCount != 3 || string(result.Visualization) != tc.visualization {
			t.Fatalf("%s: response %s %v", tc.query, body, err)
		}
	}
	response := localParameterPost(t, handler, "/api/query", map[string]any{"query": `range n from 1 to 3 step 1 | render timechart | take 1`})
	response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatalf("misplaced render status = %d", response.StatusCode)
	}
}
