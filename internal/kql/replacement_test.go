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

func TestMultipleLiteralReplacement(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print s=replace_strings("A magic trick can turn a cat into a dog",dynamic(["cat","dog"]),dynamic(["dog","pigeon"]))`, `[[A magic trick can turn a dog into a pigeon]]`},
		{`print s=replace_strings("A magic trick can turn a cat into a dog",dynamic(["turn"," into a dog"]),dynamic(["disappear",""]))`, `[[A magic trick can disappear a cat]]`},
		{`print s=replace_strings("This is an example of using replace_strings()",dynamic(["This","is"]),dynamic(["This","was"]))`, `[[This was an example of using replace_strings()]]`},
		{`print s=replace_strings("This is an example of using replace_strings()",dynamic(["is","This"]),dynamic(["was","This"]))`, `[[Thwas was an example of using replace_strings()]]`},
		{`print s=replace_strings("This is an example of using replace_strings()",dynamic(["This","is"]),dynamic([12345,"was"]))`, `[[This was an example of using replace_strings()]]`},
		{`print s=replace_strings("aaaaa",dynamic(["aa","a"]),dynamic(["X","Y"]))`, `[[XXY]]`},
		{`print s=replace_strings("ab",dynamic(["b","ab"]),dynamic(["X","Y"]))`, `[[aX]]`},
		{`print s=replace_strings("a\u0000😀b",dynamic(["\u0000","😀"]),dynamic(["X","中"]))`, `[[aX中b]]`},
		{`print s=replace_strings("ab",dynamic([1,null,{},[],"a","b"]),dynamic(["X","X","X","X",null,false]))`, `[[ab]]`},
		{`print s=replace_strings("ab",dynamic([]),dynamic([]))`, `[[ab]]`},
		{`let lookups=dynamic(["alice","bob"]); let rewrites=dynamic(["A","B"]); Events | where n<3 | project s=replace_strings(user,lookups,rewrites) | order by s asc`, `[[A] [B]]`},
		{`print s=replace_strings("ab",dynamic(["a"]),dynamic(["X"])) | getschema | project DataType`, `[[System.String]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	for _, expr := range []string{`replace_strings("a",dynamic([]))`, `replace_strings("a","b",dynamic([]))`, `replace_strings(1,dynamic([]),dynamic([]))`, `replace_strings("a",dynamic([]),"b")`} {
		if _, err := Compile(`print s=`+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}
