package kql

import (
	"fmt"
	"testing"
	"time"
)

// Microsoft's scan examples: cumulative sums, fill-forward, sessions with
// match ids, ordered sequences and output modes.
func TestScanOperator(t *testing.T) {
	db := featureDatabase(t)
	sessions := `datatable (Ts: timespan, Event: string) [0m, "A", 1m, "A", 2m, "B", 3m, "D", 32m, "B", 36m, "C", 38m, "D", 41m, "E", 75m, "A"]`
	sequence := `datatable (Ts: timespan, Event: string) [0m, "A", 1m, "Start", 2m, "B", 3m, "D", 4m, "Stop", 6m, "C", 8m, "Start", 11m, "E", 12m, "Stop"]`
	for _, tc := range []struct{ query, want string }{
		{"range x from 1 to 5 step 1 | scan declare (cumulative_x:long=0) with (step s1: true => cumulative_x = x + s1.cumulative_x;)", `[[1 1] [2 3] [3 6] [4 10] [5 15]]`},
		{"range x from 1 to 5 step 1 | extend y = 2 * x | scan declare (cumulative_x:long=0, cumulative_y:long=0) with (step s1: true => cumulative_x = iff(s1.cumulative_x >= 10, x, x + s1.cumulative_x), cumulative_y = iff(s1.cumulative_y >= 10, y, y + s1.cumulative_y);)",
			`[[1 2 1 2] [2 4 3 6] [3 6 6 12] [4 8 10 8] [5 10 5 18]]`},
		{`datatable (Ts: timespan, Event: string) [0m, "A", 1m, "", 2m, "B", 3m, ""] | sort by Ts asc | scan declare (Event_filled: string="") with (step s1: true => Event_filled = iff(isempty(Event), s1.Event_filled, Event);) | project Event_filled`, `[[A] [A] [B] [B]]`},
		{sessions + ` | sort by Ts asc | scan with_match_id=session_id declare (sessionStart: timespan) with (step inSession: true => sessionStart = iff(isnull(inSession.sessionStart), Ts, inSession.sessionStart); step endSession output=none: Ts - inSession.sessionStart > 30m;) | project tostring(Ts), tostring(sessionStart), session_id`,
			`[[00:00:00 00:00:00 0] [00:01:00 00:00:00 0] [00:02:00 00:00:00 0] [00:03:00 00:00:00 0] [00:32:00 00:32:00 1] [00:36:00 00:32:00 1] [00:38:00 00:32:00 1] [00:41:00 00:32:00 1] [01:15:00 01:15:00 2]]`},
		{sequence + ` | sort by Ts asc | scan with_match_id=m_id with (step s1: Event == "Start"; step s2: Event != "Start" and Event != "Stop" and Ts - s1.Ts <= 5m; step s3: Event == "Stop" and Ts - s1.Ts <= 5m;) | project tostring(Ts), Event, m_id`,
			`[[00:01:00 Start 0] [00:02:00 B 0] [00:03:00 D 0] [00:04:00 Stop 0] [00:08:00 Start 1] [00:11:00 E 1] [00:12:00 Stop 1]]`},
		{`datatable(Timestamp:datetime, userID:int, EventType:string)[datetime(2024-05-28 08:15:23), 1, "login", datetime(2024-05-28 12:30:45), 1, "logout"] | sort by Timestamp asc | scan declare (start: datetime, end: datetime, sessionDuration: timespan) with (step s1 output=none: EventType == "login" => start = Timestamp; step s2 output=all: EventType == "logout" => start = s1.start, end = Timestamp, sessionDuration = Timestamp - s1.start;) | project start, end, tostring(sessionDuration)`,
			`[[2024-05-28T08:15:23.000000000Z 2024-05-28T12:30:45.000000000Z 04:15:22]]`},
		{`range x from 1 to 6 step 1 | scan with (step s1 output=last: x % 3 != 0; step s2: x % 3 == 0;)`, `[[2] [3] [5] [6]]`},
		{`range x from 1 to 4 step 1 | scan with (step s1 output=last: true;)`, `[[4]]`},
		{`range x from 1 to 3 step 1 | where x > 5 | scan declare (c:long=0) with (step s1: true => c = s1.c + 1;)`, `[]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}

// Queries that crash or hang the compiler are denials of service; Compile
// reports compiler panics as errors and the lexer terminates on Unicode
// digits.
func TestCompileTerminatesSafely(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = Compile("print ٦", time.Now())
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("compiling a Unicode digit did not terminate")
	}
}

// Microsoft's examples for hashing, command lines, paths, regex helpers,
// has_any_index, extract_json, dynamic_to_json and around.
func TestForensicTextFunctions(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=hash("World"), b=hash("World", 100), c=hash(datetime("2015-01-01")), d=hash_many("Hello", "World") == hash_many("Hello", "World"), e=hash_many("Hello", "World") != hash_many("World", "Hello"), n=hash(string(null))`, `[[1846988464401551951 51 1380966698541616202 1 1 <nil>]]`},
		{`print a=parse_command_line("echo \"hello world!\"", "windows"), b=parse_command_line(strcat(@"C:\Program Files\x.exe", ' "a b" c\\"d "e""f" g\\\\"h"'), "windows"), c=parse_command_line(@"""C:\Program Files\x.exe"" -n", "windows"), bad=parse_command_line("x", "linux")`,
			`[[["echo","hello world!"] ["C:\\Program","Files\\x.exe","a b","c\"d","e\"f","g\\h"] ["C:\\Program Files\\x.exe","-n"] <nil>]]`},
		{`datatable(p:string)[@"C:\temp\file.txt", @"temp\file.txt", "file://C:/temp/file.txt:some.exe", @"\\shared\users\temp\file.txt.gz", "/usr/lib/temp/file.txt"] | project parse_path(p)`,
			`[[{"Scheme":"","RootPath":"C:","DirectoryPath":"C:\\temp","DirectoryName":"temp","Filename":"file.txt","Extension":"txt","AlternateDataStreamName":""}] [{"Scheme":"","RootPath":"","DirectoryPath":"temp","DirectoryName":"temp","Filename":"file.txt","Extension":"txt","AlternateDataStreamName":""}] [{"Scheme":"file","RootPath":"C:","DirectoryPath":"C:/temp","DirectoryName":"temp","Filename":"file.txt","Extension":"txt","AlternateDataStreamName":"some.exe"}] [{"Scheme":"","RootPath":"","DirectoryPath":"\\\\shared\\users\\temp","DirectoryName":"temp","Filename":"file.txt.gz","Extension":"gz","AlternateDataStreamName":""}] [{"Scheme":"","RootPath":"","DirectoryPath":"/usr/lib/temp","DirectoryName":"temp","Filename":"file.txt","Extension":"txt","AlternateDataStreamName":""}]]`},
		{`print q=regex_quote('(so$me.Te^xt)'), i1=indexof_regex("abcabc", @"a.c"), i2=indexof_regex("abcabcdefg", @"a.c", 0, 9, 2), i3=indexof_regex("abcabc", @"a.c", 1, -1, 2), i4=indexof_regex("ababaa", @"a.a", 0, -1, 2), i5=indexof_regex("abcabc", @"a|ab", -1), u=indexof_regex("ééabc", "abc")`, `[[\(so\$me\.Te\^xt\) 0 3 -1 -1 <nil> 2]]`},
		{`print idx1 = has_any_index("this is an example", dynamic(['this', 'example'])), idx2 = has_any_index("this is an example", dynamic(['not', 'example'])), idx3 = has_any_index("this is an example", dynamic(['not', 'found'])), idx4 = has_any_index("Example number 2", range(1, 3, 1)), idx5 = has_any_index("this is an example", dynamic([])), part = has_any_index("examples", dynamic(['example']))`, `[[0 1 -1 1 -1 -1]]`},
		{`let json = '{"name": "John", "age": 30, "city": "New York", "tags": ["a", "b"], "o": {"k": true}}'; print n=extract_json("$.name", json, typeof(string)), a=extract_json("$.age", json, typeof(long)) + 1, t=extractjson("$.tags[1]", json), o=extract_json("$['o'].k", json, typeof(bool)), all=extract_json("$.o", json), miss=extract_json("$.nope", json)`, `[[John 31 b 1 {"k":true} <nil>]]`},
		{`print j=dynamic_to_json(dynamic({'Y10':dynamic({}), 'X8': dynamic({'c3':1, 'd8':5, 'a4':6}), 'D1':114, 'A1':12, 'A14':[15, 13, 18]})) == dynamic_to_json(dynamic({'A14':[15, 13, 18], 'A1':12, 'D1':114, 'X8': dynamic({'a4':6, 'c3':1, 'd8':5}), 'Y10': dynamic({})})), s=dynamic_to_json(dynamic({'b':1, 'a':[2]}))`, `[[1 {"a":[2],"b":1}]]`},
		{`range dt from datetime(2021-01-01 01:00) to datetime(2021-01-01 02:00) step 1min | where around(dt, datetime(2021-01-01 01:30), 1min) | count`, `[[3]]`},
		{`print a=around(1.5, 2, 0.5), b=around(3, 2, 0.5), c=around(1h, 2h, 1h)`, `[[1 0 1]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}
