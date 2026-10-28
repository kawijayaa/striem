package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestUnixDatetimeExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print s=unixtime_seconds_todatetime(1546300800),ms=unixtime_milliseconds_todatetime(1546300800000),us=unixtime_microseconds_todatetime(1546300800000000),ns=unixtime_nanoseconds_todatetime(1546300800000000000)`, `[[2019-01-01T00:00:00.000000000Z 2019-01-01T00:00:00.000000000Z 2019-01-01T00:00:00.000000000Z 2019-01-01T00:00:00.000000000Z]]`},
		{`print ns=unixtime_nanoseconds_todatetime(1546300800123456700),us=unixtime_microseconds_todatetime(1546300800123456),f=unixtime_seconds_todatetime(0.1234567),negative=unixtime_seconds_todatetime(-0.1)`, `[[2019-01-01T00:00:00.123456700Z 2019-01-01T00:00:00.123456000Z 1970-01-01T00:00:00.123456700Z 1969-12-31T23:59:59.900000000Z]]`},
		{`datatable(s:long)[-1,0,1]|project d=unixtime_seconds_todatetime(s)`, `[[1969-12-31T23:59:59.000000000Z] [1970-01-01T00:00:00.000000000Z] [1970-01-01T00:00:01.000000000Z]]`},
		{`print d=unixtime_seconds_todatetime(long(null)),bad=unixtime_seconds_todatetime(long(9223372036854775807))`, `[[<nil> <nil>]]`},
		{`let f=(n:long){unixtime_nanoseconds_todatetime(n)};print d=f(100)`, `[[1970-01-01T00:00:00.000000100Z]]`},
		{`print d=unixtime_seconds_todatetime(0)|getschema|project ColumnType`, `[[datetime]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, call := range []string{`unixtime_seconds_todatetime()`, `unixtime_milliseconds_todatetime("1")`, `unixtime_microseconds_todatetime(true)`, `unixtime_nanoseconds_todatetime(1,2)`} {
		if _, err := Compile("print d="+call, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", call)
		}
	}
}
