package kql

import (
	"fmt"
	"testing"
)

func TestParsingFunctionsWithSQLNull(t *testing.T) {
	db := featureDatabase(t)
	query := `datatable(id:long,raw:string)[1,"a"] | union (datatable(id:long)[2]) | where id == 2 | project json=parse_json(raw),csv=parse_csv(raw),xml=parse_xml(raw),url=parse_url(raw),params=parse_urlquery(raw)`
	if got := fmt.Sprint(featureRows(t, db, query)); got != "[[<nil> <nil> <nil> <nil> <nil>]]" {
		t.Fatal(got)
	}
}
