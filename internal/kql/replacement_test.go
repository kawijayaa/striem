package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestLiteralReplacement(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ expression, want string }{
		{`replace_string("A magic trick can turn a cat into a dog","cat","hamster")`, `[[A magic trick can turn a hamster into a dog]]`},
		{`replace_string("aaaaa","aa","X")`, `[[XXa]]`},
		{`replace_string("a.b.a",".","$")`, `[[a$b$a]]`},
		{`replace_string("a😀é😀","😀","中")`, `[[a中é中]]`},
		{`replace_string("a\u0000b","\u0000","X")`, `[[aXb]]`},
		{`replace_string("a\u0000b\u0000b","\u0000b","X")`, `[[aXX]]`},
		{`replace_string("abc","b","\u0000")`, "[[a\x00c]]"},
		{`replace_string("ABC","a","X")`, `[[ABC]]`},
		{`replace_string("abc","","X")`, `[[abc]]`},
		{`replace_string("abc","b","")`, `[[ac]]`},
		{`replace(user,"li","X")`, `[[aXce]]`},
	} {
		t.Run(tc.expression, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, `Events | where n==1 | project `+tc.expression)); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	for _, expression := range []string{`replace_string("a","b")`, `replace_string("a","b","c","d")`, `replace_string(1,"b","c")`, `replace_string("a",true,"c")`, `replace("a","b",1)`} {
		if _, err := Compile(`Events | project `+expression, time.Now(), featureCatalog()); err == nil {
			t.Errorf("accepted %s", expression)
		}
	}
	if got := fmt.Sprint(featureRows(t, db, `Events | where n==1 | project s=replace_string(user,"alice","X") | getschema | project DataType`)); got != `[[System.String]]` {
		t.Fatal(got)
	}
}
