package kql

import (
	"fmt"
	"testing"
	"time"
)

func TestDatetimePartExecution(t *testing.T) {
	db := featureDatabase(t)
	query := `let dt=datetime(2017-10-30 01:02:03.7654321);print year=datetime_part("year",dt),quarter=datetime_part("quarter",dt),month=datetime_part("month",dt),week=datetime_part("week_of_year",dt),day=datetime_part("day",dt),dayofyear=datetime_part("dayOfYear",dt),hour=datetime_part("hour",dt),minute=datetime_part("minute",dt),second=datetime_part("second",dt),ms=datetime_part("millisecond",dt),us=datetime_part("microsecond",dt),ns=datetime_part("nanosecond",dt)`
	if got := fmt.Sprint(featureRows(t, db, query)); got != "[[2017 4 10 44 30 303 1 2 3 765 765432 765432100]]" {
		t.Fatalf("documented parts: %s", got)
	}
	for _, tc := range []struct{ query, want string }{
		{`print a=week_of_year(datetime(2020-12-31)),b=week_of_year(datetime(2020-06-15)),c=week_of_year(datetime(1970-01-01)),d=week_of_year(datetime(2000-01-01))`, `[[53 25 1 52]]`},
		{`let d=make_datetime(2024,2,29,12,30);print y=getyear(d),m=getmonth(d),mm=monthofyear(d),day=dayofmonth(d),dy=dayofyear(d),h=hourofday(d),p=datepart("quarter",d)`, `[[2024 2 2 29 60 12 1]]`},
		{`datatable(y:long)[2023,2024]|project n=dayofyear(make_datetime(y,3,1))`, `[[60] [61]]`},
		{`print n=getyear(datetime(null)),p=datetime_part("bad",datetime(2024-01-01))`, `[[<nil> <nil>]]`},
		{`let f=(d:datetime){getyear(d)};print n=f(make_datetime(2024,1,1))`, `[[2024]]`},
		{`print y=getyear(datetime(2024-01-01)),ns=datepart("nanosecond",datetime(2024-01-01))|getschema|project ColumnType`, `[[int] [int]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
	for _, call := range []string{`datetime_part("year")`, `datepart(1,datetime(2024-01-01))`, `getyear("2024-01-01")`, `week_of_year()`, `dayofyear(datetime(2024-01-01),1)`} {
		if _, err := Compile("print p="+call, time.Now()); err == nil {
			t.Fatalf("invalid signature accepted: %s", call)
		}
	}
}
