package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestBase64Execution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print b=base64_encode_tostring("Kusto"),a=base64_decode_toarray("S3VzdG8="),r=base64_encode_fromarray(base64_decode_toarray("S3VzdG8="))`, `[[S3VzdG8= [75,117,115,116,111] S3VzdG8=]]`},
		{`print b=base64_encodestring("Kusto"),s=base64_decodestring("S3VzdG8=")`, `[[S3VzdG8= Kusto]]`},
		{`print b=base64_encode_fromarray(dynamic([0,255,128])),a=base64_decode_toarray("AP+A"),s=base64_decode_tostring("AP+A")`, `[[AP+A [0,255,128] <nil>]]`},
		{`print b=base64_decode_toarray("")|project n=array_length(b)`, `[[0]]`},
		{`datatable(s:string)["Kusto","é","😀",""]|project s=base64_decode_tostring(base64_encode_tostring(s))`, `[[Kusto] [é] [😀] []]`},
		{`print b=base64_encode_fromarray(dynamic([256])),n=base64_decode_toarray("bad")`, `[[<nil> <nil>]]`},
		{`print a=base64_decode_toarray("S3VzdG8=")|mv-expand a|summarize s=sum(tolong(a))`, `[[534]]`},
		{`print a=base64_decode_toarray("S3VzdG8="),s=base64_encode_tostring("Kusto")|getschema|project ColumnType`, `[[dynamic] [string]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, call := range []string{`base64_encode_tostring()`, `base64_encode_tostring(1)`, `base64_decode_toarray(false)`, `base64_encode_fromarray("[]")`, `base64_encode_fromarray(dynamic([]),1)`} {
		if _, err := Compile("print x="+call, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", call)
		}
	}
}
