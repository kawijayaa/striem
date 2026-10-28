package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestBitwiseAggregateExecution(t *testing.T) {
	// Decimal equivalents isolate aggregate behavior from the companion compiler’s
	// current hexadecimal-literal interval-lowering defect.
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(num:long)[4294967295,4294963215,3489660925,4211015679]|summarize result=toupper(tohex(binary_all_and(num)))`, `[[CAFEF00D]]`},
		{`datatable(num:long)[2290647048,1107296256,7761920,5]|summarize result=toupper(tohex(binary_all_or(num)))`, `[[CAFEF00D]]`},
		{`datatable(num:long)[1145062464,505290270,2427173385,725082]|summarize result=toupper(tohex(binary_all_xor(num)))`, `[[CAFEF00D]]`},
		{`datatable(g:string,v:long)["a",7,"a",3,"a",long(null),"b",4,"b",4]|summarize a=binary_all_and(v),o=binary_all_or(v),x=binary_all_xor(v) by g|sort by g asc`, `[[a 3 7 4] [b 4 4 0]]`},
		{`datatable(v:long)[]|summarize a=binary_all_and(v),o=binary_all_or(v),x=binary_all_xor(v)`, `[[<nil> <nil> <nil>]]`},
		{`datatable(v:long)[long(null)]|summarize a=binary_all_and(v),o=binary_all_or(v),x=binary_all_xor(v)`, `[[<nil> <nil> <nil>]]`},
		{`datatable(v:long)[]|summarize a=binary_all_and(v) by v`, `[]`},
		{`print v=1|summarize a=binary_all_and(v)|getschema|project ColumnType`, `[[long]]`},
		{`print a=tohex(256),b=tohex(-256),c=tohex(toint(-256),8),d=tohex(256,8),e=tohex(256,2)`, `[[100 ffffffffffffff00 ffffff00 00000100 100]]`},
		{`print a=tohex(1,100),n=tohex(long(null)),w=tohex(1,long(null)),z=tohex(1,0)`, `[[0000000000000001 <nil> <nil> <nil>]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print a=binary_all_and(1)`, `print v=1|where binary_all_or(v)>0`, `print v=1|summarize a=binary_all_xor(sum(v))`, `print v=1|summarize a=binary_all_and("x")`, `print a=tohex(1.0)`, `print a=tohex(1,2,3)`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid aggregate/signature accepted: %s", query)
		}
	}
}
