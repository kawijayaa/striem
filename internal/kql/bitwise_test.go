package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestBitwiseExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=binary_and(6,3),o=binary_or(5,3),x=binary_xor(1,2),z=binary_xor(1,1),n=binary_not(100),l=binary_shift_left(1,2),r=binary_shift_right(1,2),c=bitset_count_ones(42)`, `[[2 7 3 0 -101 4 0 3]]`},
		{`print a=binary_not(long(null)),b=binary_and(1,long(null)),c=binary_shift_left(1,-1),d=bitset_count_ones(long(null))`, `[[<nil> <nil> <nil> <nil>]]`},
		{`print a=binary_shift_left(1,63),b=binary_shift_left(1,64),c=binary_shift_left(1,65),d=binary_shift_right(-1,63),e=bitset_count_ones(-1)`, `[[-9223372036854775808 1 2 -1 64]]`},
		{`datatable(v:long)[1,2,3,4]|where binary_and(v,1)==1|summarize n=count()`, `[[2]]`},
		{`let f=(n:long){binary_or(n,2)};print v=f(1)`, `[[3]]`},
		{`print a=binary_and(1,2),c=bitset_count_ones(1)|getschema|project ColumnType`, `[[long] [long]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, call := range []string{`binary_and(1)`, `binary_or(1,2,3)`, `binary_xor(true,1)`, `binary_not(1.0)`, `binary_shift_left(1,"2")`, `binary_shift_right(dynamic(1),2)`, `bitset_count_ones()`} {
		if _, err := Compile("print x="+call, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", call)
		}
	}
}
