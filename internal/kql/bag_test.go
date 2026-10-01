package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestBagAggregates(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`Events | summarize Bag=make_bag(parse_json('{"a":1,"nested":{"x":[true,null,9007199254740993]}}'))`, `[[{"a":1,"nested":{"x":[true,null,9007199254740993]}}]]`},
		{`Events | summarize Bag=make_bag_if(RawData,n == 2) | project Host=Bag.host`, `[[b]]`},
		{`Events | summarize Bag=make_bag_if(RawData,false)`, `[[{}]]`},
		{`Events | summarize Bag=make_bag_if(RawData,RawData.missing == 1)`, `[[{}]]`},
		{`Events | where n < 0 | summarize Bag=make_bag(RawData)`, `[[{}]]`},
		{`Events | summarize Bag=make_bag(parse_json('[1,2]'))`, `[[{}]]`},
		{`Events | summarize Bag=make_bag('{"not":"a dynamic object"}')`, `[[{}]]`},
		{`Events | summarize Bag=make_bag(1)`, `[[{}]]`},
		{`Events | summarize Bag=make_bag(parse_json('{"a":null,"b":false}'))`, `[[{"a":null,"b":false}]]`},
		{`Events | summarize Bag=make_dictionary(parse_json('{"a":1}'))`, `[[{"a":1}]]`},
		{`let size=1; Events | summarize Bag=make_bag(parse_json('{"a":1,"b":2}'),size) | project Size=array_length(bag_keys(Bag))`, `[[1]]`},
		{`Events | summarize Bag=make_bag_if(RawData,n == 2,1) | project Size=array_length(bag_keys(Bag))`, `[[1]]`},
		{`Events | summarize Bag=make_bag(RawData) by host | project host,Size=array_length(bag_keys(Bag)) | order by host asc`, `[[a 5] [b 5]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	compiled, err := Compile(`Events | summarize Bag=make_bag(RawData) | project Renamed=Bag`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["Renamed"]; !ok {
		t.Fatal("bag type lost through projection")
	}
}

func TestBagAggregateDiagnostics(t *testing.T) {
	for _, expr := range []string{`make_bag()`, `make_bag(RawData,0)`, `make_bag(RawData,1048577)`, `make_bag(RawData,1.5)`, `make_bag(RawData,n)`, `make_bag_if(RawData)`, `make_bag_if(RawData,"yes")`, `make_bag_if(RawData,1)`} {
		if _, err := Compile(`Events | summarize Bag=`+expr, time.Now(), featureCatalog()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}

func TestBagOperations(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print b=bag_merge(dynamic({"A1":12,"B1":2,"C1":3}),dynamic({"A2":81,"B2":82,"A1":1}))`, `[[{"A1":12,"A2":81,"B1":2,"B2":82,"C1":3}]]`},
		{`print b=bag_merge(dynamic({"a":null,"nested":{"x":1}}),dynamic({"a":2,"nested":{"y":2},"long":9223372036854775807}))`, `[[{"a":null,"long":9223372036854775807,"nested":{"x":1}}]]`},
		{`print b=bag_remove_keys(dynamic({"key1":123,"key2":{"prop1":123,"prop2":456},"key3":456}),dynamic(["$.key2.prop1","key3"]))`, `[[{"key1":123,"key2":{"prop2":456}}]]`},
		{`print b=bag_remove_keys(dynamic({"a.b":1,"a":{"b":2},"list":[1,2]}),dynamic(["a.b","$.a.b","$.list.x","missing"]))`, `[[{"a":{},"list":[1,2]}]]`},
		{`print b=bag_remove_keys(dynamic({"a.b":{"é":1,"keep":9223372036854775807}}),dynamic(["$['a.b']['é']"]))`, `[[{"a.b":{"keep":9223372036854775807}}]]`},
		{`print b=bag_remove_keys(bag_merge(bag_pack("a",1),bag_pack("b",2)),pack_array("a")) | project value=b.b`, `[[2]]`},
		{`print b=bag_remove_keys(dynamic({"a":{"b":1}}),dynamic(["$.a.b","a"]))`, `[[{}]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	compiled, err := Compile(`print b=bag_merge(dynamic({}),dynamic({})) | project renamed=b`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["renamed"]; !ok {
		t.Fatal("dynamic metadata lost")
	}
	for _, expr := range []string{`bag_merge(dynamic({}))`, `bag_merge("a",dynamic({}))`, `bag_remove_keys(dynamic({}))`, `bag_remove_keys(dynamic({}),"a")`} {
		if _, err := Compile(`print b=`+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}

func TestBagSetKey(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(input:dynamic)[dynamic({"key1":1,"key2":2}),dynamic({"key1":1,"key3":"abc"})] | project result=bag_set_key(input,"key3",3)`, `[[{"key1":1,"key2":2,"key3":3}] [{"key1":1,"key3":3}]]`},
		{`datatable(input:dynamic)[dynamic({"key1":123,"key2":{"prop1":123,"prop2":"xyz"}}),dynamic({"key1":123})] | project result=bag_set_key(input,"$.key2.prop1","abc")`, `[[{"key1":123,"key2":{"prop1":"abc","prop2":"xyz"}}] [{"key1":123,"key2":{"prop1":"abc"}}]]`},
		{`print b=bag_set_key(dynamic({}),"$.a.b.c",true)`, `[[{"a":{"b":{"c":true}}}]]`},
		{`print b=bag_set_key(dynamic({"keep":9223372036854775807}),"long",9223372036854775807)`, `[[{"keep":9223372036854775807,"long":9223372036854775807}]]`},
		{`print b=bag_set_key(dynamic({}),"str","{\"a\":1}")`, `[[{"str":"{\"a\":1}"}]]`},
		{`print b=bag_set_key(dynamic({}),"nested",pack_array(true,null,bag_pack("x",2)))`, `[[{"nested":[true,null,{"x":2}]}]]`},
		{`print b=bag_set_key(dynamic({"a":1}),"a",null)`, `[[{"a":null}]]`},
		{`print b=bag_set_key(dynamic(null),"a",1)`, `[[<nil>]]`},
		{`print b=bag_set_key(dynamic([]),"a",1)`, `[[<nil>]]`},
		{`print b=bag_set_key(dynamic({}),"$['a.b']['é']",1)`, `[[{"a.b":{"é":1}}]]`},
		{`print b=bag_set_key(dynamic({}),"a.b",1) | project value=b["a.b"]`, `[[1]]`},
		{`let path="$.nested.value"; Events | where n == 1 | project b=bag_set_key(RawData,path,n) | project value=b.nested.value`, `[[1]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	compiled, err := Compile(`print b=bag_set_key(dynamic({}),"a",true) | project renamed=b`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["renamed"]; !ok {
		t.Fatal("dynamic metadata lost")
	}
	for _, expr := range []string{`bag_set_key(dynamic({}),"a")`, `bag_set_key("{}","a",1)`, `bag_set_key(dynamic({}),1,1)`} {
		if _, err := Compile(`print b=`+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}

func TestBagZip(t *testing.T) {
	db := featureDatabase(t)
	// All six documented cases, including non-array input columns.
	query := `let Data=datatable(KeysArray:dynamic,ValuesArray:dynamic)[dynamic(["a","b","c"]),dynamic([1,"2",3.4]),dynamic(["a","b","c"]),dynamic([1,"2"]),dynamic(["a","b"]),dynamic([1,"2",2.5]),dynamic(["a",8,"b"]),dynamic([1,"2",2.5]),dynamic(["a",8,"b"]),dynamic(1),dynamic("a"),dynamic([1,"2",2.5])]; Data | project NewBag=bag_zip(KeysArray,ValuesArray)`
	want := `[[{"a":1,"b":"2","c":3.4}] [{"a":1,"b":"2","c":null}] [{"a":1,"b":"2"}] [{"a":1,"b":2.5}] [{"a":null,"b":null}] [<nil>]]`
	if got := fmt.Sprint(featureRows(t, db, query)); got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	for _, tc := range []struct{ query, want string }{
		{`print b=bag_zip(dynamic([]),dynamic([]))`, `[[{}]]`},
		{`print b=bag_zip(dynamic([true,null,{},[1]]),dynamic([1,2,3,4]))`, `[[{}]]`},
		{`print b=bag_zip(pack_array("long","bool","array","object"),pack_array(9223372036854775807,true,dynamic([null,1]),bag_pack("x",false)))`, `[[{"array":[null,1],"bool":true,"long":9223372036854775807,"object":{"x":false}}]]`},
		{`print b=bag_zip(dynamic(["é\u0000",""]),dynamic([1,2]))`, `[[{"":2,"é\u0000":1}]]`},
		{`print b=bag_zip(dynamic(["a.b","$.x"]),dynamic([1,2]))`, `[[{"$.x":2,"a.b":1}]]`},
		{`print b=bag_zip(dynamic(["<key>"]),dynamic(["&value"]))`, `[[{"\u003ckey\u003e":"\u0026value"}]]`},
		{`let keys=dynamic(["one","two"]); print b=bag_zip(keys,pack_array(1,2)) | project value=b.two`, `[[2]]`},
		{`Events | where n == 1 | project b=bag_zip(dynamic(["one","two"]),items) | project value=b.two`, `[[2]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	compiled, err := Compile(`print bag_zip(dynamic([]),dynamic([])) | project renamed=print_0`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["renamed"]; !ok {
		t.Fatal("dynamic metadata lost")
	}
	for _, expr := range []string{`bag_zip()`, `bag_zip(dynamic([]))`, `bag_zip("[]",dynamic([]))`, `bag_zip(dynamic([]),1)`, `bag_zip(dynamic([]),dynamic([]),dynamic([]))`} {
		if _, err := Compile(`print b=`+expr, time.Now()); err == nil {
			t.Errorf("accepted %s", expr)
		}
	}
}
