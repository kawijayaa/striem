package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestDatetimeDiffExecution(t *testing.T) {
	db := featureDatabase(t)
	// The documented minute-precision forms exercise shared ISO normalization.
	for _, tc := range []struct {
		period, left, right string
		want                int64
	}{
		{"year", "2017-01-01", "2000-12-31", 17},
		{"quarter", "2017-07-01", "2017-03-30", 2},
		{"month", "2017-01-01", "2015-12-30", 13},
		{"week", "2017-10-29 00:00", "2017-09-30 23:59", 5},
		{"day", "2017-10-29 00:00", "2017-09-30 23:59", 29},
		{"hour", "2017-10-31 01:00", "2017-10-30 23:59", 2},
		{"minute", "2017-10-30T23:05:01Z", "2017-10-30T23:00:59Z", 5},
		{"second", "2017-10-30T23:00:10.100Z", "2017-10-30T23:00:00.900Z", 10},
		{"millisecond", "2017-10-30T23:00:00.200100Z", "2017-10-30T23:00:00.100900Z", 100},
		{"microsecond", "2017-10-30T23:00:00.1009001Z", "2017-10-30T23:00:00.1008009Z", 100},
		{"nanosecond", "2017-10-30T23:00:00.0000000Z", "2017-10-30T23:00:00.0000007Z", -700},
	} {
		query := fmt.Sprintf(`print d=datetime_diff("%s",datetime("%s"),datetime("%s"))`, tc.period, tc.left, tc.right)
		if got := fmt.Sprint(featureRows(t, db, query)); got != fmt.Sprintf("[[%d]]", tc.want) {
			t.Fatalf("%s got %s want %d", query, got, tc.want)
		}
	}
	for _, tc := range []struct{ query, want string }{
		{`let f=(d:datetime){datetime_diff("day",d,make_datetime(2024,1,1))};print d=f(make_datetime(2024,1,2))`, `[[1]]`},
		{`datatable(y:long)[2023,2024]|extend d=make_datetime(y,1,1)|project n=datetime_diff("year",d,make_datetime(2020,1,1))`, `[[3] [4]]`},
		{`print n=datetime_diff("day",datetime(null),make_datetime(2024,1,1))`, `[[<nil>]]`},
		{`print d=datetime_diff("second",make_datetime(2024,1,1),make_datetime(2024,1,1))|getschema|project ColumnType`, `[[long]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, call := range []string{`datetime_diff("day",datetime(2024-01-01))`, `datetime_diff("day","2024-01-01",datetime(2024-01-01))`, `datetime_diff(1,datetime(2024-01-01),datetime(2024-01-02))`} {
		if _, err := Compile("print d="+call, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", call)
		}
	}
}
