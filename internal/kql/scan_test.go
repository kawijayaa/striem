package kql

import (
	"fmt"
	"testing"
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
