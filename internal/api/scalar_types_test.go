package api

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestScalarCommonTypesThroughAPI(t *testing.T) {
	server := serveStore(t, testStore(t))
	rows := queryRows(t, server.URL, `print a=iff(false,1,true),b=case(true,"text",2),c=iff(true,long(null),"x"),d=iff(true,1,2.0)/2,s=coalesce("","fallback"),missing=not(bool(null))`)
	if len(rows) != 1 {
		t.Fatal(rows)
	}
	row := rows[0]
	if row["a"] != true || row["b"] != "text" || row["c"] != nil || row["d"] != 0.5 || row["s"] != "fallback" || row["missing"] != nil {
		t.Fatalf("typed output=%#v", row)
	}
	rows = queryRows(t, server.URL, `print x=iff(true,"a\u0000b",1),large=iff(true,9223372036854775807,"x")`)
	if len(rows) != 1 || rows[0]["x"] != "a\x00b" {
		t.Fatalf("string output=%#v", rows)
	}

	// Integers beyond JavaScript's safe range are sent as exact strings.
	response := queryAPI(t, server.URL, `print large=iff(true,9223372036854775807,"x"),small=iff(true,-9223372036854775808,"x")`)
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || !strings.Contains(string(body), `"large":"9223372036854775807"`) || !strings.Contains(string(body), `"small":"-9223372036854775808"`) {
		t.Fatalf("lost integer precision: %s", body)
	}
	for _, q := range []string{`print iff(1,2,3)`, `print -"abc"`, `print true and 1`, `print coalesce(1,"x")`} {
		response := queryAPI(t, server.URL, q)
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "position") {
			t.Fatalf("%s status=%d body=%s", q, response.StatusCode, body)
		}
	}
}
