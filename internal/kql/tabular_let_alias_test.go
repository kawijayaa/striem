package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestDirectTabularLetAliases(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`let T=Events; let U=T; U | project user | order by user asc`, `[[alice] [bob] [carol]]`},
		{`let T=(LeftEvents); let ['T U']=((T)); ['T U'] | project user | order by user asc`, `[[alice] [bob]]`},
		{`let T=RightEvents; let U=T; U | project user`, `[[carol]]`},
		{`let T=LeftEvents; let U=T; U | union T | summarize n=count()`, `[[4]]`},
		{`let T=print d=parse_json('{"x":137}'),b=not(false); let U=T; U | project d.x,b`, `[[137 1]]`},
		{`let Events=139; let n=Events; print v=n`, `[[139]]`},
		{`let d=dynamic({"x":149}); let n=d.x; print v=n`, `[[149]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
	result, err := Compile(`let T=print d=dynamic([1]),b=not(false); let U=T; U`, time.Now(), featureCatalog())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.DynamicColumns["d"]; !ok {
		t.Fatal("dynamic metadata lost")
	}
	if _, ok := result.BooleanColumns["b"]; !ok {
		t.Fatal("Boolean metadata lost")
	}
	for _, query := range []string{`let T=NoSuchTable; T`, `let T=Events; let T=Events; T`, `let x=1; let x=Events; x`} {
		if result, err := Compile(query, time.Now(), featureCatalog()); err == nil || result.SQL != "" {
			t.Fatalf("accepted invalid binding: %s: %#v, %v", query, result, err)
		}
	}
}
