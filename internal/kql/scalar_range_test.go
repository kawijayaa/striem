package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestScalarRangeExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print r=range(1,8,3)`, `[[[1,4,7]]]`},
		{`print r=range(1,3)`, `[[[1,2,3]]]`},
		{`print r=range(5,1,-2),e=range(5,1),z=range(1,5,0)`, `[[[5,3,1] [] <nil>]]`},
		{`print r=range(0.5,2.0,0.5)`, `[[[0.5,1,1.5,2]]]`},
		{`datatable(n:long)[1,2,3]|project r=range(1,n)|mv-expand r|summarize s=sum(tolong(r))`, `[[10]]`},
		{`print s=unicode_codepoints_to_string(range(48,57),range(65,90),range(97,122))`, `[[0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz]]`},
		{`print r=range(long(9223372036854775806),long(9223372036854775807))`, `[[[9223372036854775806,9223372036854775807]]]`},
		{`print r=range(1,1000000000)|mv-expand r|count`, `[[1048576]]`},
		{`print r=range(1,3)|getschema|project ColumnType`, `[[dynamic]]`},
		{`let f=(n:long){iff(true,range(1,n),dynamic([]))};print r=f(3)`, `[[[1,2,3]]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, call := range []string{`range()`, `range(1)`, `range(1,2,3,4)`, `range("1",3)`, `range(true,3)`, `range(datetime(2020-01-01),datetime(2020-01-02))`} {
		if _, err := Compile("print r="+call, time.Now()); err == nil {
			t.Fatalf("unsupported signature accepted: %s", call)
		}
	}
}
