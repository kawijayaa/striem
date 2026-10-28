package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestCountOfExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=countof("aaa","a"),b=countof("aaaa","aa"),c=countof("ababa","ab","normal"),d=countof("ababa","aba"),e=countof("ababa","aba","regex"),f=countof("abcabc","a.c","regex")`, `[[3 3 2 2 1 2]]`},
		{`datatable(s:string)["aaaa","aba","bbb"]|where countof(s,"a")>0|project n=countof(s,"a")`, `[[4] [2]]`},
		{`print a=countof("😀😀😀","😀😀"),b=countof("éé","é"),c=countof("abc","^","regex"),d=countof("abc","$","regex")`, `[[2 2 1 1]]`},
		{`let f=(s:string){countof(s,"aa")};print n=f("aaaa")`, `[[3]]`},
		{`print n=countof("aaa","a")|getschema|project ColumnType`, `[[long]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, call := range []string{`countof()`, `countof("a")`, `countof(1,"a")`, `countof("a",dynamic("a"))`, `countof("a","b",true)`, `countof("a","b","normal",1)`} {
		if _, err := Compile("print n="+call, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", call)
		}
	}
}
