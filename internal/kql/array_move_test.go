package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestArrayRotationAndShift(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ expr, want string }{
		{`array_rotate_left(dynamic([1,2,3,4,5]),2)`, `[3,4,5,1,2]`},
		{`array_rotate_left(dynamic([1,2,3,4,5]),-2)`, `[4,5,1,2,3]`},
		{`array_rotate_right(dynamic([1,2,3,4,5]),2)`, `[4,5,1,2,3]`},
		{`array_rotate_right(dynamic([1,2,3,4,5]),-2)`, `[3,4,5,1,2]`},
		{`array_shift_left(dynamic([1,2,3,4,5]),2)`, `[3,4,5,null,null]`},
		{`array_shift_left(dynamic([1,2,3,4,5]),2,-1)`, `[3,4,5,-1,-1]`},
		{`array_shift_left(dynamic([1,2,3,4,5]),-2,-1)`, `[-1,-1,1,2,3]`},
		{`array_shift_right(dynamic([1,2,3,4,5]),2)`, `[null,null,1,2,3]`},
		{`array_shift_right(dynamic([1,2,3,4,5]),2,-1)`, `[-1,-1,1,2,3]`},
		{`array_shift_right(dynamic([1,2,3,4,5]),-2,-1)`, `[3,4,5,-1,-1]`},
		{`array_rotate_left(dynamic([1,2,3]),4)`, `[2,3,1]`},
		{`array_rotate_right(dynamic([1,2,3]),-4)`, `[2,3,1]`},
		{`array_rotate_left(dynamic([1,2,3]),0)`, `[1,2,3]`},
		{`array_shift_left(dynamic([1,2,3]),0)`, `[1,2,3]`},
		{`array_shift_right(dynamic([1,2,3]),3)`, `[null,null,null]`},
		{`array_shift_left(dynamic([1,2,3]),-10,true)`, `[true,true,true]`},
		{`array_shift_right(dynamic(["a","b"]),1,"")`, `["","a"]`},
		{`array_shift_left(dynamic(["a","b"]),1,null)`, `["b",null]`},
		{`array_shift_left(dynamic(["a","b"]),0)`, `["a","b"]`},
		{`array_rotate_right(dynamic([9223372036854775807,null,true,{"x":1},[2,3],"é\u0000"]),1)`, `["é\u0000",9223372036854775807,null,true,{"x":1},[2,3]]`},
		{`array_shift_left(dynamic([1,2]),1,bag_pack("x",9223372036854775807))`, `[2,{"x":9223372036854775807}]`},
		{`array_shift_right(dynamic([1,2]),1,"{\"x\":1}")`, `["{\"x\":1}",1]`},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, `print a=`+tc.expr)); got != "[["+tc.want+"]]" {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	for _, name := range []string{"array_rotate_left", "array_rotate_right", "array_shift_left", "array_shift_right"} {
		if got := fmt.Sprint(featureRows(t, db, `print a=`+name+`(dynamic([]),1)`)); got != `[[[]]]` {
			t.Fatal(got)
		}
		for _, args := range []string{`dynamic([])`, `"[]",1`, `dynamic([]),true`, `dynamic([]),1.5`, `dynamic([]),1,2,3`} {
			if _, err := Compile(`print a=`+name+`(`+args+`)`, time.Now()); err == nil {
				t.Errorf("accepted %s(%s)", name, args)
			}
		}
		compiled, err := Compile(`print `+name+`(dynamic([1]),1) | project renamed=print_0`, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := compiled.DynamicColumns["renamed"]; !ok {
			t.Fatal("dynamic metadata lost")
		}
	}
	if _, err := Compile(`print a=array_rotate_left(dynamic([]),1,2)`, time.Now()); err == nil {
		t.Fatal("accepted rotation padding")
	}
	for _, tc := range []struct{ query, want string }{
		{`let offset=-1; Events | where n == 1 | project a=array_rotate_right(items,offset) | mv-expand a | summarize total=sum(tolong(a))`, `[[3]]`},
		{`Events | where n == 1 | project a=array_shift_right(items,n,1) | mv-expand a | summarize total=sum(tolong(a))`, `[[2]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}
