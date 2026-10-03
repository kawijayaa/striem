package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestArgMinMaxTypedResultsAndDiagnosticsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `datatable(n:long,d:dynamic,b:bool)[1,dynamic({"x":1}),false,2,dynamic([1,true,null]),true]|summarize arg_max(n,*)`)
	if len(rows) != 1 || rows[0]["n"] != float64(2) || rows[0]["b"] != true {
		t.Fatalf("rows=%#v", rows)
	}
	d, ok := rows[0]["d"].([]any)
	if !ok || len(d) != 3 || d[0] != float64(1) || d[1] != true || d[2] != nil {
		t.Fatalf("dynamic=%#v", rows)
	}
	rows = queryRows(t, server.URL, `datatable(n:long,s:string)[1,"first",2,"last"]|summarize (latest,lastLabel)=arg_max(n,s),(earliest,firstLabel)=arg_min(n,s),total=count()`)
	if len(rows) != 1 || rows[0]["latest"] != float64(2) || rows[0]["lastLabel"] != "last" || rows[0]["earliest"] != float64(1) || rows[0]["firstLabel"] != "first" || rows[0]["total"] != float64(2) {
		t.Fatalf("tuples=%#v", rows)
	}
	for _, query := range []string{`print arg_max(1)`, `print n=1|summarize (a,b)=arg_min(n)`, `print s="abc"|summarize arg_max(s+"!",*)`, `print n=1|summarize arg_max()`} {
		response := queryAPI(t, server.URL, query)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "position") {
			t.Fatalf("%s status=%d body=%s", query, response.StatusCode, body)
		}
	}
}
