package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestIPv4ConversionExecutionAndTypes(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(ip:string)["192.168.1.1","192.168.1.1/24","255.255.255.255/31"]|project n=parse_ipv4(ip)`, `[[3232235777] [3232235776] [4294967294]]`},
		{`datatable(ip:string,p:long)["192.168.1.1",24,"192.168.1.1",32,"192.168.1.1/24",32,"192.168.1.1/24",-1]|project a=format_ipv4(ip,p),b=format_ipv4_mask(ip,p)`, `[[192.168.1.0 192.168.1.0/24] [192.168.1.1 192.168.1.1/32] [192.168.1.0 192.168.1.0/24] [ ]]`},
		{`print x=parse_ipv4_mask("127.0.0.1",24)`, `[[2130706432]]`},
		{`datatable(ip:string)["10.1.2.3","192.168.1.1/24","127.0.0.1/16"]|project n=ipv4_netmask_suffix(ip)`, `[[32] [24] [16]]`},
		{`print a=format_ipv4(4294967295),b=format_ipv4_mask(0),c=parse_ipv4("bad"),d=format_ipv4(-1)`, `[[255.255.255.255 0.0.0.0/32 <nil> ]]`},
		{`let mask=24;datatable(ip:string)["192.168.1.1","192.168.1.254"]|summarize n=count() by network=format_ipv4_mask(ip,mask)`, `[[192.168.1.0/24 2]]`},
		{`print a=parse_ipv4("255.255.255.255")+1,b=parse_ipv4_mask("1.2.3.4",long(null)),c=format_ipv4("1.2.3.4",long(null))`, `[[4294967296 <nil> ]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print a=parse_ipv4("1.2.3.4"),b=format_ipv4(1),c=ipv4_netmask_suffix("1.2.3.4/24")|getschema`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(compiled.SQL, compiled.Args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var types []string
	for rows.Next() {
		var name, dataType, columnType string
		var ordinal int
		if err := rows.Scan(&name, &ordinal, &dataType, &columnType); err != nil {
			t.Fatal(err)
		}
		types = append(types, columnType)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if strings.Join(types, ",") != "long,string,long" {
		t.Fatal(types)
	}
}

func TestIPv4ConversionDiagnostics(t *testing.T) {
	for _, expr := range []string{`parse_ipv4()`, `parse_ipv4(1)`, `parse_ipv4("1.2.3.4",24)`, `parse_ipv4_mask("1.2.3.4")`, `parse_ipv4_mask("1.2.3.4",1.5)`, `format_ipv4()`, `format_ipv4(1,24,32)`, `format_ipv4(true)`, `format_ipv4(1.0)`, `format_ipv4(dynamic("1.2.3.4"))`, `format_ipv4_mask(1,"24")`, `ipv4_netmask_suffix(1)`} {
		if _, err := Compile("print x="+expr, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", expr)
		}
	}
}

func TestIPv4ComparisonExecutionAndTypes(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(a:string,b:string)["192.168.1.0","192.168.1.0","192.168.1.1/24","192.168.1.255","192.168.1.1","192.168.1.255/24","192.168.1.1/30","192.168.1.255/24"]|project c=ipv4_compare(a,b),m=ipv4_is_match(a,b)`, `[[0 1] [0 1] [0 1] [0 1]]`},
		{`datatable(a:string,b:string,p:long)["192.168.1.1","192.168.1.0",31,"192.168.1.1/24","192.168.1.255",31,"192.168.1.1","192.168.1.255",24]|project c=ipv4_compare(a,b,p),m=ipv4_is_match(a,b,p)`, `[[0 1] [0 1] [0 1]]`},
		{`print c=ipv4_compare("255.255.255.255","0.0.0.0"),m=ipv4_is_match("1.2.3.4","1.2.3.5"),n=ipv4_is_match("bad","1.2.3.4"),p=ipv4_is_match("1.2.3.4","1.2.3.4",long(null))`, `[[1 0 <nil> <nil>]]`},
		{`datatable(ip:string)["10.1.2.3","10.2.3.4","11.1.2.3","bad"]|where ipv4_is_match(ip,"10.0.0.0/8")|count`, `[[2]]`},
		{`let f=(ip:string){ipv4_is_match(ip,"10.0.0.0/8")};print m=f("10.2.3.4")`, `[[1]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print c=ipv4_compare("1.2.3.4","1.2.3.5"),m=ipv4_is_match("1.2.3.4","1.2.3.5")`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.BooleanColumns["m"]; !ok {
		t.Fatal("match result lost Boolean metadata")
	}
	if _, ok := compiled.BooleanColumns["c"]; ok {
		t.Fatal("comparison mislabeled Boolean")
	}
	for _, expr := range []string{`ipv4_compare()`, `ipv4_compare("1.2.3.4")`, `ipv4_compare("1.2.3.4","1.2.3.4",24,24)`, `ipv4_compare(1,"1.2.3.4")`, `ipv4_is_match("1.2.3.4",true)`, `ipv4_is_match("1.2.3.4","1.2.3.4",1.5)`} {
		if _, err := Compile("print x="+expr, time.Now()); err == nil {
			t.Fatalf("invalid comparison signature accepted: %s", expr)
		}
	}
}

func TestIPv4RangeCIDRExecutionAndTypes(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print start_IP="1.1.128.0",end_IP="1.1.140.255"|project ranges=ipv4_range_to_cidr_list(start_IP,end_IP)`, `[[["1.1.128.0/21","1.1.136.0/22","1.1.140.0/24"]]]`},
		{`print ranges=ipv4_range_to_cidr_list("0.0.0.0","255.255.255.255")|project n=array_length(ranges),first=ranges[0]`, `[[1 0.0.0.0/0]]`},
		{`datatable(a:string,b:string)["192.168.1.1","192.168.1.6","10.0.0.0","10.255.255.255"]|extend ranges=ipv4_range_to_cidr_list(a,b)|mv-expand ranges|project ranges`, `[[192.168.1.1/32] [192.168.1.2/31] [192.168.1.4/31] [192.168.1.6/32] [10.0.0.0/8]]`},
		{`print x=ipv4_range_to_cidr_list("bad","1.2.3.4"),y=ipv4_range_to_cidr_list("1.2.3.5","1.2.3.4")`, `[[<nil> <nil>]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print ranges=ipv4_range_to_cidr_list("1.2.3.4","1.2.3.5")`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["ranges"]; !ok {
		t.Fatal("CIDR list lost dynamic metadata")
	}
	for _, expression := range []string{`ipv4_range_to_cidr_list()`, `ipv4_range_to_cidr_list("1.2.3.4")`, `ipv4_range_to_cidr_list("1.2.3.4","1.2.3.5",1)`, `ipv4_range_to_cidr_list(1,"1.2.3.5")`, `ipv4_range_to_cidr_list("1.2.3.4",false)`} {
		if _, err := Compile("print x="+expression, time.Now()); err == nil {
			t.Fatalf("invalid range signature accepted: %s", expression)
		}
	}
}
