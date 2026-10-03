package compiler_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kawijayaa/ksql"
	"github.com/kawijayaa/ksql/dialect"
)

func TestQuotedJoinKeysAndLookupOutput(t *testing.T) {
	db := openInvestigationDB(t)
	left := `datatable(['left key']:string,v:long)["a",1,"a",2,"b",3]`
	right := `datatable(['right key']:string,r:long)["a",5,"a",6,"c",7]`
	for _, mode := range []ksql.ParameterizationMode{ksql.LiteralParameters, ksql.BoundParameters} {
		compiler := ksql.New(dialect.SQLite(), ksql.WithParameterization(mode))
		for _, tc := range []struct {
			kind  string
			count int
		}{{"inner", 4}, {"innerunique", 2}, {"leftouter", 5}, {"rightouter", 5}, {"fullouter", 6}, {"leftsemi", 2}, {"rightsemi", 2}, {"leftanti", 1}, {"rightanti", 1}} {
			query := left + ` | join kind=` + tc.kind + ` (` + right + `) on $left.['left key'] == $right.["right key"] | count`
			result := compiler.Compile(query)
			if got := fmt.Sprint(queryRows(t, db, result)); got != fmt.Sprintf("[[%d]]", tc.count) {
				t.Fatalf("%s: got=%s", query, got)
			}
		}
		for _, tc := range []struct{ query, want string }{
			{`datatable(['a.b']:long)[1] | join (datatable(['a.b']:long)[1]) on ['a.b'] | count`, `[[1]]`},
			{`datatable(['a.b']:long)[1] | join (datatable(['a.b']:long)[1]) on $left.['a' '.b'] == $right.['a.b'] | count`, `[[1]]`},
			{`datatable(a:int)[1] | join kind=inner (datatable(b:long)[1]) on $left.a==$right.b | count`, `[[1]]`},
			{`print a=1,b=2 | join (print a=1,b=3) on a and b | count`, `[[0]]`},
			{left + ` | join kind=inner (` + right + `) where $left.['left key'] == $right.['right key'] and $left.v<$right.r | count`, `[[4]]`},
			{left + ` | join kind=inner (` + right + `) where $left.v>$right.r | count`, `[[0]]`},
			{`print ['a.b']=1,v="left" | lookup (print ['right key']=1,v="right") on $left.['a.b']==$right.['right key'] | project ['a.b'],v,v1`, `[[1 left right]]`},
			{`print k=1,v="left" | lookup kind=inner (print k=1,v="right",d=dynamic([true]),b=not(false)) on k | project v,v1,d,b`, `[[left right [true] 1]]`},
			{`print k=1,k2=2 | lookup (print key=1,key2=2,v="matched") on $left.k==$right.key and $left.k2==$right.key2`, `[[1 2 matched]]`},
			{`print k=2,v="left" | lookup (print k=1,v="right") on k | project v,v1`, `[[left <nil>]]`},
			{`print d=dynamic({"key":1}) | extend k=1+0 | join kind=inner (print k=1) on k | count`, `[[1]]`},
		} {
			result := compiler.Compile(tc.query)
			if got := fmt.Sprint(queryRows(t, db, result)); got != tc.want {
				t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
			}
		}
		result := compiler.Compile(`print ['left key']=1,v=2 | lookup (print ['right key']=1,v=3) on $left.['left key']==$right.['right key']`)
		if !result.OK() || len(result.Columns) != 3 || result.Columns[2].Name != "v1" {
			t.Fatalf("lookup removed non-key columns or kept right keys: %#v", result)
		}
	}
}

func TestJoinKeyContracts(t *testing.T) {
	compiler := ksql.New(dialect.SQLite())
	for _, query := range []string{
		`print k=1 | join kind=inner (print k=1) on $right.k==$left.k`,
		`print k=1 | join kind=inner (print k=1) on $left.k==$left.k`,
		`print k=1 | join kind=inner (print k=1) on k==k`,
		`print k=1 | join kind=inner (print k=1) on $left.k>$right.k`,
		`print k=1 | join kind=inner (print k=1) on $left.k==$right.k or $left.k==$right.k`,
		`print k=1 | join kind=inner (print k=1) on $left.k+1==$right.k`,
		`print k=1 | join kind=inner (print k=1) on tostring($left.k)==tostring($right.k)`,
		`print d=dynamic({"k":1}) | join kind=inner (print d=dynamic({"k":1})) on $left.d.k==$right.d.k`,
		`print d=dynamic([1]) | join kind=inner (print d=dynamic([1])) on $left.d==$right.d`,
		`print k=1 | join kind=inner (print k="a") on $left.k==$right.k`,
		`print k=1 | join kind=inner (print k=1) on $left.['missing']==$right.['k']`,
		`print k=1 | join kind=inner (print k=1) on $left["k"]==$right["k"]`,
		`let name="k"; print k=1 | join kind=inner (print k=1) on $left.[name]==$right.[name]`,
		`print k=1 | join kind=inner (print k=1) on`,
		`print k=1 | join kind=inner (print k=1) on ,`,
		`print k=1 | join kind=inner (print k=1) where`,
		`print k=1 | join kind=inner (print k=1) where $left.k ==`,
		`print k=1 | join kind=inner (print k=1) where 1`,
		`print k=1 | lookup kind=rightsemi (print k=1) on k`,
		`print k=1 | lookup kind=innerunique (print k=1) on k`,
		`print k=1 | lookup (print k=1) where $left.k==$right.k`,
		`Unknown | lookup (Other) on k`,
	} {
		result := compiler.Compile(query)
		if result.OK() || result.SQL != "" || len(result.Diagnostics) == 0 || result.Diagnostics[0].Span.Line < 1 {
			t.Fatalf("accepted invalid join %s: %#v", query, result)
		}
	}
	limited := ksql.New(dialect.SQLite(), ksql.WithLimits(ksql.Limits{MaxJoinKeys: 1}))
	for _, condition := range []string{`a,b`, `a and b`, `$left.a==$right.a and $left.b==$right.b`} {
		result := limited.Compile(`print a=1,b=2 | join (print a=1,b=2) on ` + condition)
		if result.OK() || result.SQL != "" || !strings.Contains(fmt.Sprint(result.Diagnostics), "MaxJoinKeys") {
			t.Fatalf("key limit bypassed: %v", result.Diagnostics)
		}
	}
	widthLimited := ksql.New(dialect.SQLite(), ksql.WithLimits(ksql.Limits{MaxProjectionItems: 1}))
	result := widthLimited.Compile(`print k=1 | join (print k=1) on k`)
	if result.OK() || result.SQL != "" || !strings.Contains(fmt.Sprint(result.Diagnostics), "MaxProjectionItems") {
		t.Fatalf("join output bypassed width limit: %#v", result)
	}
	result = widthLimited.Compile(`print k=1 | lookup (print k=1) on k`)
	if !result.OK() || len(result.Columns) != 1 {
		t.Fatalf("lookup key counted as an output: %#v", result)
	}
}
