package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestIPAnyRangeExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print b=ipv4_is_in_any_range('192.168.1.6','192.168.1.1/24','10.0.0.1/8','127.1.0.1/16')`, `[[1]]`},
		{`print b=ipv4_is_in_any_range("127.0.0.1",dynamic(["127.0.0.1","192.168.1.1"]))`, `[[1]]`},
		{`let networks=dynamic(["192.168.1.1/16","127.0.0.1/8","10.0.0.1/8"]);datatable(ip:string)["10.1.2.3","192.168.1.5","123.1.11.21","1.1.1.1"]|project b=ipv4_is_in_any_range(ip,networks)`, `[[1] [1] [0] [0]]`},
		{`let networks=dynamic(["a5e:f127:8a9d:146d:e102:b5d3:c755:f6cd/112","0:0:0:0:0:ffff:c0a8:ac/60"]);datatable(ip:string)["a5e:f127:8a9d:146d:e102:b5d3:c755:abcd","a5e:f127:8a9d:146d:e102:b5d3:c755:abce","a5e:f127:8a9d:146d:e102:b5d3:c755:abcf","a5e:f127:8a9d:146d:e102:b5d3:c756:abd1"]|project b=ipv6_is_in_any_range(ip,networks)`, `[[1] [1] [1] [0]]`},
		{`print b=ipv6_is_in_any_range("::1","2001::/16","::/64"),f=ipv6_is_in_any_range("::1",dynamic([])),n=ipv6_is_in_any_range("::1",dynamic(["::/0","bad"]))`, `[[1 0 <nil>]]`},
		{`print b=ipv4_is_in_any_range(dynamic("127.0.0.1"),"127.0.0.0/8"),c=ipv6_is_in_any_range(dynamic("::1"),"::/0")`, `[[1 1]]`},
		{`datatable(ip:string)["10.1.2.3","192.168.0.1","bad"]|where ipv4_is_in_any_range(ip,"10.0.0.0/8","127.0.0.0/8")|count`, `[[1]]`},
		{`print b=ipv6_is_in_any_range("::1","::/0")|getschema|project ColumnType`, `[[bool]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, name := range []string{"ipv4_is_in_any_range", "ipv6_is_in_any_range"} {
		for _, args := range []string{``, `"::1"`, `1,"::/0"`, `"::1",1`, `"::1",dynamic([]),"::/0"`} {
			if _, err := Compile("print b="+name+"("+args+")", time.Now()); err == nil {
				t.Fatalf("accepted invalid signature %s(%s)", name, args)
			}
		}
	}
	for _, tc := range []struct{ name, ip, miss, hit string }{
		{"ipv4_is_in_any_range", "127.0.0.1", "10.0.0.0/8", "127.0.0.0/8"},
		{"ipv6_is_in_any_range", "::1", "2001::/16", "::/0"},
	} {
		ranges := strings.Repeat(`"`+tc.miss+`",`, 256) + `"` + tc.hit + `"`
		for _, arg := range []string{ranges, "dynamic([" + ranges + "])"} {
			query := fmt.Sprintf(`print b=%s("%s",%s)`, tc.name, tc.ip, arg)
			if got := fmt.Sprint(featureRows(t, db, query)); got != "[[1]]" {
				t.Fatalf("257 ranges: %s", got)
			}
		}
	}
}
