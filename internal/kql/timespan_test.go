package kql

import (
	"fmt"
	"testing"
	"time"
)

// Documented string-operator, rounding and temporal behavior through Striem's
// runtime. Timespans are ticks in SQL; the API formats them as Kusto text.
func TestDocumentedCoreBehavior(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a="abc" has "b", b="North America" has "america", c="abc" contains "B", d="abc" contains_cs "B", e="abc" startswith "AB", f="abc" endswith "BC"`, `[[0 1 1 0 1 1]]`},
		{`print a="abc" contains "_", b="a%c" startswith "a%", c="x" contains "%"`, `[[0 1 0]]`},
		{`print a=5 between (1 .. 10), b=5 !between (1 .. 10), c=bin(4.5, 1), d=bin(-4.5, 1), e=bin(datetime(1970-05-11 13:45:07), 1d)`, `[[1 0 4 -5 1970-05-11T00:00:00.000000000Z]]`},
		{`print a=substring("123456", 1), b=substring("123456", 2, 2), c=substring("ABCD", 0, 2), d=substring("123456", -2, 2)`, `[[23456 34 AB 56]]`},
		{`print a=extract("x=([0-9.]+)", 1, "hello x=45.6|wo"), b=extract("x=([0-9.]+)", 1, "hello x=45.6|wo", typeof(real)), c=extract("n=(\\d+)", 1, "n=12", typeof(long)), d=extract("n=(\\w+)", 1, "n=ab", typeof(long))`, `[[45.6 45.6 12 <nil>]]`},
		{`print a=1d, b=2h + 30m, c=1d / 1h, d=datetime(2020-01-02) - datetime(2020-01-01), e=datetime(2020-01-01) + 1d, f=datetime(2020-01-01) - 1h`,
			`[[864000000000 90000000000 24 864000000000 2020-01-02T00:00:00.000000000Z 2019-12-31T23:00:00.000000000Z]]`},
		{`print a=now() - ago(1h), b=ago(-1d), c=now(1h), d=tostring(1h), e=tostring(totimespan("1.02:03:04")), f=dayofweek(datetime(1947-11-30 10:00:05)), g=dayofweek(datetime(1970-05-11))`,
			`[[36000000000 2026-01-05T00:00:00.000000000Z 2026-01-04T01:00:00.000000000Z 01:00:00 1.02:03:04 0 864000000000]]`},
		{`Events | extend Since=now() - TimeGenerated | where Since > 2d | project user, d=tostring(Since)`, `[[alice 3.00:00:00]]`},
		{`Events | summarize N=count() by Day=bin(TimeGenerated, 1d) | where Day > ago(3d) | order by Day asc | project Day`, `[[2026-01-02T00:00:00.000000000Z] [2026-01-03T00:00:00.000000000Z]]`},
		{`let window=12h; Events | where TimeGenerated between (datetime(2026-01-02) - window .. datetime(2026-01-02) + window) | count`, `[[1]]`},
		{`print a=bin(90m, 1h), b=tostring(bin(90m, 1h))`, `[[36000000000 01:00:00]]`},
		{`print t=1h | getschema | project ColumnType`, `[[timespan]]`},
		{`print a=abs(-3), b=sign(-2.5), c=sqrt(16), d=pow(2, 10), e=exp(0), f=log(1), g=log10(1000), h=ceiling(1.2), i=floor(-1.2), j=round(log(exp(2)), 9), k=sqrt(-1)`, `[[3 -1 4 1024 1 0 3 2 -2 2 <nil>]]`},
		{`print a=floor(7, 3), b=bin(7, 3), c=sin(0), d=cos(0), e=round(atan2(1, 1) * 4, 9), f=sign(-4), g=floor(5)`, `[[6 6 0 1 3.141592654 -1 5]]`},
		{`print a=tostring(true), b=strcat("a", int(null), "b"), c=strcat("x", false)`, `[[true ab xfalse]]`},
		{`LeftEvents | where host in (RightEvents | project host) | project user`, `[[alice]]`},
		{`LeftEvents | where user !in (RightEvents | project user) | summarize hint.strategy=shuffle make_set(user)`, `[[["alice","bob"]]]`},
		{`union isfuzzy=true LeftEvents, NoSuchTable | count`, `[[2]]`},
		{`LeftEvents | where TimeGenerated in (RightEvents | where TimeGenerated > datetime(2026-01-02) | project TimeGenerated) | count`, `[[0]]`},
		{`print x=dynamic([1,2]), y=dynamic(["a"]) | mv-expand x, y`, `[[1 a] [2 <nil>]]`},
		{`union withsource=T LeftEvents, RightEvents | summarize n=count() by T | order by T asc`, `[[LeftEvents 2] [RightEvents 1]]`},
		{`Events | where user in (dynamic(["alice", "carol"])) | summarize make_set(user)`, `[[["alice","carol"]]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print ago(1)`, `print bin(now(), 5)`, `print extract("a", 0, "a", typeof(dynamic))`, `print datetime(2020-01-01) + datetime(2020-01-02)`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid query accepted: %s", query)
		}
	}
}

// Microsoft's format_datetime, format_timespan and make_timespan examples.
func TestTemporalFormatting(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`let dt = datetime(2017-01-29 09:00:05); print v1=format_datetime(dt,'yy-MM-dd [HH:mm:ss]'), v2=format_datetime(dt, 'yyyy-M-dd [H:mm:ss]'), v3=format_datetime(dt, 'yy-MM-dd [hh:mm:ss tt]')`,
			`[[17-01-29 [09:00:05] 2017-1-29 [9:00:05] 17-01-29 [09:00:05 AM]]]`},
		{`let dt = datetime(2009-06-15 13:45:30.6175425); print a=format_datetime(dt, 'h:mm tt'), b=format_datetime(dt, 'fff'), c=format_datetime(dt, 'FFFFFFF'), d=format_datetime(datetime(2009-06-15 13:45:30.05), 'ss.F'), e=format_datetime(datetime(0900-01-01), 'yyyy y yy')`,
			`[[1:45 PM 617 6175425 30 0900 0 00]]`},
		{`let t = time(29.09:00:05.12345); print v1=format_timespan(t, 'dd.hh:mm:ss:FF'), v2=format_timespan(t, 'ddd.h:mm:ss [fffffff]')`,
			`[[29.09:00:05:12 029.9:00:05 [1234500]]]`},
		{`print ['timespan'] = make_timespan(1,12,30,55.123), a=tostring(make_timespan(2, 30)), b=make_timespan(24, 0), c=make_timespan(1, 2, 3)`,
			`[[1314551230000 02:30:00 <nil> 37230000000]]`},
		{`print x=tostring(make_timespan(1,12,30,55.123))`, `[[1.12:30:55.1230000]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}

// Timespans inside dynamic values and text conversions use Kusto's text form,
// as in Microsoft's pack_array example.
func TestTimespansInDynamicAndText(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`range x from 1 to 3 step 1 | extend y = tostring(x * 2) | extend z = (x * 2) * 1s | project pack_array(x, y, z)`, `[[[1,"2","00:00:02"]] [[2,"4","00:00:04"]] [[3,"6","00:00:06"]]]`},
		{`print b=bag_pack("t", 90m), s=strcat_delim("-", 1h, "x"), r=strrep(1m, 2, ","), v=reverse(1h)`, `[[{"t":"01:30:00"} 01:00:00-x 00:01:00,00:01:00 00:00:10]]`},
		{`datatable(n:string,c:long)["a",1] | extend d = bag_pack("name", n, "c", c) | summarize l=make_list(d)`, `[[[{"c":1,"name":"a"}]]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}
