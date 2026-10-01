package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestRegexTrimming(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`let source=@'https://bing.com';let pattern='https://';print x=trim_start(pattern,source)`, `[[bing.com]]`},
		{`let source=@'bing.com';let pattern='.com';print x=trim_end(pattern,source)`, `[[bing]]`},
		{`let source=@'--https://bing.com--';let pattern='--';print x=trim(pattern,source)`, `[[https://bing.com]]`},
		{`range x from 1 to 2 step 1 | project s=strcat('-  ','Te st',x,@'// $') | project a=trim_start(@'[^\w]+',s),b=trim_end(@'[^\w]+',s),c=trim(@'[^\w]+',s)`, `[[Te st1// $ -  Te st1 Te st1] [Te st2// $ -  Te st2 Te st2]]`},
		{`print a=trim_start(@'\s+','    Hello, world!    '),b=trim_end(@'\s+','    Hello, world!    '),c=trim(@'\s+','    Hello, world!    ')`, `[[Hello, world!         Hello, world! Hello, world!]]`},
		{`print a=trim_start('ab','ababXabab'),b=trim_end('ab','ababXabab'),c=trim('ab','ababXabab')`, `[[abXabab ababXab X]]`},
		{`print a=trim('😀','😀é😀'),b=trim('\u0000','\u0000é\u0000')`, `[[é é]]`},
		{`print a=trim('x|y','xxxAy'),b=trim_start('a|b','baba')`, `[[A aba]]`},
		{`print a=trim_start('(?m)x','xA\nxB'),b=trim_end('(?m)x','Ax\nBx')`, "[[A\nxB Ax\nB]]"},
		{`print a=trim('x',''),b=trim('x','y')`, `[[ y]]`},
		{`Events | where n==1 | project x=trim_start('a',user)`, `[[lice]]`},
		{`print x=trim_end('b','ab') | getschema | project DataType`, `[[System.String]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	for _, name := range []string{"trim", "trim_start", "trim_end"} {
		for _, expr := range []string{name + `("a")`, name + `("a","b","c")`, name + `("(","a")`, name + `(user,"a")`, name + `("a",1)`} {
			if _, err := Compile(`Events | project x=`+expr, time.Now(), featureCatalog()); err == nil {
				t.Errorf("accepted %s", expr)
			}
		}
	}
}
