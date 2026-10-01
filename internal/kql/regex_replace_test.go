package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestRegexReplacementExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print x=replace_regex('Number is 1.000000',@'is (\d+)',@'was: \1')`, `[[Number was: 1.000000]]`},
		{`range x from 1 to 3 step 1 | extend s=strcat('Number is ',tostring(x)) | project s=replace_regex(s,@'is (\d+)',@'was: \1')`, `[[Number was: 1] [Number was: 2] [Number was: 3]]`},
		{`print x=replace_regex('a12b34',@'(\d+)',@'[\0/\1]')`, `[[a[12/12]b[34/34]]]`},
		{`print x=replace_regex('aaaaa','aa','X')`, `[[XXa]]`},
		{`print x=replace_regex('abc','b',@'$1')`, `[[a$1c]]`},
		{`print x=replace_regex('abc','b',@'\\1')`, `[[a\1c]]`},
		{`print x=replace_regex('a😀é😀','(😀)',@'<\1>')`, `[[a<😀>é<😀>]]`},
		{`print x=replace_regex('a\u0000b','\u0000','X')`, `[[aXb]]`},
		{`print x=replace_regex('a\nb','(?s)a.b','X')`, `[[X]]`},
		{`print x=replace_regex('a\nb','(?m)^.','X')`, "[[X\nX]]"},
		{`print x=replace_regex('b','(a)?b',@'<\1>')`, `[[<>]]`},
		{`Events | where n==1 | project x=replace(@'(li)',@'<\1>',user)`, `[[a<li>ce]]`},
		{`let pattern=@'(li)';let rewrite=@'<\1>'; Events | where n==1 | project x=replace_regex(user,pattern,rewrite)`, `[[a<li>ce]]`},
		{`print x=replace_regex('abc','b','X') | getschema | project DataType`, `[[System.String]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	for _, expr := range []string{`replace_regex("a","b")`, `replace_regex(1,"b","c")`, `replace_regex("a","(","c")`, `replace_regex("a",user,"c")`, `replace_regex("a","b",user)`, `replace(user,"c","a")`} {
		if _, err := Compile(`Events | project x=`+expr, time.Now(), featureCatalog()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}
