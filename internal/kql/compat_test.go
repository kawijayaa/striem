package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestNotCompatibility(t *testing.T) {
	db := featureDatabase(t)
	for _, c := range []struct{ query, want string }{
		{`Events | where not(n == 1) | project n | order by n asc`, `[[2] [3]]`},
		{`Events | where not(message has "powershell") | project n`, `[[2]]`},
		{`Events | where not(not(n == 1)) | project n`, `[[1]]`},
		{`Events | where not(n == 1 or n == 3) | project n`, `[[2]]`},
		{`Events | take 1 | project A=not(true), B=not(false), C=not(RawData.missing == 1)`, `[[0 1 <nil>]]`},
	} {
		t.Run(c.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, c.query)); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
	compiled, err := Compile(`Events | project Negated=not(true)`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := compiled.BooleanColumns["Negated"]; !ok {
		t.Fatal("not() result must retain boolean API metadata")
	}
}

func TestAdditionalHuntingFunctions(t *testing.T) {
	db := featureDatabase(t)
	for _, c := range []struct{ query, want string }{
		{`Events | summarize Users=dcount(user), Hosts=count_distinct(host), Chosen=dcountif(user,n>1), Average=avgif(n,n>1), Minimum=minif(n,n>1), Maximum=maxif(n,n<3)`, `[[3 2 2 2.5 2 2]]`},
		{`Events | where n < 0 | summarize N=dcount(host), Average=avgif(n,true), Minimum=minif(n,true), Maximum=maxif(n,true)`, `[[0 <nil> <nil> <nil>]]`},
		{`Events | summarize N=dcount(host,4), M=dcountif(host,n>1,0), Z=count_distinctif(host,false)`, `[[2 2 0]]`},
		{`Events | summarize N=count() by Bucket=bin(n,2) | order by Bucket asc`, `[[0 1] [2 2]]`},
		{`Events | summarize N=count() by Day=bin(TimeGenerated,1d) | order by Day asc`, `[[2026-01-01T00:00:00.000000000Z 1] [2026-01-02T00:00:00.000000000Z 1] [2026-01-03T00:00:00.000000000Z 1]]`},
		{`let size=1d; Events | summarize N=count() by Day=bin(TimeGenerated,size) | count`, `[[3]]`},
		{`Events | take 1 | project A=bin(-3,2), B=bin(4.5,1), C=bin(1,0), D=bin(9007199254740993,2)`, `[[-4 4 <nil> 9007199254740992]]`},
		{`Events | take 1 | project D=startofday(datetime("2024-02-29T12:30:00Z")), W=startofweek(datetime("2024-02-29")), M=endofmonth(datetime("2024-02-29")), Y=endofyear(datetime("2024-02-29"))`, `[[2024-02-29T00:00:00.000000000Z 2024-02-25T00:00:00.000000000Z 2024-02-29T23:59:59.999999900Z 2024-12-31T23:59:59.999999900Z]]`},
		{`Events | take 1 | project D=endofday(datetime("2024-02-29"),-1), W=endofweek(datetime("2024-02-29")), M=startofmonth(datetime("2024-01-31"),1), Y=startofyear(datetime("2024-02-29"),-1)`, `[[2024-02-28T23:59:59.999999900Z 2024-03-02T23:59:59.999999900Z 2024-02-01T00:00:00.000000000Z 2023-01-01T00:00:00.000000000Z]]`},
		{`Events | take 1 | project B=bin(datetime("1969-12-31T23:59:59.999999900Z"),1s), C=bin(datetime("2500-01-01T12:30:00Z"),1d)`, `[[1969-12-31T23:59:59.000000000Z 2500-01-01T00:00:00.000000000Z]]`},
	} {
		t.Run(c.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, c.query)); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestCompatibilityArityErrors(t *testing.T) {
	for _, expression := range []string{`not()`, `not(true,false)`, `not(1)`, `bin(1)`, `avgif(n)`, `dcount()`, `dcount(n,5)`, `dcount(n,0.5)`, `dcount(n,n)`, `count_distinct(n,2)`, `startofday()`, `endofweek(TimeGenerated,1,2)`} {
		if _, err := Compile(`Events | project X=`+expression, time.Now(), featureCatalog()); err == nil {
			t.Errorf("invalid call accepted: %s", expression)
		}
	}
}
