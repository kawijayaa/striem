package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestRepeat(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`Events | where n == 1 | extend r=repeat(1,3) | project r`, `[[[1,1,1]]]`},
		{`print a=repeat(1,0),b=repeat(1,-1)`, `[[[] <nil>]]`},
		{`print a=repeat(true,2),b=repeat(false,2)`, `[[[true,true] [false,false]]]`},
		{`print a=repeat(9223372036854775807,2),b=repeat(0.5,2)`, `[[[9223372036854775807,9223372036854775807] [0.5,0.5]]]`},
		{`print a=repeat("é\u0000",2),b=repeat("[1]",2)`, `[[["é\u0000","é\u0000"] ["[1]","[1]"]]]`},
		{`print a=repeat(null,2)`, `[[[null,null]]]`},
		{`print a=repeat(dynamic([1,true,null]),2)`, `[[[[1,true,null],[1,true,null]]]]`},
		{`print a=repeat(bag_pack("long",9223372036854775807),2)`, `[[[{"long":9223372036854775807},{"long":9223372036854775807}]]]`},
		{`let value="hello"; let count=2; print a=repeat(value,count)[1]`, `[[hello]]`},
		{`Events | where n == 2 | project a=repeat(user,n) | mv-expand a | summarize count()`, `[[2]]`},
		{`print a=repeat(1,65536) | project n=array_length(a)`, `[[65536]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	compiled, err := Compile(`print a=repeat(true,2) | project renamed=a`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["renamed"]; !ok {
		t.Fatal("dynamic metadata lost")
	}
	for _, expr := range []string{`repeat()`, `repeat(1)`, `repeat(1,true)`, `repeat(1,1.5)`, `repeat(1,"2")`, `repeat(1,2,3)`} {
		if _, err := Compile(`print a=`+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}
