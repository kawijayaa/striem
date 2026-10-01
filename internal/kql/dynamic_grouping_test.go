package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestDynamicGroupingRequiresExplicitCasts(t *testing.T) {
	for _, query := range []string{
		`Events | summarize n=count() by RawData.user`,
		`Events | distinct RawData.user`,
		`Events | distinct *`,
		`print d=dynamic([1]) | sample-distinct 1 of d`,
		`print x=1 | extend parse_json("{bad}") | distinct Column1`,
	} {
		if _, err := Compile(query, time.Now(), featureCatalog()); err == nil || !strings.Contains(err.Error(), "dynamic column") {
			t.Fatalf("expected type error: %s: %v", query, err)
		}
	}
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`Events | distinct tostring(RawData.user) | order by RawData_user asc`, `[[alice] [bob] [carol]]`},
		{`Events | summarize n=count() by user=tostring(RawData.user) | order by user asc`, `[[alice 1] [bob 1] [carol 1]]`},
		{`Events | project user=tostring(RawData.user) | sample-distinct 5 of user | order by user asc`, `[[alice] [bob] [carol]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s: got=%s want=%s", tc.query, got, tc.want)
		}
	}
}
