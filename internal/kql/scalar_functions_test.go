package kql

import (
	"fmt"
	"testing"
	"time"
)

// Microsoft's examples for string size, guid base64 and time zones, with
// invalid-input edges; geo, series and unit functions are not supported.
func TestScalarRuntimeFunctions(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=string_size("hello"), b=string_size("⒦⒰⒮⒯⒪"), c=string_size(""), e=base64_encode_fromguid(toguid("ae3133f2-6e22-49ae-b06a-16e6a9b212eb")), d=base64_decode_toguid("JpbpECu8dUy7Pv5gbeJXAA=="), bad=base64_decode_toguid("AAAA")`,
			`[[5 15 0 8jMxriJurkmwahbmqbIS6w== 10e99626-bc2b-4c75-bb3e-fe606de25700 <nil>]]`},
		// Decoding inverts encoding (.NET Guid byte order).
		{`print r=base64_decode_toguid(base64_encode_fromguid(toguid("ae3133f2-6e22-49ae-b06a-16e6a9b212eb")))`, `[[ae3133f2-6e22-49ae-b06a-16e6a9b212eb]]`},
		{`datatable(local_dt: datetime, tz: string)[datetime(2020-02-02 20:02:20), 'US/Pacific', datetime(2020-02-02 20:02:20), 'Europe/Paris', datetime(2020-07-01 12:00), 'Nowhere/City'] | extend utc_dt = datetime_local_to_utc(local_dt, tz), back = datetime_utc_to_local(datetime_local_to_utc(local_dt, tz), tz) | project utc_dt, back`,
			`[[2020-02-03T04:02:20.000000000Z 2020-02-02T20:02:20.000000000Z] [2020-02-02T19:02:20.000000000Z 2020-02-02T20:02:20.000000000Z] [<nil> <nil>]]`},
		{`range i from 1 to 50 step 1 | extend g=new_guid() | summarize distinct=dcount(g), shaped=countif(g matches regex "^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$")`, `[[50 50]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print new_guid(1)`, `print datetime_utc_to_local(1, "UTC")`, `print convert_length(1.2, "Meter", "Foot")`, `print geo_point_to_h3cell(1, 1)`, `print series_add(dynamic([1]), 1)`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid call accepted: %s", query)
		}
	}
}

// Microsoft's pack_all example, which packs every input column.
func TestPackAll(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(Source:string,Target:string,CharsCount:long)['555-1234','555-1212',46, '555-1313','',42, '','555-3456',74] | extend Packed=pack_all(), PackedIgnoreNullEmpty=pack_all(true) | project Packed, PackedIgnoreNullEmpty`,
			`[[{"CharsCount":46,"Source":"555-1234","Target":"555-1212"} {"CharsCount":46,"Source":"555-1234","Target":"555-1212"}] [{"CharsCount":42,"Source":"555-1313","Target":""} {"CharsCount":42,"Source":"555-1313"}] [{"CharsCount":74,"Source":"","Target":"555-3456"} {"CharsCount":74,"Target":"555-3456"}]]`},
		{`datatable(a:long, b:dynamic)[1, dynamic(null), 2, dynamic([1])] | project p=pack_all(true)`, `[[{"a":1}] [{"a":2,"b":[1]}]]`},
		{`print x=1 | project p=pack_all(false)`, `[[{"x":1}]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	if _, err := Compile(`print x=1 | project pack_all(x)`, time.Now()); err == nil {
		t.Fatal("non-constant pack_all argument accepted")
	}
}

// Microsoft's has_ipv4 examples and the prefix/any forms; temporal range.
func TestIPv4SearchAndTemporalRange(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=has_ipv4('05:04:54 127.0.0.1 GET /favicon.ico 404', '127.0.0.1'), b=has_ipv4('05:04:54 127.0.0.256 GET /favicon.ico 404', '127.0.0.256'), c=has_ipv4('05:04:54127.0.0.1 GET /favicon.ico 404', '127.0.0.1'), d=has_ipv4('ip=10.0.0.1.', '10.0.0.1'), e=has_ipv4('10.0.0.12', '10.0.0.1')`, `[[1 0 0 1 0]]`},
		{`print a=has_ipv4_prefix('05:04:54 127.0.0.1 GET', '127.0.'), b=has_ipv4_prefix('05:04:54 127.0.0.1 GET', '127.0'), c=has_ipv4_prefix('x 127.0.0.1', '127.0.0.1'), d=has_ipv4_prefix('x 127.0.0.1', '27.')`, `[[1 0 1 0]]`},
		{`print a=has_any_ipv4('src 10.0.0.1 dst 10.0.0.2', '1.1.1.1', '10.0.0.2'), b=has_any_ipv4('src 10.0.0.1', dynamic(['8.8.8.8', '10.0.0.1'])), c=has_any_ipv4_prefix('src 192.168.1.5', dynamic(['10.', '192.168.'])), d=has_any_ipv4('none', '1.1.1.1')`, `[[1 1 1 0]]`},
		{`print r=range(datetime(2024-01-01), datetime(2024-01-01 02:00), 1h), s=range(1h, 3h, 1h), e=range(datetime(2024-01-02), datetime(2024-01-01), 1h)`,
			`[[["2024-01-01T00:00:00.000000000Z","2024-01-01T01:00:00.000000000Z","2024-01-01T02:00:00.000000000Z"] ["01:00:00","02:00:00","03:00:00"] []]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print range(datetime(2024-01-01), datetime(2024-01-02))`, `print range(datetime(2024-01-01), 5, 1h)`, `print has_any_ipv4(dynamic([]), '1.1.1.1')`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid call accepted: %s", query)
		}
	}
}

// Microsoft's mv-apply examples with take, top, summarize and item indexes.
func TestMvApplySubqueries(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable (index: int, Arr: dynamic)[0, dynamic([7]), 1, dynamic([6, 11, 7])] | mv-apply Arr2=Arr to typeof(long) on (take 2) | order by index asc, Arr2 asc`, `[[0 [7] 7] [1 [6,11,7] 6] [1 [6,11,7] 11]]`},
		{`datatable (index: int, Arr: dynamic)[0, dynamic([1, 5, 3]), 1, dynamic([10, 2, 5, 7])] | mv-apply topElem=Arr to typeof(long) on (top 1 by topElem) | order by index asc`, `[[0 [1,5,3] 5] [1 [10,2,5,7] 10]]`},
		{`datatable (index: int, Arr: dynamic)[0, dynamic([5, 1, 3]), 1, dynamic([4, 10, 8, 7])] | mv-apply Arr2=Arr to typeof(long) on (top 2 by Arr2 | summarize Top2=make_list(Arr2)) | order by index asc`, `[[0 [5,1,3] [5,3]] [1 [4,10,8,7] [10,8]]]`},
		{`datatable (Val:int, Arr1:dynamic, Arr2:dynamic)[1, dynamic(['A1', 'A2', 'A3']), dynamic([10, 30, 7]), 7, dynamic(['B1', 'B2', 'B5']), dynamic([15, 11, 50])] | mv-apply NewArr1=Arr1, NewArr2=Arr2 to typeof(long) on (top 2 by NewArr2 | summarize NewArr1=make_list(NewArr1), NewArr2=make_list(NewArr2)) | order by Val asc`,
			`[[1 ["A1","A2","A3"] [10,30,7] ["A2","A1"] [30,10]] [7 ["B1","B2","B5"] [15,11,50] ["B5","B1"] [50,15]]]`},
		{`datatable (row: int, Arr: dynamic)[0, dynamic([5, 1, 3]), 1, dynamic([4, 10, 8, 7])] | mv-apply with_itemindex=index value=Arr to typeof(long) on (where index >= 2) | order by row asc, index asc`, `[[0 [5,1,3] 3 2] [1 [4,10,8,7] 8 2] [1 [4,10,8,7] 7 3]]`},
		{`print rings = dynamic([["a"], ["b", "c"]]) | mv-apply rings on (summarize cells = make_list(rings))`, `[[["a","b","c"]]]`},
		{`datatable (g: string, Arr: dynamic)["x", dynamic([1, 2, 2]), "y", dynamic([3])] | mv-apply v=Arr to typeof(long) on (distinct v | count) | order by g asc`, `[[x [1,2,2] 2] [y [3] 1]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	if _, err := Compile(`print a=dynamic([1]) | mv-apply a on (join (print a=1) on a)`, time.Now()); err == nil {
		t.Fatal("unsupported mv-apply subquery accepted")
	}
}

// make_list and make_set add the elements of dynamic arrays (Microsoft's
// geo and H3 examples rely on it), skip nulls, and honor maximum sizes.
func TestCollectAggregates(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(x:long)[1, 2, long(null), 2, 3] | summarize l=make_list(x), s=make_set(x), n=make_list_with_nulls(x), i=make_list_if(x, x > 1), si=make_set_if(x, x > 1, 1), m=make_list(x, 2)`, `[[[1,2,2,3] [1,2,3] [1,2,null,2,3] [2,2,3] [2] [1,2]]]`},
		{`datatable(d:dynamic)[dynamic([1, 2]), dynamic([2, 3]), dynamic({"a":1}), dynamic(null)] | summarize l=make_list(d), s=make_set(d)`, `[[[1,2,2,3,{"a":1}] [1,2,3,{"a":1}]]]`},
		{`datatable(d:dynamic)[dynamic([[1], [2]])] | summarize l=make_list(d), p=make_list(pack_array(d))`, `[[[[1],[2]] [[[1],[2]]]]]`},
		{`datatable(b:bool, t:timespan, s:string)[true, 1h, "a<b", false, 2m, "a<b"] | summarize b=make_list(b), t=make_list(t), s=make_set(s)`, `[[[true,false] ["01:00:00","00:02:00"] ["a<b"]]]`},
		{`datatable(x:long)[] | summarize l=make_list(x), s=makeset(x)`, `[[[] []]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print x=1 | summarize make_list_if(x, 1)`, `print x=1 | summarize make_list_with_nulls(x, 5)`, `print x=1 | summarize make_set(x, 1.5)`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid call accepted: %s", query)
		}
	}
}

// Microsoft's bin_auto example and set query_now.
func TestSetRequestOptions(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{"set query_bin_auto_size=1h;\nset query_bin_auto_at=datetime(2017-01-01 00:05);\nrange Timestamp from datetime(2017-01-01 00:05) to datetime(2017-01-01 02:00) step 1m\n| summarize count() by bin_auto(Timestamp) | order by Timestamp asc", `[[2017-01-01T00:05:00.000000000Z 60] [2017-01-01T01:05:00.000000000Z 56]]`},
		{`set query_now = datetime(2020-01-01); print n=now(), a=now(-1d), b=ago(1d)`, `[[2020-01-01T00:00:00.000000000Z 2019-12-31T00:00:00.000000000Z 2019-12-31T00:00:00.000000000Z]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}

// Microsoft's buildschema example: types in first-seen order, nested
// objects and array indexers.
func TestBuildSchema(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(value: dynamic) [dynamic({"x":1, "y":3.5}), dynamic({"x":"somevalue", "z":[1, 2, 3]}), dynamic({"y":{"w":"zzz"}, "t":["aa", "bb"], "z":["foo"]})] | summarize buildschema(value)`,
			`[[{"x":["long","string"],"y":["double",{"w":"string"}],"z":{"` + "`indexer`" + `":["long","string"]},"t":{"` + "`indexer`" + `":"string"}}]]`},
		{`datatable(value: dynamic) [dynamic(null)] | summarize s=buildschema(value)`, `[[<nil>]]`},
		{`datatable(value: dynamic) [dynamic({"a":[]}), dynamic({"a":true})] | summarize s=buildschema(value)`, `[[{"a":[{"` + "`indexer`" + `":null},"bool"]}]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}

// evaluate narrow, pivot and ipv4_lookup, following Microsoft's examples.
func TestEvaluatePlugins(t *testing.T) {
	db := featureDatabase(t)
	storm := `datatable(State:string, EventType:string, Deaths:long)["ARKANSAS", "Heat", 3, "ARIZONA", "Flash Flood", 6, "ARKANSAS", "Heat", 1, "ARIZONA", "Lightning", 1]`
	ips := `let IP_Data = datatable(network:string, continent_code:string, country_name:string)["111.68.128.0/17", "AS", "Japan", "5.8.0.0/19", "EU", "Russia", "2.20.183.0/24", "EU", "United Kingdom"]; let IPs = datatable(ip:string)['2.20.183.12', '5.8.1.2', '192.165.12.17']; IPs`
	for _, tc := range []struct{ query, want string }{
		{`print a=1, b="x" | evaluate narrow()`, `[[0 a 1] [0 b x]]`},
		{storm + ` | project State, EventType | evaluate pivot(State) | order by EventType asc`, `[[Flash Flood 1 0] [Heat 0 2] [Lightning 1 0]]`},
		{storm + ` | evaluate pivot(State, sum(Deaths)) | order by EventType asc`, `[[Flash Flood 6 0] [Heat 0 4] [Lightning 1 0]]`},
		{storm + ` | evaluate pivot(State, sum(Deaths), EventType) | getschema | project ColumnName`, `[[EventType] [ARIZONA] [ARKANSAS]]`},
		{storm + ` | project State, EventType | evaluate pivot(State): (EventType:string, ARKANSAS:long) | order by EventType asc`, `[[Flash Flood 0] [Heat 2] [Lightning 0]]`},
		{ips + ` | evaluate ipv4_lookup(IP_Data, ip, network) | order by ip asc`, `[[2.20.183.12 2.20.183.0/24 EU United Kingdom] [5.8.1.2 5.8.0.0/19 EU Russia]]`},
		{ips + ` | evaluate ipv4_lookup(IP_Data, ip, network, return_unmatched = true) | order by ip asc`, `[[192.165.12.17 <nil> <nil> <nil>] [2.20.183.12 2.20.183.0/24 EU United Kingdom] [5.8.1.2 5.8.0.0/19 EU Russia]]`},
		{`let L = datatable(net:string, name:string)["fe80::/10", "link-local", "2001:db8::/32", "doc"]; datatable(ip:string)["2001:db8::1", "::1"] | evaluate ipv6_lookup(L, ip, net, true) | order by ip asc`, `[[2001:db8::1 2001:db8::/32 doc] [::1 <nil> <nil>]]`},
		{`let L = datatable(net:string, site:string, tag:string)["10.0.0.0/8", "a", "x", "10.0.0.0/8", "b", "y"]; datatable(ip:string, site:string)["10.1.2.3", "b"] | evaluate ipv4_lookup(L, ip, net, site)`, `[[10.1.2.3 b 10.0.0.0/8 y]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print a=1 | evaluate narrow(1)`, `print a=1 | evaluate pivot(nope)`, `print a=1 | evaluate pivot(a, percentile(a, 50))`, `print ip="1.1.1.1" | evaluate ipv4_lookup(nope, ip, net)`} {
		if _, err := Compile(query, time.Now(), CompileConfig{Tables: featureCatalog(), Values: testValues(db)}); err == nil {
			t.Fatalf("invalid plugin call accepted: %s", query)
		}
	}
}
