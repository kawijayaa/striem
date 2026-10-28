package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestDatetimeConstructionAndAddition(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print d=make_datetime(2017,10,1),m=make_datetime(2017,10,1,12,10),s=make_datetime(2017,10,1,12,11,0.1234567)`, `[[2017-10-01T00:00:00.000000000Z 2017-10-01T12:10:00.000000000Z 2017-10-01T12:11:00.123456700Z]]`},
		{`let d=make_datetime(2017,1,1);print y=datetime_add("year",1,d),q=datetime_add("quarter",1,d),m=datetime_add("month",1,d),w=datetime_add("week",1,d),day=datetime_add("day",1,d),h=datetime_add("hour",1,d),min=datetime_add("minute",1,d),s=datetime_add("second",1,d)`, `[[2018-01-01T00:00:00.000000000Z 2017-04-01T00:00:00.000000000Z 2017-02-01T00:00:00.000000000Z 2017-01-08T00:00:00.000000000Z 2017-01-02T00:00:00.000000000Z 2017-01-01T01:00:00.000000000Z 2017-01-01T00:01:00.000000000Z 2017-01-01T00:00:01.000000000Z]]`},
		{`let d=make_datetime(2017,1,1);print y=datetime_add("year",-5,d),q=datetime_add("quarter",12,d),m=datetime_add("month",-15,d),w=datetime_add("week",100,d)`, `[[2012-01-01T00:00:00.000000000Z 2020-01-01T00:00:00.000000000Z 2015-10-01T00:00:00.000000000Z 2018-12-02T00:00:00.000000000Z]]`},
		{`print m=datetime_add("month",1,make_datetime(2024,1,31)),y=datetime_add("year",1,make_datetime(2024,2,29)),bad=make_datetime(2023,2,29)`, `[[2024-02-29T00:00:00.000000000Z 2025-02-28T00:00:00.000000000Z <nil>]]`},
		{`datatable(y:long)[2023,2024]|project d=make_datetime(y,2,29)`, `[[<nil>] [2024-02-29T00:00:00.000000000Z]]`},
		{`print d=make_datetime(2024,1,1)|where datetime_add("day",1,d)>d|count`, `[[1]]`},
		{`print d=make_datetime(2024,1,1),a=datetime_add("day",1,make_datetime(2024,1,1))|getschema|project ColumnType`, `[[datetime] [datetime]]`},
		{`let f=(n:long){datetime_add("day",n,make_datetime(2024,1,1))};print d=f(1)`, `[[2024-01-02T00:00:00.000000000Z]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, call := range []string{`make_datetime(2024,1)`, `make_datetime(2024,1,1.5)`, `make_datetime(2024,1,1,0,0,0,0)`, `datetime_add("day",1,"2024-01-01")`, `datetime_add(1,1,datetime(2024-01-01))`, `datetime_add("day",1.5,datetime(2024-01-01))`} {
		if _, err := Compile("print d="+call, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", call)
		}
	}
}
