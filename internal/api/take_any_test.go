package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestTakeAnyTypedCoherentResultsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `datatable(n:long,d:dynamic,b:bool)[9,dynamic({"id":9}),true,1,dynamic({"id":1}),false]|summarize take_any(*),total=count()`)
	if len(rows) != 1 || rows[0]["total"] != float64(2) {
		t.Fatalf("rows=%#v", rows)
	}
	row := rows[0]
	d, ok := row["d"].(map[string]any)
	if !ok || d["id"] != row["n"] || row["n"] == float64(9) && row["b"] != true || row["n"] == float64(1) && row["b"] != false {
		t.Fatalf("incoherent row=%#v", row)
	}
	rows = queryRows(t, server.URL, `datatable(n:long,b:bool)[1,false,2,true]|summarize enabled=take_anyif(b,n==2),missing=take_anyif(n,false),total=count()`)
	if len(rows) != 1 || rows[0]["enabled"] != true || rows[0]["missing"] != nil || rows[0]["total"] != float64(2) {
		t.Fatalf("conditional=%#v", rows)
	}
	for _, q := range []string{`print take_any(1)`, `print n=1|summarize take_anyif(n,1)`, `print n=1|summarize (a,b)=take_any(n)`} {
		response := queryAPI(t, server.URL, q)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "position") {
			t.Fatalf("%s status=%d body=%s", q, response.StatusCode, body)
		}
	}
}
