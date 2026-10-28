package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestHashExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(s:string)["a","b","a"]|summarize n=count() by h=hash_sha256(s)|sort by n desc|project n`, `[[2] [1]]`},
		{`print h1=hash_md5("World"),h2=hash_md5(datetime(2020-01-01))`, `[[f5a7924e621e84c9280a9a27e1bcb7f6 786c530672d1f8db31fee25ea8a9390b]]`},
		{`print h1=hash_sha1("World"),h2=hash_sha1(datetime(2020-01-01))`, `[[70c07ec18ef89c5309bbb0937f3a6342411e1fdd e903e533f4d636b4fc0dcf3cf81e7b7f330de776]]`},
		{`print h1=hash_sha256("World"),h2=hash_sha256(datetime(2020-01-01))`, `[[78ae647dc5544d227130a0682a51e30bc7777fbb6d8a8f17007463a3ecd1d524 ba666752dc1a20eb750b0eb64e780cc4c968bc9fb8813461c1d7e750f302d71d]]`},
		{`print h=hash_sha256("abc"),e=hash_md5(""),s=hash_sha1("abc")`, `[[ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad d41d8cd98f00b204e9800998ecf8427e a9993e364706816aba3e25717850c26c9cd0d89d]]`},
		{`datatable(state:string)["TEXAS","KANSAS","IOWA","ILLINOIS","MISSOURI"]|project h=hash_sha256(state)`, `[[9087f20f23f91b5a77e8406846117049029e6798ebbd0d38aea68da73a00ca37] [c80e328393541a3181b258cdb4da4d00587c5045e8cf3bb6c8fdb7016b69cc2e] [f85893dca466f779410f65cd904fdc4622de49e119ad4e7c7e4a291ceed1820b] [ae3eeabfd7eba3d9a4ccbfed6a9b8cff269dc43255906476282e0184cf81b7fd] [d15dfc28abc3ee73b7d1f664a35980167ca96f6f90e034db2a6525c0b8ba61b1]]`},
		{`print h=hash_md5("abc")|getschema|project ColumnType`, `[[string]]`},
		{`print b=hash_sha256(true)==hash_sha256("true"),n=hash_md5(long(null))==hash_md5(""),i=hash_md5(long(9223372036854775807))==hash_md5("9223372036854775807")`, `[[1 1 1]]`},
		{`let f=(s:string){hash_sha256(s)};print h=f("abc")`, `[[ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, call := range []string{`hash_md5()`, `hash_sha1("a","b")`, `hash_sha256(dynamic({"a":1}))`} {
		if _, err := Compile("print h="+call, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", call)
		}
	}
}
