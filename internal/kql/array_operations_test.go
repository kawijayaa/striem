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
	if !reflect.DeepEqual(compiled.Columns, []string{"print_0", "print_1", "named"}) {
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

func TestArraySlice(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print arr=dynamic([1,2,3]) | extend sliced=array_slice(arr,1,2)`, `[[[1,2,3] [2,3]]]`},
		{`print arr=dynamic([1,2,3,4,5]) | extend sliced=array_slice(arr,2,-1)`, `[[[1,2,3,4,5] [3,4,5]]]`},
		{`print arr=dynamic([1,2,3,4,5]) | extend sliced=array_slice(arr,-3,-2)`, `[[[1,2,3,4,5] [3,4]]]`},
		{`print a=array_slice(dynamic([1,2,3]),-10,10)`, `[[[1,2,3]]]`},
		{`print a=array_slice(dynamic([1,2,3]),0,0)`, `[[[1]]]`},
		{`print a=array_slice(dynamic([1,2,3]),-1,-1)`, `[[[3]]]`},
		{`print a=array_slice(dynamic([1,2,3]),2,1)`, `[[[]]]`},
		{`print a=array_slice(dynamic([1,2,3]),4,10)`, `[[[]]]`},
		{`print a=array_slice(dynamic([1,2,3]),0,-10)`, `[[[]]]`},
		{`print a=array_slice(dynamic([]),0,-1)`, `[[[]]]`},
		{`print a=array_slice(dynamic([null,true,{"long":9223372036854775807},[1,2],"é\u0000"]),1,-1)`, `[[[true,{"long":9223372036854775807},[1,2],"é\u0000"]]]`},
		{`print a=array_slice(dynamic([1,2]),tolong("-9223372036854775808"),9223372036854775807)`, `[[[1,2]]]`},
		{`let first=1; let last=-1; Events | where n == 1 | project a=array_slice(items,first,last) | mv-expand a | project n=tolong(a)`, `[[2]]`},
		{`print a=array_slice(array_reverse(pack_array(1,2,3)),0,1)[1]`, `[[2]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	compiled, err := Compile(`print array_slice(dynamic([]),0,1)`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(compiled.Columns, []string{"print_0"}) {
		t.Fatal(compiled.Columns)
	}
	if _, ok := compiled.DynamicColumns["print_0"]; !ok {
		t.Fatal("dynamic metadata lost")
	}
	for _, expr := range []string{`array_slice()`, `array_slice(dynamic([]),0)`, `array_slice("[]",0,1)`, `array_slice(dynamic([]),0.5,1)`, `array_slice(dynamic([]),0,true)`, `array_slice(dynamic([]),0,1,2)`} {
		if _, err := Compile(`print a=`+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}
