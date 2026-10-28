package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestUnicodeCodepointExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=unicode_codepoints_from_string("⒦⒰⒮⒯⒪")`, `[[[9382,9392,9390,9391,9386]]]`},
		{`print a=unicode_codepoints_from_string("קוסטו - Kusto")`, `[[[1511,1493,1505,1496,1493,32,45,32,75,117,115,116,111]]]`},
		{`print s=unicode_codepoints_to_string(75,117,115,116,111)`, `[[Kusto]]`},
		{`print s=unicode_codepoints_to_string(dynamic([75,117,115,116,111]))`, `[[Kusto]]`},
		{`print s=unicode_codepoints_to_string(dynamic([75,117,115]),116,111)`, `[[Kusto]]`},
		{`print s=unicode_codepoints_to_string(75,10,117,10,115,10,116,10,111)`, `[[K
u
s
t
o]]`},
		{`print s=make_string(128512),a=to_utf8("😀"),empty=make_string(dynamic([])),bad=make_string(55296)`, `[[😀 [128512]  <nil>]]`},
		{`datatable(s:string)["Kusto","😀","é",""]|project s=unicode_codepoints_to_string(unicode_codepoints_from_string(s))`, `[[Kusto] [😀] [é] []]`},
		{`print a=to_utf8("😀")|mv-expand a|project n=tolong(a)`, `[[128512]]`},
		{`print a=to_utf8("Kusto"),s=make_string(75)|getschema|project ColumnType`, `[[dynamic] [string]]`},
		{`let f=(n:long){make_string(n)};print s=f(75)`, `[[K]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	if got := fmt.Sprint(featureRows(t, db, "print s=make_string("+strings.Repeat("75,", 63)+"75)")); got != "[["+strings.Repeat("K", 64)+"]]" {
		t.Fatal("64 arguments failed")
	}
	for _, call := range []string{`make_string()`, `make_string("75")`, `make_string(75.0)`, `make_string(true)`, `to_utf8(75)`, `to_utf8("a","b")`, "make_string(" + strings.Repeat("75,", 64) + "75)"} {
		if _, err := Compile("print x="+call, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", call)
		}
	}
}
