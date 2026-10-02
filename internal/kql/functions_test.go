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

func TestTabularQueryFunctionsExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`let f=(T:(n:long),minimum:long=2){T|where n>=minimum}; LeftEvents | invoke f() | project n,user`, `[[2 bob]]`},
		{`let f=(T:(n:long)){T}; f(LeftEvents) | project n,user | order by n asc`, `[[1 alice] [2 bob]]`},
		{`let T=LeftEvents; let f=(){T}; let g=(){let T=RightEvents; f()}; g() | project n`, `[[1] [2]]`},
		{`let f=(A:(host:string),B:(host:string,user:string)){A|join kind=inner (B|project host,Peer=user) on host}; f(LeftEvents,RightEvents) | project user,Peer`, `[[alice carol]]`},
		{`let f=(T:(n:long)){T|where n>99}; f(LeftEvents) | count`, `[[0]]`},
		{`let f=(T:(*)){T|count}; RightEvents | invoke f()`, `[[1]]`},
		{`let f=(T:(n:long),v:long){T|where n==v}; f((LeftEvents|where n>1),2) | project user`, `[[bob]]`},
		{`let f=(T:(items:dynamic)){T|extend b=not(false)}; f(RightEvents) | project items,b`, `[[[] 1]]`},
		{`let f=(T:(n:long)){let g=(U:(n:long)){U|extend doubled=n*2}; g(T)}; LeftEvents | invoke f() | project doubled`, `[[2] [4]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`let f=(T:(d:dynamic,b:bool)){T}; print d=dynamic({"n":2}),b=not(false) | invoke f() | project renamed=d,b`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["renamed"]; !ok {
		t.Fatal("lost dynamic table return metadata")
	}
	if _, ok := compiled.BooleanColumns["b"]; !ok {
		t.Fatal("lost Boolean table return metadata")
	}
}

func TestQuotedColumnDeclarationsExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`LeftEvents | project ['some number']=n,['a.b']=user | where ['some number']==2 | project-rename ['user name']=['a.b'] | project ['user name']`, `[[bob]]`},
		{`LeftEvents | extend ['some number']=n*2 | project ['some number'] | order by ['some number'] asc`, `[[2] [4]]`},
		{`datatable(['some number']:long)[3,5] | summarize ['row count']=count() by ['some number'] | order by ['some number'] asc`, `[[3 1] [5 1]]`},
		{`let f=(T:(['some number']:long)){T}; f(datatable(['some' ' number']:long)[7])`, `[[7]]`},
		{`let ['some' ' text']="hello"; print ['result name']=['some text']`, `[[hello]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print ['a.b']=dynamic([1]),['bool value']=not(false) | project-rename ['renamed data']=['a.b'] | project ['renamed data'],['bool value']`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.DynamicColumns["renamed data"]; !ok {
		t.Fatal("lost quoted dynamic output metadata")
	}
	if _, ok := compiled.BooleanColumns["bool value"]; !ok {
		t.Fatal("lost quoted Boolean output metadata")
	}
}

func TestQuotedJoinKeysAndLookupExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`LeftEvents | project ['left host']=host,user | join kind=inner (RightEvents|project ['right host']=host,Peer=user) on $left.['left host']==$right.['right host'] | project user,Peer`, `[[alice carol]]`},
		{`LeftEvents | project ['a.b']=host,user | join (RightEvents|project ['a.b']=host,Peer=user) on ['a.b'] | project user,Peer`, `[[alice carol]]`},
		{`LeftEvents | lookup kind=inner (RightEvents) on host | project user,user1`, `[[alice carol]]`},
		{`LeftEvents | lookup (RightEvents|project RightHost=host,user) on $left.host==$right.RightHost | project host,user,user1 | order by user asc`, `[[a alice carol] [b bob <nil>]]`},
		{`print d=dynamic({"key":1}) | extend k=tolong(d.key) | join kind=inner (print k=1) on k | count`, `[[1]]`},
		{`LeftEvents | join kind=inner (RightEvents) where $left.['host']==$right.['host'] and $left.n<$right.n | project user,user1`, `[[alice carol]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	compiled, err := Compile(`print k=1,d=dynamic([1]),b=not(false) | lookup (print RightKey=1,d=dynamic([2]),b=not(true)) on $left.k==$right.RightKey | project d,d1,b,b1`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"d", "d1"} {
		if _, ok := compiled.DynamicColumns[column]; !ok {
			t.Fatal("lost lookup dynamic metadata: " + column)
		}
	}
	for _, column := range []string{"b", "b1"} {
		if _, ok := compiled.BooleanColumns[column]; !ok {
			t.Fatal("lost lookup Boolean metadata: " + column)
		}
	}
}
