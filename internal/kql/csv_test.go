package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestParseCSVExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print result=parse_csv('aa,"b,b,b",cc,"Escaping quotes: ""Title""","line1\nline2"')`, `[[["aa","b,b,b","cc","Escaping quotes: \"Title\"","line1\nline2"]]]`},
		{`print result=parse_csv('record1,a,b,c\nrecord2,x,y,z')`, `[[["record1","a","b","c"]]]`},
		{`datatable(session:string,participants:string)["a","a,b,c,d","b","a,b,c"] | where array_length(parse_csv(participants)) > 3 | project session`, `[[a]]`},
		{`print raw='a,"b,c",d' | project parse_csv(raw) | project middle=raw[1],length=array_length(raw)`, `[[b,c 3]]`},
		{`let raw='a,b'; print root=1 | project parse_csv(raw) | project first=raw[0]`, `[[a]]`},
		{`print a=parse_csv("1,true,null") | project first=tostring(a[0]),second=tostring(a[1]),third=tostring(a[2])`, `[[1 true null]]`},
		{`print d=parse_csv('a,"unfinished')`, `[[<nil>]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print raw="a,b" | project renamed=parse_csv(raw)`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["renamed"]; !ok {
		t.Fatal("lost dynamic metadata")
	}
	for _, input := range []string{``, `1`, `true`, `dynamic([])`, `"a","b"`} {
		if _, err := Compile(`print d=parse_csv(`+input+`)`, time.Now()); err == nil {
			t.Errorf("accepted invalid parse_csv(%s)", input)
		}
	}
}
