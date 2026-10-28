package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestURLEncodeExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`let url='https://www.bing.com/hello world';print form=url_encode(url),component=url_encode_component(url)`, `[[https%3a%2f%2fwww.bing.com%2fhello+world https%3a%2f%2fwww.bing.com%2fhello%20world]]`},
		{`datatable(s:string)["a+b","é😀",""]|project s=url_decode(url_encode_component(s))`, `[[a+b] [é😀] []]`},
		{`print e=url_encode("a b")|getschema|project ColumnType`, `[[string]]`},
		{`let f=(s:string){url_encode_component(s)};print e=f("a b")`, `[[a%20b]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, call := range []string{`url_encode()`, `url_encode(1)`, `url_encode_component(false)`, `url_encode_component("a","b")`, `url_decode(dynamic([]))`} {
		if _, err := Compile("print x="+call, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", call)
		}
	}
}
