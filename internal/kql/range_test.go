package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestNumericRangeExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`range x from 1 to 8 step 3`, `[[1] [4] [7]]`},
		{`range x from 8 to 1 step -3`, `[[8] [5] [2]]`},
		{`range x from 9007199254740992 to 9007199254740994 step 1`, `[[9007199254740992] [9007199254740993] [9007199254740994]]`},
		{`range x from 1 to 2 step 0.5`, `[[1] [1.5] [2]]`},
		{`range x from 1 to 9223372036854775807 step 1 | take 3`, `[[1] [2] [3]]`},
		{`range x from 1 to 5 step 1 | summarize variance(x),stdevp(x)`, `[[2.5 1.4142135623730951]]`},
		{`let R=range x from 1 to 3 step 1; R | where not(x==1) | count`, `[[2]]`},
		{`range n from 1 to 3 step 1 | join kind=inner (Events) on n | project n,user | order by n asc`, `[[1 alice] [2 bob] [3 carol]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
	for _, query := range []string{`range x from "1" to 3 step 1`, `range x from datetime("2024-01-01") to datetime("2024-01-02") step 1d`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Errorf("accepted unsupported range: %s", query)
		}
	}
}
