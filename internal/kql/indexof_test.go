package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestIndexOfExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print indexof("abcdefg","cde"),indexof("abcdefg","cde",1,4),indexof("abcdefg","cde",1,2),indexof("abcdefg","cde",3,4),indexof("abcdefg","cde",-5),indexof(1234567,5,1,4),indexof("abcdefg","cde",2,-1),indexof("abcdefgabcdefg","cde",1,10,2),indexof("abcdefgabcdefg","cde",1,-1,3)`, `[[2 2 -1 -1 2 4 2 9 -1]]`},
		{`print indexof("a😀é中😀","😀",2),indexof("a😀é中😀","😀",0,-1,2),indexof("a😀é中😀","é中",2,2)`, `[[4 4 2]]`},
		{`print indexof('a\u0000b','\u0000'),indexof('a\u0000b','b'),indexof("éABC","abc")`, `[[1 2 -1]]`},
		{`print indexof("abc","a",-4),indexof("abc","a",0,-2),indexof("abc","a",0,-1,-1)`, `[[<nil> <nil> <nil>]]`},
		{`print indexof("abc","a",9223372036854775807),indexof("abc","b",0,9223372036854775807),indexof("abc","a",-9223372036854775807),indexof("abc","a",0,-1,9223372036854775807)`, `[[-1 1 <nil> -1]]`},
		{`print indexof("abc","a",0,0),indexof("abc","bc",2,1),indexof("abc","bc",1,2)`, `[[-1 -1 1]]`},
		{`print indexof(true,"r"),indexof(9223372036854775807,"5807"),indexof(12.5,2.5)`, `[[1 15 1]]`},
		{`Events | where indexof(user,"o")>=0 | project user | order by user asc`, `[[bob] [carol]]`},
		{`let occurrence=2; print indexof("abcabc","a",0,-1,occurrence)`, `[[3]]`},
		{`print x=indexof("abc","a") | getschema | project DataType`, `[[System.Int64]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	for _, expr := range []string{`indexof("a")`, `indexof("a","b",0,1,1,2)`, `indexof("a","b",0.5)`, `indexof("a","b",0,true)`, `indexof("a","b",0,-1,n)`} {
		if _, err := Compile(`Events | project x=`+expr, time.Now(), featureCatalog()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}
