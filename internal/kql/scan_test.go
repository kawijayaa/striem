package kql

import (
	"database/sql"
	"fmt"
	"strings"
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
		{`print h1 = hash("Hello"), h2 = hash("World") | extend c = hash_combine(h1, h2), same = hash_combine(h1, h2) == hash_many("Hello", "World"), n = isnull(hash_combine(h1, long(null))) | project h1, h2, same, n`, `[[753694413698530628 1846988464401551951 1 0]]`},
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

// Microsoft's row_window_session and rows_near examples.
func TestSessionsAndRowsNear(t *testing.T) {
	db := featureDatabase(t)
	events := `datatable (ID:string, Timestamp:datetime) ["1", datetime(2024-04-11 10:00:00), "2", datetime(2024-04-11 10:18:00), "1", datetime(2024-04-11 11:00:00), "3", datetime(2024-04-11 11:30:00), "2", datetime(2024-04-11 13:30:00), "2", datetime(2024-04-11 10:16:00)]`
	values := `datatable (Timestamp:datetime, Value:long, State:string)[datetime(2021-06-01), 1, "Success", datetime(2021-06-02), 4, "Success", datetime(2021-06-03), 3, "Success", datetime(2021-06-04), 11, "Success", datetime(2021-06-05), 15, "Success", datetime(2021-06-06), 2, "Success", datetime(2021-06-07), 19, "Error", datetime(2021-06-08), 12, "Success", datetime(2021-06-09), 7, "Success", datetime(2021-06-10), 9, "Success"]`
	for _, tc := range []struct{ query, want string }{
		{events + ` | sort by ID asc, Timestamp asc | extend SessionStarted = row_window_session(Timestamp, 1h, 5m, ID != prev(ID)) | project ID, tostring(Timestamp), tostring(SessionStarted)`,
			`[[1 2024-04-11T10:00:00.000000000Z 2024-04-11T10:00:00.000000000Z] [1 2024-04-11T11:00:00.000000000Z 2024-04-11T11:00:00.000000000Z] [2 2024-04-11T10:16:00.000000000Z 2024-04-11T10:16:00.000000000Z] [2 2024-04-11T10:18:00.000000000Z 2024-04-11T10:16:00.000000000Z] [2 2024-04-11T13:30:00.000000000Z 2024-04-11T13:30:00.000000000Z] [3 2024-04-11T11:30:00.000000000Z 2024-04-11T11:30:00.000000000Z]]`},
		{`range t from datetime(2024-01-01 00:00) to datetime(2024-01-01 00:20) step 4m | extend n = 1, s = row_window_session(t, 10m, 5m) | project tostring(s), n`,
			`[[2024-01-01T00:00:00.000000000Z 1] [2024-01-01T00:00:00.000000000Z 1] [2024-01-01T00:00:00.000000000Z 1] [2024-01-01T00:12:00.000000000Z 1] [2024-01-01T00:12:00.000000000Z 1] [2024-01-01T00:12:00.000000000Z 1]]`},
		{values + ` | sort by Timestamp asc | evaluate rows_near(State == "Error", 2) | project Value`, `[[15] [2] [19] [12] [7]]`},
		{values + ` | sort by Timestamp asc | evaluate rows_near(State == "Error", 1, 3) | project Value`, `[[2] [19] [12] [7] [9]]`},
		{values + ` | sort by Timestamp asc | evaluate rows_near(Value > 100, 2) | count`, `[[0]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print t=now() | extend row_window_session(t, 1h, 1h)`, `print t=now() | evaluate rows_near(true, -1)`, `print t=now() | evaluate rows_near(1, 2)`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid query accepted: %s", query)
		}
	}
}

// Microsoft's top-nested "other column data" pattern and top-hitters.
func TestTopNestedAndHitters(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(id: string, timestamp: datetime, otherInformation: string)["Barak", datetime(2015-01-01), "1", "Barak", datetime(2016-01-01), "2", "Barak", datetime(2017-01-20), "3", "Donald", datetime(2017-01-20), "4", "Donald", datetime(2017-01-18), "5", "Donald", datetime(2017-01-19), "6"] | top-nested of id by Ignore0=max(1), top-nested 2 of timestamp by Ignore1=max(timestamp), top-nested of otherInformation by Ignore2=max(1) | project-away Ignore0, Ignore1, Ignore2 | order by id asc, timestamp asc`,
			`[[Barak 2016-01-01T00:00:00.000000000Z 2] [Barak 2017-01-20T00:00:00.000000000Z 3] [Donald 2017-01-19T00:00:00.000000000Z 6] [Donald 2017-01-20T00:00:00.000000000Z 4]]`},
		{`Events | top-hitters 1 of host`, `[[a 2]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}

// assert passes true conditions through and fails the query otherwise.
func TestAssert(t *testing.T) {
	db := featureDatabase(t)
	if got := fmt.Sprint(featureRows(t, db, `print x = assert(1 < 2, "ok")`)); got != `[[1]]` {
		t.Fatalf("assert(true) = %s", got)
	}
	// Microsoft's example passes a positional argument after a named one.
	checked := `let checkLength = (len:long, s:string) { assert(len > 0, "Length must be greater than zero") and strlen(s) > len }; datatable(input:string) ['123', '4567'] | where checkLength(len=3, input)`
	if got := fmt.Sprint(featureRows(t, db, checked)); got != `[[4567]]` {
		t.Fatalf("checkLength = %s", got)
	}
	compiled, err := Compile(`print x = assert(1 > 2, "numbers broke")`, time.Now(), featureCatalog())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(compiled.SQL, compiled.Args...)
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "assertion failed: numbers broke") {
		t.Fatalf("assert(false) error = %v", err)
	}
}

// Microsoft's reduce examples and kind=source.
func TestReduce(t *testing.T) {
	db := featureDatabase(t)
	cities := `datatable (City: string) ["San Bernard", "San Jose", "San Diego", "Moscow", "Moscow", "One -on- One", "Two -on- Three", "Paris", "Lone"]`
	for _, tc := range []struct{ query, want string }{
		{`range x from 1 to 10 step 1 | project str = strcat("foo", "Z", tostring(x)) | reduce by str`, `[[others 10 ]]`},
		{`range x from 1 to 10 step 1 | project str = strcat("foo", "Z", tostring(x)) | reduce by str with characters="Z"`, `[[foo* 10 fooZ1]]`},
		{`range x from 1 to 1000 step 1 | project MyText = strcat("MachineLearningX", tostring(x % 10)) | reduce by MyText with threshold=0.5, characters="X"`, `[[MachineLearning* 1000 MachineLearningX1]]`},
		{`range x from 1 to 1000 step 1 | project MyText = strcat("MachineLearningX", tostring(x % 10)) | reduce by MyText with threshold=0.01, characters="X" | count`, `[[10]]`},
		{cities + ` | reduce by City with threshold=0.15`, `[[San * 3 San Bernard] [Moscow 2 Moscow] [* -on- * 2 One -on- One] [others 2 ]]`},
		{cities + ` | reduce kind=source by City with threshold=0.15 | where City in ("San Jose", "Paris")`, `[[San Jose San *] [Paris others]]`},
		{`datatable (s: string) [] | reduce by s`, `[]`},
		{`datatable (s: string) ["a", "a"] | extend s = iff(true, string(null), s) | reduce by s`, `[]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{
		`datatable (s: string) ["a"] | reduce by s with threshold=2`,
		`datatable (s: string) ["a"] | reduce kind=other by s`,
		`datatable (s: string) ["a"] | reduce by s with foo=1`,
	} {
		compiled, err := Compile(query, time.Now(), featureCatalog())
		if err == nil {
			var rows *sql.Rows
			if rows, err = db.Query(compiled.SQL, compiled.Args...); err == nil {
				for rows.Next() {
				}
				err = rows.Err()
				rows.Close()
			}
		}
		if err == nil {
			t.Errorf("%s: want an error", query)
		}
	}
}

// Microsoft's StormEvents sequence_detect example on its matching records:
// every chain within the windows is an occurrence.
func TestSequenceDetect(t *testing.T) {
	db := featureDatabase(t)
	storms := `datatable (StartTime: datetime, EventType: string, State: string) [
		datetime(2007-05-08 00:00), "Excessive Heat", "CALIFORNIA", datetime(2007-05-08 16:02), "Wildfire", "CALIFORNIA",
		datetime(2007-05-10 11:30), "Wildfire", "CALIFORNIA", datetime(2007-07-23 12:00), "Excessive Heat", "SOUTH DAKOTA",
		datetime(2007-07-27 09:00), "Wildfire", "SOUTH DAKOTA", datetime(2007-07-24 00:00), "Wildfire", "TEXAS",
		datetime(2007-08-31 08:00), "Excessive Heat", "CALIFORNIA", datetime(2007-09-01 11:28), "Wildfire", "CALIFORNIA",
		datetime(2007-09-02 12:00), "Excessive Heat", "CALIFORNIA", datetime(2007-09-02 13:30), "Wildfire", "CALIFORNIA",
		datetime(2007-10-01 00:00), "Excessive Heat", "CALIFORNIA", datetime(2007-10-07 00:00), "Wildfire", "CALIFORNIA"]`
	for _, tc := range []struct{ query, want string }{
		{storms + ` | evaluate sequence_detect(StartTime, 5d, 5d, heat=(EventType == "Excessive Heat"), wildfire=(EventType == 'Wildfire'), State) | project State, tostring(heat_StartTime), tostring(wildfire_StartTime), tostring(Duration)`,
			`[[CALIFORNIA 2007-05-08T00:00:00.000000000Z 2007-05-08T16:02:00.000000000Z 16:02:00] [CALIFORNIA 2007-05-08T00:00:00.000000000Z 2007-05-10T11:30:00.000000000Z 2.11:30:00] [SOUTH DAKOTA 2007-07-23T12:00:00.000000000Z 2007-07-27T09:00:00.000000000Z 3.21:00:00] [CALIFORNIA 2007-08-31T08:00:00.000000000Z 2007-09-01T11:28:00.000000000Z 1.03:28:00] [CALIFORNIA 2007-08-31T08:00:00.000000000Z 2007-09-02T13:30:00.000000000Z 2.05:30:00] [CALIFORNIA 2007-09-02T12:00:00.000000000Z 2007-09-02T13:30:00.000000000Z 01:30:00]]`},
		// Three steps: each step within 1h of the previous, all within 90m.
		{`datatable (T: datetime, E: string, U: string) [datetime(2026-01-01 00:00), "login", "a", datetime(2026-01-01 00:30), "sudo", "a", datetime(2026-01-01 01:20), "exfil", "a", datetime(2026-01-01 00:10), "login", "b", datetime(2026-01-01 01:30), "sudo", "b", datetime(2026-01-01 00:00), "sudo", "c"] | evaluate sequence_detect(T, 1h, 90m, E == "login", E == "sudo", E == "exfil", U) | project U, tostring(Duration)`,
			`[[a 01:20:00]]`},
		{`datatable (T: datetime, E: string) [datetime(2026-01-01), "x"] | evaluate sequence_detect(T, 1h, 1h, E == "x", E == "y") | count`, `[[0]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{
		`datatable (T: datetime, E: string) [] | evaluate sequence_detect(T, 1h, 1h, E)`,
		`datatable (T: string, E: string) [] | evaluate sequence_detect(T, 1h, 1h, E == "x")`,
		`datatable (T: datetime, E: string) [] | evaluate sequence_detect(T, 1, 1h, E == "x")`,
		`datatable (T: datetime, E: string) [] | evaluate sequence_detect(T, 1h, 1h, E == "x", strlen(E) + 1)`,
	} {
		if _, err := Compile(query, time.Now(), featureCatalog()); err == nil {
			t.Errorf("%s: want a compile error", query)
		}
	}
}

// Microsoft's sliding_window_counts example, plus dimensions and windows
// that end after the data.
func TestSlidingWindowCounts(t *testing.T) {
	db := featureDatabase(t)
	data := `let start = datetime(2017 - 08 - 01); let end = datetime(2017 - 08 - 07); let lookbackWindow = 3d; let bin = 1d;
let T = datatable(UserId: string, Timestamp: datetime) ['Bob', datetime(2017 - 08 - 01), 'David', datetime(2017 - 08 - 01), 'David', datetime(2017 - 08 - 01), 'John', datetime(2017 - 08 - 01), 'Bob', datetime(2017 - 08 - 01),
'Ananda', datetime(2017 - 08 - 02), 'Atul', datetime(2017 - 08 - 02), 'John', datetime(2017 - 08 - 02), 'Ananda', datetime(2017 - 08 - 03), 'Atul', datetime(2017 - 08 - 03), 'Atul', datetime(2017 - 08 - 03),
'John', datetime(2017 - 08 - 03), 'Bob', datetime(2017 - 08 - 03), 'Betsy', datetime(2017 - 08 - 04), 'Bob', datetime(2017 - 08 - 05)];
`
	for _, tc := range []struct{ query, want string }{
		{data + `T | evaluate sliding_window_counts(UserId, Timestamp, start, end, lookbackWindow, bin)`,
			`[[2017-08-01T00:00:00.000000000Z 5 3] [2017-08-02T00:00:00.000000000Z 8 5] [2017-08-03T00:00:00.000000000Z 13 5] [2017-08-04T00:00:00.000000000Z 9 5] [2017-08-05T00:00:00.000000000Z 7 5] [2017-08-06T00:00:00.000000000Z 2 2] [2017-08-07T00:00:00.000000000Z 1 1]]`},
		{data + `T | extend Team = iff(UserId startswith "A", "a", "b") | evaluate sliding_window_counts(UserId, Timestamp, start, datetime(2017-08-02), 1d, 1d, Team) | project tostring(Timestamp), Team, Count, dcount | order by Timestamp asc, Team asc`,
			`[[2017-08-01T00:00:00.000000000Z b 5 3] [2017-08-02T00:00:00.000000000Z a 2 2] [2017-08-02T00:00:00.000000000Z b 1 1]]`},
		{`datatable (Id: long, At: long) [1, 3, 2, 4, 1, 9] | evaluate sliding_window_counts(Id, At, 0, 10, 4, 2)`, `[[2 1 1] [4 2 2] [6 1 1] [8 1 1] [10 1 1]]`},
		{`datatable (Id: long, At: datetime) [] | evaluate sliding_window_counts(Id, At, datetime(2020-01-01), datetime(2020-01-02), 1d, 1h) | count`, `[[0]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{
		`datatable (Id: long, At: string) [] | evaluate sliding_window_counts(Id, At, 0, 1, 1, 1)`,
		`datatable (Id: long, At: datetime) [] | evaluate sliding_window_counts(Id, At, now(), now(), 1d, "week")`,
		`datatable (Id: long, At: datetime) [] | evaluate sliding_window_counts(Id, At, now(), now(), 1d)`,
	} {
		if _, err := Compile(query, time.Now(), featureCatalog()); err == nil {
			t.Errorf("%s: want a compile error", query)
		}
	}
}

// Typed mv-expand converts with KQL semantics: SQLite's TIMESTAMP cast
// turned "2017-08-01T00:00:00Z" into 2017.
func TestTypedExpansionConversions(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print x=dynamic(["2017-08-01T00:00:00Z", "bad", null]) | mv-expand x to typeof(datetime)`, `[[2017-08-01T00:00:00.000000000Z] [<nil>] [<nil>]]`},
		{`print x=dynamic(["1.00:00:00"]) | mv-expand x to typeof(timespan) | project y = tostring(x + 1h)`, `[[1.01:00:00]]`},
		{`print x=dynamic(["6f8e1a4c-3c4f-4a3e-9b1f-0a9c2b7f1e33"]) | mv-expand x to typeof(guid) | project tostring(x)`, `[[6f8e1a4c-3c4f-4a3e-9b1f-0a9c2b7f1e33]]`},
		{`print x=dynamic(["2017-08-01"]), y=dynamic([1, 2]) | mv-expand x to typeof(datetime), y to typeof(long) | project tostring(x), y`, `[[2017-08-01T00:00:00.000000000Z 1] [ 2]]`},
		{`print d = datetime(2017 - 08 - 01 10:30)`, `[[2017-08-01T10:30:00.000000000Z]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}

// Microsoft's union examples with views, wildcards and isfuzzy.
func TestUnionPatterns(t *testing.T) {
	db := featureDatabase(t)
	views := "let View_1 = view () { print x=1 }; let View_2 = view () { print x=1 }; let OtherView_1 = view () { print x=1 };\n"
	for _, tc := range []struct{ query, want string }{
		{views + `union isfuzzy=true View*, SomeView*, OtherView* | count`, `[[3]]`},
		{views + `union isfuzzy=true (View_1 | where x > 0), (View_2 | where x > 0), (View_3 | where x > 0) | count`, `[[2]]`},
		{views + `union withsource=T View* | order by T asc | project T`, `[[View_1] [View_2]]`},
		{views + `union *View_1 | count`, `[[2]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	if _, err := Compile(views+`union SomeView*`, time.Now(), featureCatalog()); err == nil {
		t.Error("union of an unmatched pattern without isfuzzy should fail")
	}
}

// Microsoft's gettype table, element types read from their JSON container,
// blank parse_json, typed calendar functions and guid literals.
func TestRuntimeTypes(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a=gettype("a"), b=gettype(111), c=gettype(1==1), d=gettype(now()), e=gettype(1s), f=gettype(parse_json('1')), g=gettype(parse_json(' "abc" ')), h=gettype(parse_json(' {"abc":1} ')), i=gettype(parse_json(' [1, 2, 3] ')), j=gettype(123.45), k=gettype(parse_json(''))`,
			`[[string long bool datetime timespan int string dictionary array real null]]`},
		{`print a=gettype(dynamic({"a":"123"}).a), b=gettype(dynamic({"a":true}).a), c=gettype(dynamic({"a":{"b":[1, "x"]}}).a.b[-1]), d=gettype(dynamic([1])[5]), e=gettype(parse_json("5000000000")), f=gettype(dynamic(null))`,
			`[[string bool string null long null]]`},
		{`print a=isnull(parse_json("  ")), b=tostring(parse_json("")), c=parse_json("abc")`, `[[1  "abc"]]`},
		{`range h from startofmonth(datetime(2023-02-01 05:00)) to datetime(2023-02-01 23:00) step 4.534h | count`, `[[6]]`},
		{`print g = guid(12E8B78D-55B4-46AE-B068-26D7A0080254)`, `[[12e8b78d-55b4-46ae-b068-26d7a0080254]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	if _, err := Compile(`print startofmonth("x")`, time.Now(), featureCatalog()); err == nil {
		t.Error("startofmonth of a string should fail")
	}
}

// Microsoft's parse_version comparison example; values are 32 zero-padded
// digits so comparisons stay exact.
func TestParseVersion(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`datatable(lab: string, v: string) ["Lab A", "0.0.0.5", "Lab B", "0.0.7.0", "Lab D","0.0.3", "Lab C", "0.2", "Lab G", "0.1.2.0", "Lab F", "1.2.3.4", "Lab E", "1"]
| extend parsed_version = parse_version(v) | extend needs_update = iff(parsed_version < parse_version("1.0.0.0"), "Yes", "No") | project lab, v, needs_update | sort by lab asc, v, needs_update`,
			`[[Lab A 0.0.0.5 Yes] [Lab B 0.0.7.0 Yes] [Lab C 0.2 Yes] [Lab D 0.0.3 Yes] [Lab E 1 No] [Lab F 1.2.3.4 No] [Lab G 0.1.2.0 Yes]]`},
		{`print a = parse_version("1.2.3.4"), b = parse_version("1.0") == parse_version("1.0.0.0"), c = parse_version("1.0.0.1") < parse_version("1.0.0.2"), d = parse_version("99999999.0.0.0") > parse_version("9.99999999")`,
			`[[00000001000000020000000300000004 1 1 1]]`},
		{`print a = isnull(parse_version("1.2.3.4.5")), b = isnull(parse_version("1..2")), c = isnull(parse_version("123456789")), d = isnull(parse_version("v1")), e = isnull(parse_version("")), f = isnull(parse_version(string(null))), g = isnull(parse_version("-1"))`,
			`[[1 1 1 1 1 1 1]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}

// column_ifexists, bag_pack_columns, table() with a parameter and data
// scope, and documented aliases.
func TestColumnAndTableFunctions(t *testing.T) {
	db := featureDatabase(t)
	data := `datatable(Id: string, Value: string, Other: long) ["A", "val_a", 1, "B", "val_b", 2]`
	for _, tc := range []struct{ query, want string }{
		{data + ` | project column_ifexists("Capital", Id), column_ifexists("Value", "none"), columnifexists("Missing", Other * 10)`, `[[A val_a 10] [B val_b 20]]`},
		{data + ` | extend Packed = bag_pack_columns(Id, Value) | project Packed`, `[[{"Id":"A","Value":"val_a"}] [{"Id":"B","Value":"val_b"}]]`},
		{`let T = datatable(x: long) [1, 2, 3]; let foo = (tableName: string) { table(tableName) | count }; foo('T')`, `[[3]]`},
		{`let T = datatable(x: long) [1, 2]; table('T', 'hotcache') | count`, `[[2]]`},
		{`let f = (c: string) { datatable(a: long) [5] | project v = column_ifexists(c, -1) }; union (f("a")), (f("b")) | order by v asc`, `[[-1] [5]]`},
		{`print a = arraylength(dynamic([1, 2])), b = array_strcat(dynamic(["x", "y"]), "-"), c = hash_xxhash64("World"), d = hash_xxhash64("World", 100)`, `[[2 x-y 1846988464401551951 51]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{
		data + ` | project column_ifexists(strcat("I", "d"), 1)`,
		data + ` | project bag_pack_columns(Id, strlen(Value))`,
		`let T = datatable(x: long) [1]; table('T', 'cold')`,
		`table('sqlite_master')`,
	} {
		if _, err := Compile(query, time.Now(), featureCatalog()); err == nil {
			t.Errorf("%s: want a compile error", query)
		}
	}
}

// Microsoft's punycode examples, round trips and malformed input.
func TestPunycode(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print a = punycode_from_string('académie-française'), b = punycode_to_string('acadmie-franaise-npb1a'), c = punycode_from_string('艺术')`, `[[acadmie-franaise-npb1a académie-française cqv902d]]`},
		{`datatable(domain:string)['Lê Lợi。Thuận Thiên。com', 'Riðill｡Skáldskaparmál｡org', "Kaledvoulc'h.Artorījos.edu"] | extend str = punycode_domain_to_string(domain) | project str`,
			`[[xn--L Li-gpa4517b.xn--Thun Thin-s4a7194f.com] [xn--Riill-jta.xn--Skldskaparml-dbbj.org] [Kaledvoulc'h.xn--Artorjos-ejb.edu]]`},
		{`datatable(encoded:string)["xn--Ge-mia.Bulg.edu", "xn--Lin-8na.Celtchair.org", "xn--Ry-lja8c.xn--Jng-uta63a.xn--Bng-9ka.com"] | extend domain = punycode_domain_from_string(encoded) | project domain`,
			`[[Gáe.Bulg.edu] [Lúin.Celtchair.org] [Rúyì.Jīngū.Bàng.com]]`},
		{`print a = punycode_to_string('ab$c'), b = punycode_to_string('é'), c = punycode_domain_from_string('xn--99999999999.com'), d = isnull(punycode_to_string(string(null))), e = punycode_domain_from_string('аpple.com')`, `[[   1 аpple.com]]`},
		{`print punycode_domain_from_string(punycode_domain_to_string('раураl.com'))`, `[[раураl.com]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}

// Microsoft's parse_user_agent examples and look-for validation.
func TestParseUserAgent(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print useragent = "Mozilla/5.0 (Windows; U; en-US) AppleWebKit/531.9 (KHTML, like Gecko) AdobeAIR/2.5.1" | project x = parse_user_agent(useragent, "browser")`,
			`[[{"Browser":{"Family":"AdobeAIR","MajorVersion":"2","MinorVersion":"5","Patch":"1"}}]]`},
		{`print useragent = "Mozilla/5.0 (SymbianOS/9.2; U; Series60/3.1 NokiaN81-3/10.0.032 Profile/MIDP-2.0 Configuration/CLDC-1.1 ) AppleWebKit/413 (KHTML, like Gecko) Safari/4" | project x = parse_user_agent(useragent, dynamic(["browser","os","device"]))`,
			`[[{"Browser":{"Family":"Nokia OSS Browser","MajorVersion":"3","MinorVersion":"1","Patch":""},"OperatingSystem":{"Family":"Symbian OS","MajorVersion":"9","MinorVersion":"2","Patch":"","PatchMinor":""},"Device":{"Family":"Nokia N81","Brand":"Nokia","Model":"N81-3"}}]]`},
		{`print a = parse_user_agent("curl/8.4.0", "browser").Browser.Family, b = parse_user_agent("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36", "OS").OperatingSystem.Family, c = isnull(parse_user_agent(string(null), "device")), d = isnull(parse_user_agent(strrep(strrep("A", 100), 100), "device"))`,
			`[[curl Windows 1 1]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print parse_user_agent("x", "cpu")`, `print parse_user_agent("x", dynamic([1]))`} {
		compiled, err := Compile(query, time.Now(), featureCatalog())
		if err == nil {
			var rows *sql.Rows
			if rows, err = db.Query(compiled.SQL, compiled.Args...); err == nil {
				for rows.Next() {
				}
				err = rows.Err()
				rows.Close()
			}
		}
		if err == nil {
			t.Errorf("%s: want an error", query)
		}
	}
}

// Kusto's string operators: term matching, case rules, per-row terms that
// cannot inject regex syntax, null strings read as "", and datetime between
// spans.
func TestStringOperatorSemantics(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print s="North Carolina" | project a=s has "carolina", b=s has "carol", c=s has_cs "carolina", d=s hasprefix "car", e=s hassuffix "lina", f=s contains_cs "th ca", g=s startswith_cs "north", h=s =~ "north carolina"`, `[[1 0 0 1 1 0 0 1]]`},
		{`print s="abc-def.ghi@x.com" | project a=s has "ghi@x", b=s has_any ("zz", "ghi"), c=s has_all ("abc", "nope"), d=s !contains "DEF"`, `[[1 1 0 0]]`},
		{`datatable(s:string, t:string)["hello world", "world", "hello", "wor", "a.b+c", "b+c", "x", "", "Hello", "HELLO", "a b", ".*"] | project h = s has t, cs = s has_cs t, p = s hasprefix t, n = s !has t`,
			`[[1 1 1 0] [0 0 0 1] [1 1 1 0] [0 0 0 1] [1 0 1 0] [0 0 0 1]]`},
		{`let ioc = "world"; datatable(s:string)["hello world", "worldly"] | where s has ioc`, `[[hello world]]`},
		{`datatable(s:string)["a", "", "b"] | extend n = iff(s == "a", string(null), s) | summarize ne = countif(n != "b"), empty = countif(n == ""), notin = countif(n !in ("b")), ci = countif(n =~ "")`, `[[2 2 2 2]]`},
		{`print a = datetime(2020-01-02) between (datetime(2020-01-01) .. 1d), b = datetime(2020-01-03) between (datetime(2020-01-01) .. 1d), c = datetime(2020-01-03) !between (datetime(2020-01-01) .. 1d)`, `[[1 0 1]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}
