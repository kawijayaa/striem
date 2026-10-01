package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestUnicodeCaseConversion(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print x=tolower("Hello")=="hello",y=toupper("hello")=="HELLO"`, `[[1 1]]`},
		{`print x=tolower("ÉCOLE ΓΕΙΑ ДОМ"),y=toupper("école γεια дом")`, `[[école γεια дом ÉCOLE ΓΕΙΑ ДОМ]]`},
		{`print x=tolower("A\u0000É"),y=toupper("a\u0000é")`, "[[a\x00é A\x00É]]"},
		{`print x=tolower("😀中123"),y=toupper("")`, `[[😀中123 ]]`},
		{`Events | project User=toupper(user) | where User=="BOB"`, `[[BOB]]`},
		{`datatable(x:string)["É","é"] | summarize N=count() by x=tolower(x)`, `[[é 2]]`},
		{`print x=tolower("Hello") | getschema | project DataType`, `[[System.String]]`},
		{`Events | where n==1 | project x=toupper(RawData.user)`, `[[ALICE]]`},
		{`Events | where n==1 | project x=tolower(RawData.missing)`, `[[<nil>]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	for _, name := range []string{"tolower", "toupper"} {
		for _, expr := range []string{name + `()`, name + `("a","b")`, name + `(1)`, name + `(true)`} {
			if _, err := Compile(`print x=`+expr, time.Now()); err == nil {
				t.Errorf("accepted %s", expr)
			}
		}
	}
}
