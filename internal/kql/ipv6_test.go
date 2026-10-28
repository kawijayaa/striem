package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestIPv6ExecutionAndMetadata(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(ip:string)["fe80::1","fe80::2","2001:db8::1","bad"]|where ipv6_is_in_range(ip,"fe80::/64")|count`, `[[2]]`},
		{`print b=ipv6_is_in_range("::1","::/0"),f=ipv6_is_in_range("::1","::2"),n=ipv6_is_in_range("bad","::/0")`, `[[1 0 <nil>]]`},
		{`print b=ipv6_is_in_range("::1","::/0")|getschema|project ColumnType`, `[[bool]]`},
		{`datatable(ip:string)["192.168.255.255","192.168.255.255/24","255.255.255.255"]|project ip=parse_ipv6(ip)`, `[[0000:0000:0000:0000:0000:ffff:c0a8:ffff] [0000:0000:0000:0000:0000:ffff:c0a8:ff00] [0000:0000:0000:0000:0000:ffff:ffff:ffff]]`},
		{`print ip=parse_ipv6_mask("fe80::85d:e82c:9446:7994/120",124),c=ipv6_compare("::","ffff::"),m=ipv6_is_match("192.168.1.1/24","::ffff:c0a8:01ff"),n=ipv6_is_match("bad","::1")`, `[[fe80:0000:0000:0000:085d:e82c:9446:7900 -1 1 <nil>]]`},
		{`datatable(ip:string)["fe80::1","fe80::2","2001:db8::1","bad"]|where ipv6_is_match(ip,"fe80::/64")|count`, `[[2]]`},
		{`let mask=120;print ip=parse_ipv6_mask("192.168.255.255",mask)`, `[[0000:0000:0000:0000:0000:ffff:c0a8:ff00]]`},
		{`let f=(ip:string){ipv6_is_match(ip,"::ffff:c0a8:01ff",120)};print x=f("192.168.1.1")`, `[[1]]`},
		{`print p=parse_ipv6("bad"),m=parse_ipv6_mask("::1",long(null)),c=ipv6_compare("::1","::1",long(null))`, `[[  <nil>]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print match=ipv6_is_match("::1","::1"),comparison=ipv6_compare("::1","::2")`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.BooleanColumns["match"]; !ok {
		t.Fatal("IPv6 match lost Boolean metadata")
	}
	if _, ok := compiled.BooleanColumns["comparison"]; ok {
		t.Fatal("comparison marked Boolean")
	}
	if got := fmt.Sprint(featureRows(t, db, `print p=parse_ipv6("::1"),m=parse_ipv6_mask("::1",128),c=ipv6_compare("::1","::2"),b=ipv6_is_match("::1","::1")|getschema|project ColumnType`)); got != "[[string] [string] [long] [bool]]" {
		t.Fatalf("IPv6 result schema changed: %s", got)
	}
	for _, expression := range []string{`ipv6_is_in_range("::1")`, `ipv6_is_in_range("::1",1)`, `ipv6_is_in_range("::1","::/0",1)`, `parse_ipv6()`, `parse_ipv6(1)`, `parse_ipv6("::1",1)`, `parse_ipv6_mask("::1")`, `parse_ipv6_mask("::1",1.5)`, `ipv6_compare("::1")`, `ipv6_compare("::1",false)`, `ipv6_is_match("::1","::1",true)`, `ipv6_is_match("::1","::1",1,2)`} {
		if _, err := Compile("print x="+expression, time.Now()); err == nil {
			t.Fatalf("invalid IPv6 signature accepted: %s", expression)
		}
	}
}
