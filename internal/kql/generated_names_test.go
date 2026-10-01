package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestGeneratedProjectionAndExtendNames(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print x=1 | extend x+1 | project Column1`, `[[2]]`},
		{`print Column1=5 | extend 10 | project Column1,Column2`, `[[5 10]]`},
		{`let Column1=99; print x=1 | extend 2 | project Column2`, `[[2]]`},
		{`print x=1 | project named=x,x+1,x+2 | project named,Column1,Column2`, `[[1 2 3]]`},
		{`print x=1 | extend parse_json("{bad}") | project Column1`, `[["{bad}"]]`},
		{`Events | where n == 1 | extend array_reverse(items) | project Column1`, `[[[2,1]]]`},
		{`print x=1 | extend 3 | project 4 | project Column2`, `[[4]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print x=1 | extend parse_json('{"a":1}') | project Column1`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["Column1"]; !ok {
		t.Fatal("generated dynamic column metadata lost")
	}
}
