package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestExtractAllExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, name := range []string{"extract_all", "extractall"} {
		for _, tc := range []struct{ args, want string }{
			{`@"(\d+)","numbers: 123, 567 and 789"`, `[[["123","567","789"]]]`},
			{`@"([\da-f]{2})","82b8be2d-dfa7-4bd1-8f63-24ad26d31449"`, `[[["82","b8","be","2d","df","a7","4b","d1","8f","63","24","ad","26","d3","14","49"]]]`},
			{`@"(\w)(\w+)(\w)","82b8be2d-dfa7-4bd1-8f63-24ad26d31449"`, `[[[["8","2b8be2","d"],["d","fa","7"],["4","bd","1"],["8","f6","3"],["2","4ad26d3144","9"]]]]`},
			{`@"(\w)(\w+)(\w)",dynamic([1,3]),"82b8be2d-dfa7-4bd1-8f63-24ad26d31449"`, `[[[["8","d"],["d","7"],["4","1"],["8","3"],["2","9"]]]]`},
			{`@"(?P<first>\w)(?P<middle>\w+)(?P<last>\w)",dynamic(["first",2,"last"]),"82b8be2d-dfa7-4bd1-8f63-24ad26d31449"`, `[[[["8","2b8be2","d"],["d","fa","7"],["4","bd","1"],["8","f6","3"],["2","4ad26d3144","9"]]]]`},
			{`"(a)(b)",dynamic([2]),"ab ab"`, `[[["b","b"]]]`},
			{`"(a)","none"`, `[[<nil>]]`},
		} {
			query := `print d=` + name + `(` + tc.args + `)`
			if got := fmt.Sprint(featureRows(t, db, query)); got != tc.want {
				t.Fatalf("%s: got=%s want=%s", query, got, tc.want)
			}
		}
		for _, args := range []string{``, `"(a)"`, `"(a)","a",1,2`, `1,"a"`, `"(a)",true`, `"(a)","[1]","a"`, `"a","a"`, `"(","a"`} {
			if _, err := Compile(`print `+name+`(`+args+`)`, time.Now()); err == nil {
				t.Errorf("accepted invalid %s(%s)", name, args)
			}
		}
		query := `datatable(source:string,groups:dynamic)["a1 a2",dynamic([2])] | extend d=` + name + `("(a)([0-9])",groups,source) | project last=d[-1],n=array_length(d)`
		if got := fmt.Sprint(featureRows(t, db, query)); got != "[[2 2]]" {
			t.Fatal(got)
		}
		compiled, err := Compile(`print raw="a1" | project `+name+`("(a)",raw)`, time.Now())
		if err != nil || fmt.Sprint(compiled.Columns) != "[Column1]" {
			t.Fatalf("generated name: %v %v", compiled, err)
		}
		if _, ok := compiled.DynamicColumns["Column1"]; !ok {
			t.Fatal("lost dynamic metadata")
		}
		if _, err := Compile(`print pattern="(a)" | project `+name+`(pattern,"a")`, time.Now()); err == nil {
			t.Fatal("accepted non-constant pattern")
		}
		if _, err := Compile(`print `+name+`("`+strings.Repeat("(a)", 17)+`","a")`, time.Now()); err == nil {
			t.Fatal("accepted more than 16 captures")
		}
	}
	query := `datatable(id:long,raw:string)[1,"a"] | union (datatable(id:long)[2]) | where id == 2 | project d=extract_all("(a)",raw)`
	if got := fmt.Sprint(featureRows(t, db, query)); got != "[[<nil>]]" {
		t.Fatal(got)
	}
}
