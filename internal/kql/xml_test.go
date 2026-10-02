package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestParseXMLExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print d=parse_xml('<e name="value">text</e>') | project attr=d.e.["@name"],text=d.e.["#text"]`, `[[value text]]`},
		{`print d=parse_xml('<e><a>first</a><a>second</a></e>') | project first=d.e.a[0],last=d.e.a[-1],n=array_length(d.e.a)`, `[[first second 2]]`},
		{`datatable(raw:string)["<e>text</e>","<e/>","bad"] | extend d=parse_xml(raw) | project text=d.e`, `[[text] [<nil>] [<nil>]]`},
		{`let raw='<e><a>7</a></e>'; print root=1 | project parse_xml(raw) | project n=toint(raw.e.a)+1`, `[[8]]`},
		{`print raw='<e><a>7</a></e>' | project parse_xml(raw) | project n=toint(raw.e.a)`, `[[7]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print d=parse_xml("<e/>") | project renamed=d`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["renamed"]; !ok {
		t.Fatal("lost dynamic metadata")
	}
	for _, input := range []string{``, `1`, `true`, `dynamic({})`, `"a","b"`} {
		if _, err := Compile(`print parse_xml(`+input+`)`, time.Now()); err == nil {
			t.Errorf("accepted parse_xml(%s)", input)
		}
	}
}
