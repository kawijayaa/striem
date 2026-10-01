package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestSplitExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=split("aa_bb","_"),b=split("aaa_bbb_ccc","_",1),c=split("","_"),d=split("a__b","_"),e=split("aabbcc","bb")`, `[[["aa","bb"] ["bbb"] [""] ["a","","b"] ["aa","cc"]]]`},
		{`print a=split("_a_","_"),b=split("_a_","_",2),c=split("aaa","aa")`, `[[["","a",""] [""] ["","a"]]]`},
		{`print a=split("é😀中😀a","😀"),b=split('a\u0000b','\u0000',1)`, `[[["é","中","a"] ["b"]]]`},
		{`print a=split(12345,"3"),b=split(true,"r",1)`, `[[["12","45"] ["ue"]]]`},
		{`Events | where n==1 | project a=split(user,"i",n)`, `[[["ce"]]]`},
		{`print a=split("a,b",",")[1],b=array_length(split("a,b",","))`, `[[b 2]]`},
		{`print a=split("a,b",",") | mv-expand a to typeof(string) | order by a asc`, `[[a] [b]]`},
		{`print a=split("a,b",",") | getschema | project DataType`, `[[System.Object]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	for _, expr := range []string{`split("a")`, `split("a","b",1,2)`, `split("a",1)`, `split("a","b",0.5)`, `split("a","b",true)`} {
		if _, err := Compile(`print a=`+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
	compiled, err := Compile(`print a=split("a,b",",") | project result=a`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["result"]; !ok {
		t.Fatalf("missing dynamic metadata: %#v", compiled)
	}
}
