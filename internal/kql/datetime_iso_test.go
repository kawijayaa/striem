package kql

import (
	"fmt"
	"testing"
)

func TestISOMinuteDatetimeExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print d=datetime(2017-10-15 12:00),r=reverse(datetime(2017-10-15 12:00))`, `[[2017-10-15T12:00:00.000000000Z Z0000000.00:00:21T51-01-7102]]`},
		{`print a=todatetime("2014-05-25T08:20"),b=todatetime("2014-11-08 15:55")`, `[[2014-05-25T08:20:00.000000000Z 2014-11-08T15:55:00.000000000Z]]`},
		{`print n=datetime_diff("hour",datetime(2017-10-31 01:00),datetime(2017-10-30 23:59))`, `[[2]]`},
		{`print d=datetime_add("minute",1,datetime(2024-02-29 23:59))`, `[[2024-03-01T00:00:00.000000000Z]]`},
		{`datatable(s:string)["2024-01-01T01:00+01:00","0001-01-01T00:00+01:00"]|project d=todatetime(s)`, `[[2024-01-01T00:00:00.000000000Z] [<nil>]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
}
