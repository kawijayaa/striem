package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestArraySplit(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print arr=dynamic([1,2,3,4,5]) | extend arr_split=array_split(arr,2)`, `[[[1,2,3,4,5] [[1,2],[3,4,5]]]]`},
		{`print arr=dynamic([1,2,3,4,5]) | extend arr_split=array_split(arr,dynamic([1,3]))`, `[[[1,2,3,4,5] [[1],[2,3],[4,5]]]]`},
		{`print a=array_split(dynamic([1,2,3]),-1)`, `[[[[1,2],[3]]]]`},
		{`print a=array_split(dynamic([1,2,3]),dynamic([-3,-1]))`, `[[[[],[1,2],[3]]]]`},
		{`print a=array_split(dynamic([1,2,3]),0)`, `[[[[],[1,2,3]]]]`},
		{`print a=array_split(dynamic([1,2,3]),3)`, `[[[[1,2,3],[]]]]`},
		{`print a=array_split(dynamic([1,2,3]),dynamic([0,1,1,3]))`, `[[[[],[1],[],[2,3],[]]]]`},
		{`print a=array_split(dynamic([1,2,3]),dynamic([]))`, `[[[[1,2,3]]]]`},
		{`print a=array_split(dynamic([]),0)`, `[[[[],[]]]]`},
		{`print a=array_split(dynamic([]),dynamic([]))`, `[[[[]]]]`},
		{`print a=array_split(dynamic([true,null,9223372036854775807,{"x":[1]},"é\u0000"]),2)`, `[[[[true,null],[9223372036854775807,{"x":[1]},"é\u0000"]]]]`},
		{`let points=dynamic([1,2]); print a=array_split(pack_array(1,2,3),points) | project v=a[1][0]`, `[[2]]`},
		{`Events | where n == 1 | project a=array_split(items,n) | mv-expand a | project n=array_length(a)`, `[[1] [1]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	compiled, err := Compile(`print array_split(dynamic([]),0) | project renamed=print_0`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["renamed"]; !ok {
		t.Fatal("dynamic metadata lost")
	}
	for _, expr := range []string{`array_split()`, `array_split(dynamic([]))`, `array_split("[]",1)`, `array_split(dynamic([]),true)`, `array_split(dynamic([]),1.5)`, `array_split(dynamic([]),"1")`, `array_split(dynamic([]),0,1)`} {
		if _, err := Compile(`print a=`+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}
