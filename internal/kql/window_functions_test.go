package kql

import (
	"fmt"
	"testing"
	"time"
)

// Hunting-style window queries over Striem's runtime, including datetime
// differences between adjacent sign-in events.
func TestWindowFunctionExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(User:string, Time:datetime)["alice",datetime(2024-01-01T10:00:00Z),"alice",datetime(2024-01-01T10:00:20Z),"alice",datetime(2024-01-01T12:00:00Z),"bob",datetime(2024-01-01T10:00:00Z)] | sort by User asc, Time asc | extend Gap=datetime_diff('second', Time, prev(Time)), Session=row_cumsum(iff(User != prev(User) or datetime_diff('second', Time, prev(Time)) > 600, 1, 0)) | project User, Gap, Session`,
			`[[alice <nil> 1] [alice 20 1] [alice 7180 2] [bob -7200 3]]`},
		{`datatable(Host:string, Count:long)["a",5,"b",9,"c",9,"d",1] | sort by Count desc, Host asc | extend Rank=row_rank_min(Count), Dense=row_rank_dense(Count), N=row_number()`,
			`[[b 9 1 1 1] [c 9 1 1 2] [a 5 3 2 3] [d 1 4 3 4]]`},
		{`datatable(n:long)[3,1,2] | sort by n asc | where n - prev(n, 1, 0) == 1 | project n`, `[[1] [2] [3]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	if _, err := Compile(`datatable(n:long)[1] | extend p=prev(n)`, time.Now()); err == nil {
		t.Fatal("window function over an unserialized table was accepted")
	}
}
