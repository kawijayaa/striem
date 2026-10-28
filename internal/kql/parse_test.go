package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

const parseTraces = `let Traces = datatable(EventText: string)[
"Event: NotifySliceRelease (resourceName=PipelineScheduler, totalSlices=27, sliceNumber=23, lockTime=02/17/2016 08:40:01, releaseTime=02/17/2016 08:40:01, previousLockTime=02/17/2016 08:39:01)",
"Event: NotifySliceRelease (resourceName=PipelineScheduler, totalSlices=nonValidLongValue, sliceNumber=20, lockTime=02/17/2016 08:40:01, releaseTime=nonValidDateTime 08:40:01, previousLockTime=02/17/2016 08:39:01)"];
`

// Microsoft's parse and parse-where examples through Striem's SQLite runtime,
// including its US-format datetime conversion.
func TestParseOperatorExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{parseTraces + `Traces | parse EventText with * "resourceName=" resourceName ", totalSlices=" totalSlices: long * "sliceNumber=" sliceNumber: long * "lockTime=" lockTime ", releaseTime=" releaseTime: date "," * "previousLockTime=" previousLockTime: date ")" * | project resourceName, totalSlices, sliceNumber, lockTime, releaseTime, previousLockTime`,
			`[[PipelineScheduler 27 23 02/17/2016 08:40:01 2016-02-17T08:40:01.000000000Z 2016-02-17T08:39:01.000000000Z] [<nil> <nil> <nil> <nil> <nil> <nil>]]`},
		{parseTraces + `Traces | parse kind=relaxed EventText with * "resourceName=" resourceName ", totalSlices=" totalSlices: long ", sliceNumber=" sliceNumber: long * "lockTime=" lockTime ", releaseTime=" releaseTime: date "," * "previousLockTime=" previousLockTime: date ")" * | project totalSlices, sliceNumber, releaseTime, previousLockTime`,
			`[[27 23 2016-02-17T08:40:01.000000000Z 2016-02-17T08:39:01.000000000Z] [<nil> 20 <nil> 2016-02-17T08:39:01.000000000Z]]`},
		{parseTraces + `Traces | parse-where EventText with * "sliceNumber=" sliceNumber: long * "releaseTime=" releaseTime: date "," * | project sliceNumber, releaseTime`,
			`[[23 2016-02-17T08:40:01.000000000Z]]`},
		{parseTraces + `Traces | parse kind=regex flags=Ui EventText with * "RESOURCENAME=" resourceName ',' * | project resourceName`,
			`[[PipelineScheduler] [PipelineScheduler]]`},
		{`datatable(Contacts: string)["Event: LeadContact (email=abi@fourthcoffee.com, Website=https:www.fourthcoffee.com)"] | parse Contacts with * "email=" alias:string "@" domain: string ", Website=https:" WebsiteDomain: string ")" | project EmailAddress=strcat(alias, "@", domain), WebsiteDomain`,
			`[[abi@fourthcoffee.com www.fourthcoffee.com]]`},
		// Typical SIEM extraction followed by aggregation.
		{`datatable(Message:string)["Failed password for root from 10.0.0.5 port 22","Failed password for admin from 10.0.0.5 port 2222","Accepted password for bob from 10.0.0.9 port 22"] | parse-where Message with "Failed password for " User " from " SourceIp " port " Port:int | summarize Attempts=count(), Users=make_set(User) by SourceIp`,
			`[[10.0.0.5 2 ["root","admin"]]]`},
		{`datatable(s:string)["Sat, 8 Nov 14 15:05:02 GMT"] | parse s with * ", " rest | extend t=todatetime(s) | project rest, t`, `[[8 Nov 14 15:05:02 GMT 2014-11-08T15:05:02.000000000Z]]`},
		{`datatable(s:string)["x=1"] | parse s with "x=" x:long | getschema | project ColumnName, ColumnType`, `[[s string] [x long]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{
		`datatable(s:string)["a"] | parse kind=simple flags=i s with "a" a`,
		`datatable(s:string)["a"] | parse s with "a" a * "b" b:long`,
		`datatable(s:string)["a"] | parse kind=regex s with "(" a`,
		`datatable(s:string)["a"] | parse-where kind=relaxed s with "a" a`,
		`datatable(s:string)["a"] | parse s with "` + strings.Repeat("a", 600) + `" a`,
	} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid parse accepted: %s", query)
		}
	}
}
