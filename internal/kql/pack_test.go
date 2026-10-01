package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestDynamicConstructors(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`range x from 1 to 3 step 1 | extend y=x*2 | extend z=y*2 | project a=pack_array(x,y,z)`, `[[[1,2,4]] [[2,4,8]] [[3,6,12]]]`},
		{`range x from 1 to 3 step 1 | extend y=x*2 | project a=pack_array(*)`, `[[[1,2]] [[2,4]] [[3,6]]]`},
		{`let a=99; datatable(a:long,b:string)[1,"x"] | project x=pack_array(*)`, `[[[1,"x"]]]`},
		{`print a=pack_array(true,false,null,9223372036854775807,"{\"x\":1}",dynamic([1,true]))`, `[[[true,false,null,9223372036854775807,"{\"x\":1}",[1,true]]]]`},
		{`print a=bag_pack("Level","Information","ProcessID",1234,"Data",bag_pack("url","www.bing.com"))`, `[[{"Data":{"url":"www.bing.com"},"Level":"Information","ProcessID":1234}]]`},
		{`datatable(Destination:int,Message:string)[100,"Hi",200,"Hello"] | extend MyBag=bag_pack("Dest",Destination,"Text",Message) | project Dest=MyBag.Dest,Text=MyBag.Text`, `[[100 Hi] [200 Hello]]`},
		{`print a=pack("x",true),b=pack_dictionary("x",null)`, `[[{"x":true} {"x":null}]]`},
		{`print a=pack_array('a\u0000b','😀'),b=bag_pack('a\u0000b','😀')`, `[[["a\u0000b","😀"] {"a\u0000b":"😀"}]]`},
		{`print a=pack_array(bag_pack("long",9223372036854775807),dynamic({"x":[true,null]}))`, `[[[{"long":9223372036854775807},{"x":[true,null]}]]]`},
		{`Events | where n==1 | project a=bag_pack(user,n)`, `[[{"alice":1}]]`},
		{`print a=pack_array(1,2) | mv-expand a to typeof(long) | order by a asc`, `[[1] [2]]`},
		{`print a=bag_pack("x",1) | getschema | project DataType`, `[[System.Object]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
	for _, expr := range []string{`pack_array()`, `pack_array(*,1)`, `bag_pack()`, `bag_pack("a")`, `bag_pack(1,2)`, `pack_dictionary("a",1,"b")`} {
		if _, err := Compile(`Events | project a=`+expr, time.Now(), featureCatalog()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}
