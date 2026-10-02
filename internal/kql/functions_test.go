package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestScalarQueryFunctionsExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`let eligible=(name:string){name has "alice"}; Events | where eligible(user) | project n`, `[[1]]`},
		{`let getQueryParamValue=(querystring:string,param:string){let params=parse_urlquery(querystring); tostring(params["Query Parameters"].[param])}; print result=getQueryParamValue("view=vs-2019&preserve-view=true","view")`, `[[vs-2019]]`},
		{`let find=(input:string){extract_all("([0-9]+)",input)}; print a=find("a1 b2"),b=find("c3") | project last=a[-1],first=b[0]`, `[[2 3]]`},
		{`let f=(x:dynamic){bag_pack("value",x)}; print result=f(dynamic([1,true,null]))`, `[[{"value":[1,true,null]}]]`},
		{`let echo=(text:string){text}; print result=echo("x' OR 1=1 --")`, `[[x' OR 1=1 --]]`},
		{`let f=(a:long,b:string="b.default",c:long=0){strcat(a,"-",b,"-",c)}; print a=f(c=1,a=12,b="b"),b=f(12,c=1,b="b")`, `[[12-b-1 12-b-1]]`},
		{`let f=(x:datetime=datetime("2026-01-01")){x}; print result=f()`, `[[2026-01-01T00:00:00.000000000Z]]`},
		{`let f=(a:long){let add=(b:long){a+b}; add(1)}; Events | project result=f(n) | order by result asc`, `[[2] [3] [4]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`let wrap=(x:bool){bag_pack("value",x)}; let invert=(x:bool){not(x)}; print d=wrap(true),b=invert(false) | project renamed=d,b`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["renamed"]; !ok {
		t.Fatal("lost dynamic function return metadata")
	}
	if _, ok := compiled.BooleanColumns["b"]; !ok {
		t.Fatal("lost Boolean function return metadata")
	}
}

func TestFunctionDiagnosticsAndExpansionLimits(t *testing.T) {
	for _, query := range []string{
		`let f=(x:long){x}; print result=f("bad")`,
		`let f=(){user}; Events | project f()`,
		`let f=(){let hidden=3; hidden}; print hidden`,
		`let f=(x:long){x}; print f(x=)`,
		`let f=(x:long){x+x}; print ` + strings.Repeat("f(", 40) + "1" + strings.Repeat(")", 40),
	} {
		if _, err := Compile(query, time.Now(), featureCatalog()); err == nil {
			t.Fatalf("accepted %s", query)
		}
	}
}
