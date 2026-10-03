package compiler_test

import (
	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
	"testing"
)

func TestProjectionWidthLimits(t *testing.T) {
	compiler := ksql.New(dialect.SQLite(), ksql.WithLimits(ksql.Limits{MaxProjectionItems: 2}))
	for _, query := range []string{
		`print a=1,b=2,c=3`,
		`print a=1 | extend b=2 | extend c=3`,
		`print a=1,b=2 | extend 3`,
		`print a=1,b=2 | project *,c=3`,
		`print a=1,b=2 | project c=3,*`,
		`print a=1,b=2 | distinct *,3`,
		`datatable(g:long,v:long)[1,2] | summarize total=sum(v),n=count() by g`,
	} {
		result := compiler.Compile(query)
		if result.OK() || result.SQL != "" {
			t.Fatalf("accepted %s: %#v", query, result)
		}
		found := false
		for _, d := range result.Diagnostics {
			if d.Code == "KQLB0344" && d.Span.Length > 0 {
				found = true
			}
		}
		if !found {
			t.Fatalf("no positioned width diagnostic for %s: %#v", query, result.Diagnostics)
		}
	}
	for _, query := range []string{
		`print a=1 | extend b=2 | extend a=3 | project a,b`,
		`print a=1 | extend 2 | project a,Column1`,
		`print a=1,b=2 | project *`,
		`datatable(a:long,b:long,c:long)[1,2,3] | project a,b`,
		`datatable(g:long,v:long)[1,2] | summarize total=sum(v) by g`,
	} {
		if result := compiler.Compile(query); !result.OK() {
			t.Fatalf("rejected %s: %#v", query, result.Diagnostics)
		}
	}
	unlimited := ksql.New(dialect.SQLite()).Compile(`print a=1,b=2,c=3 | extend d=4`)
	if !unlimited.OK() || len(unlimited.Columns) != 4 {
		t.Fatal(unlimited)
	}
}
