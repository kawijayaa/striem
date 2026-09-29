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
