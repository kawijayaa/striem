package kql

import (
	"fmt"
	"testing"
)

// Microsoft's parse-kv examples through Striem's runtime, including typed
// datetime and guid conversions.
func TestParseKVExecution(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`print str="ThreadId:458745723, Machine:Node001, Text: The service is up, Level: Info" | parse-kv str as (Text: string, ThreadId:long, Machine: string) with (pair_delimiter=',', kv_delimiter=':') | project-away str`,
			`[[The service is up 458745723 Node001]]`},
		{`print str='src=10.1.1.123 dst=10.1.1.124 bytes=125 failure="connection aborted" "event time"=2021-01-01T10:00:54' | parse-kv str as (['event time']:datetime, src:string, dst:string, bytes:long, failure:string) with (pair_delimiter=' ', kv_delimiter='=', quote='"') | project-away str`,
			`[[2021-01-01T10:00:54.000000000Z 10.1.1.123 10.1.1.124 125 connection aborted]]`},
		{`print str='src=10.1.1.123 dst=10.1.1.124 bytes=125 failure=(connection aborted) (event time)=(2021-01-01 10:00:54)' | parse-kv str as (['event time']:datetime, src:string, dst:string, bytes:long, failure:string) with (pair_delimiter=' ', kv_delimiter='=', quote='()') | project-away str`,
			`[[2021-01-01T10:00:54.000000000Z 10.1.1.123 10.1.1.124 125 connection aborted]]`},
		{`print str='src=10.1.1.123 dst=10.1.1.124 bytes=125 failure="the remote host sent \\"bye!\\"" time=2021-01-01T10:00:54' | parse-kv str as (['time']:datetime, src:string, dst:string, bytes:long, failure:string) with (pair_delimiter=' ', kv_delimiter='=', quote='"', escape='\\') | project-away str`,
			`[[2021-01-01T10:00:54.000000000Z 10.1.1.123 10.1.1.124 125 the remote host sent "bye!"]]`},
		{`print str='name=John Doe phone=555 5555 city=New York' | parse-kv str as (name:string, phone:string, city:string) with (pair_delimiter=' ', kv_delimiter='=', greedy=true) | project-away str`,
			`[[John Doe 555 5555 New York]]`},
		{`print str="2021-01-01T10:00:34 [INFO] ThreadId:458745723, Machine:Node001, Text: 'The service \\' is up'" | parse-kv str as (Text: string, ThreadId:long, Machine: string) with (quote="'", escape='\\') | project-away str`,
			`[[The service ' is up 458745723 Node001]]`},
		{`print str=@'["referer url: https://hostname.com/redirect?dest=/?h=1234", "request url: https://hostname.com/?h=1234", "advertiser id: 24fefbca-cf27-4d62-a623-249c2ad30c73"]' | parse-kv str as (['referer url']:string, ['request url']:string, ['advertiser id']: guid) with (regex=@'"([\w ]+)\s*:\s*([^"]*)"') | project-away str`,
			`[[https://hostname.com/redirect?dest=/?h=1234 https://hostname.com/?h=1234 24fefbca-cf27-4d62-a623-249c2ad30c73]]`},
		// A firewall-style log summarized after extraction.
		{`datatable(Line:string)["action=deny src=10.0.0.5 dport=22","action=allow src=10.0.0.9 dport=443","action=deny src=10.0.0.5 dport=3389"] | parse-kv Line as (action, src, dport:int) with (pair_delimiter=' ', kv_delimiter='=') | where action == "deny" | summarize Ports=make_set(dport) by src`,
			`[[10.0.0.5 [22,3389]]]`},
		{`print g=toguid("{24FEFBCA-CF27-4D62-A623-249C2AD30C73}"), bad=toguid("x")`, `[[24fefbca-cf27-4d62-a623-249c2ad30c73 <nil>]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Fatalf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
}
