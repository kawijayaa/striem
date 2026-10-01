package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestStringIndexing(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ expr, want string }{
		{`substring("123456",1),substring("123456",2,2),substring("ABCD",0,2),substring("123456",-2,2)`, `[[23456 34 AB 56]]`},
		{`strlen(RawData.user),substring(RawData.n,0)`, `[[5 1]]`},
		{`substring(1234567,1,4),substring(true,1),substring(123.45,0,3)`, `[[2345 rue 123]]`},
		{`substring("abc",0),substring("abc",3),substring("abc",20),substring("abc",-20)`, `[[abc   abc]]`},
		{`substring("abc",1,0),substring("abc",1,9007199254740993),substring("abc",9223372036854775807),substring("abc",-9223372036854775807)`, `[[ bc  abc]]`},
		{`substring("a😀é中",1,2),substring("a😀é中",-2),strlen("a😀é中"),strlen("é")`, `[[😀é é中 4 2]]`},
		{`strlen("a\u0000b"),substring("a\u0000b",1,2)`, `[[3 \u0000b]]`},
		{`substring(user,n-1,2),strlen(user)`, `[[al 5]]`},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			got := fmt.Sprint(featureRows(t, db, `Events | where n==1 | project `+tc.expr))
			want := tc.want
			if tc.expr == `strlen("a\u0000b"),substring("a\u0000b",1,2)` {
				want = "[[3 \x00b]]"
			}
			if got != want {
				t.Fatalf("got %q want %q", got, want)
			}
		})
	}
	for _, expr := range []string{`substring("a")`, `substring("a",0,1,2)`, `substring("a",0.5)`, `strlen()`, `strlen(1)`, `strlen("a","b")`} {
		if _, err := Compile(`Events | project `+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}
