package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestCharacterStringsExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=translate("abc","x","abc"),b=translate("abc","","ab"),c=translate("krasp","otsku","spark")`, `[[xxx  kusto]]`},
		{`print s="ABCDEFGHIJKLMNOPQRSTUVWXYZ"|project s=reverse(s)`, `[[ZYXWVUTSRQPONMLKJIHGFEDCBA]]`},
		{`print i=reverse(12345),r=reverse(123.45),d=reverse(datetime(2017-10-15T12:00:00Z)),b=reverse(true)`, `[[54321 54.321 Z0000000.00:00:21T51-01-7102 eurt]]`},
		{`datatable(s:string)["a😀é","","abc"]|project s=reverse(s)`, `[[é😀a] [] [cba]]`},
		{`print s=translate("😀é","x☃","😀é😀"),n=translate("","xy","abc")`, `[[x☃x abc]]`},
		{`let f=(s:string){reverse(translate("ab","xy",s))};print s=f("abc")`, `[[cyx]]`},
		{`print a=reverse("abc"),b=translate("a","b","a")|getschema|project ColumnType`, `[[string] [string]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, call := range []string{`translate("a","b")`, `translate(1,"b","a")`, `reverse()`, `reverse("a","b")`, `reverse(dynamic([]))`} {
		if _, err := Compile("print s="+call, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", call)
		}
	}
}
