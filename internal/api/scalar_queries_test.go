package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestToScalarTypedResultsAndDiagnosticsThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `let n=toscalar(range x from 1 to 9 step 1|count); print n=n,d=toscalar(print dynamic({"x":[1,true,null]})),b=toscalar(print not(false)),empty=toscalar(datatable(x:long)[])`)
	if len(rows) != 1 || rows[0]["n"] != float64(9) || rows[0]["b"] != true || rows[0]["empty"] != nil {
		t.Fatalf("rows=%#v", rows)
	}
	d, ok := rows[0]["d"].(map[string]any)
	if !ok {
		t.Fatalf("dynamic=%#v", rows)
	}
	x, ok := d["x"].([]any)
	if !ok || len(x) != 3 || x[0] != float64(1) || x[1] != true || x[2] != nil {
		t.Fatalf("dynamic=%#v", d)
	}
	for _, query := range []string{
		`print toscalar()`,
		`let f=(x:long){toscalar(x)}; datatable(x:long)[1]|project f(x)`,
		`let f=(x:long){let t=datatable(n:long)[1]|extend n=x;toscalar(t|count)};datatable(x:long)[1]|project f(x)`,
	} {
		response := queryAPI(t, server.URL, query)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "position") {
			t.Fatalf("%s status=%d body=%s", query, response.StatusCode, body)
		}
	}
}
