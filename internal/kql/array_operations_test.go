package kql

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestArrayOperations(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`range x from 1 to 3 step 1 | extend y=x*2 | extend z=y*2 | extend a1=pack_array(x,y,z),a2=pack_array(x,y) | project array_concat(a1,a2)`, `[[[1,2,4,1,2]] [[2,4,8,2,4]] [[3,6,12,3,6]]]`},
		{`print arr=dynamic(["this","is","an","example"]) | project Result=array_reverse(arr)`, `[[["example","an","is","this"]]]`},
		{`print a=array_concat(dynamic([]))`, `[[[]]]`},
		{`print a=array_reverse(dynamic([]))`, `[[[]]]`},
		{`print a=array_concat(dynamic([]),dynamic([1]),dynamic([]),dynamic([2,3]))`, `[[[1,2,3]]]`},
		{`print a=array_reverse(dynamic([1,null,true,"é\u0000",[2,3],{"long":9223372036854775807}]))`, `[[[{"long":9223372036854775807},[2,3],"é\u0000",true,null,1]]]`},
		{`print a=array_concat(dynamic([9223372036854775807,null]),pack_array(true,"[]",dynamic([1])))`, `[[[9223372036854775807,null,true,"[]",[1]]]]`},
		{`print a=array_reverse(array_reverse(dynamic([1,2])))`, `[[[1,2]]]`},
		{`print a=array_reverse(array_concat(dynamic([1]),dynamic([2,3]))) | mv-expand a | summarize total=sum(tolong(a))`, `[[6]]`},
		{`let a=dynamic([1,2]); print a=array_reverse(a)[0],b=array_length(array_concat(a,a))`, `[[2 4]]`},
		{`Events | where n == 1 | project a=array_reverse(items)`, `[[[2,1]]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	compiled, err := Compile(`print array_concat(dynamic([])),array_reverse(dynamic([])),named=array_reverse(dynamic([]))`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(compiled.Columns, []string{"Column1", "Column2", "named"}) {
		t.Fatal(compiled.Columns)
	}
	if len(compiled.DynamicColumns) != 3 {
		t.Fatal("dynamic metadata lost")
	}
	for _, expr := range []string{`array_concat()`, `array_concat("[]")`, `array_reverse()`, `array_reverse(dynamic([]),dynamic([]))`, `array_reverse(1)`, `array_concat(` + strings.Repeat(`dynamic([]),`, 64) + `dynamic([]))`} {
		if _, err := Compile(`print a=`+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
	query := `print a=array_concat(` + strings.TrimSuffix(strings.Repeat(`dynamic([1]),`, 64), ",") + `) | project n=array_length(a)`
	if got := fmt.Sprint(featureRows(t, db, query)); got != `[[64]]` {
		t.Fatal(got)
	}
}
