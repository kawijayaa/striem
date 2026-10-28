package kql

import (
	"fmt"
	"testing"
	"time"
)

// Microsoft's percentile, t-digest and hll examples, with nearest-rank and
// sketch edge cases.
func TestPercentilesAndSketches(t *testing.T) {
	db := featureDatabase(t)
	latency := `datatable (ReqCount:long, LatencyBucket:long) [8, 10, 6, 20, 3, 30, 1, 40]`
	for _, tc := range []struct{ query, want string }{
		{latency + ` | summarize percentilesw_array(LatencyBucket, ReqCount, 50, 75, 99.9)`, `[[[20,20,40]]]`},
		{latency + ` | summarize percentilesw_array(LatencyBucket, ReqCount, dynamic([50, 75, 99.9]))`, `[[[20,20,40]]]`},
		{latency + ` | summarize percentilesw(LatencyBucket, ReqCount, 50, 75, 99.9)`, `[[20 20 40]]`},
		{latency + ` | summarize percentilew(LatencyBucket, ReqCount, 50)`, `[[20]]`},
		{`range x from 1 to 10 step 1 | summarize percentile(x, 50), percentiles(x, 0, 10, 99.9, 100), percentiles_array(x, 25, 75)`, `[[5 1 1 10 10 [3,8]]]`},
		{`range x from 1 to 10 step 1 | summarize (p25, p75) = percentiles(x, 25, 75) | project p75, p25`, `[[8 3]]`},
		{`range x from 1 to 4 step 1 | extend d = datetime(2024-01-01) + x * 1h, s = x * 1m | summarize percentile(d, 50), percentile(s, 100)`, `[[2024-01-01T02:00:00.000000000Z 2400000000]]`},
		{`range x from 1 to 10 step 1 | extend y = x + 10 | summarize tdigestX = tdigest(x), tdigestY = tdigest(y) | project merged = merge_tdigest(tdigestX, tdigestY) | project percentile_tdigest(merged, 100, typeof(long))`, `[[20]]`},
		{`range x from 1 to 1000 step 1 | summarize t_x=tdigest(x) | project rank_of_685=rank_tdigest(t_x, 685), pr=percentrank_tdigest(t_x, 250), p=percentile_tdigest(t_x, 50), arr=percentiles_array_tdigest(t_x, 10, 90)`, `[[685 25 500 [100,900]]]`},
		{`range x from 1 to 20 step 1 | summarize t=tdigest(x) by g=x % 2 | summarize m=tdigest_merge(t) | project percentile_tdigest(m, 50, typeof(long))`, `[[10]]`},
		{`range x from 1 to 10 step 1 | extend y = x + 10 | summarize hll_x = hll(x), hll_y = hll(y) | project merged = hll_merge(hll_x, hll_y) | project dcount_hll(merged)`, `[[20]]`},
		{`range x from 1 to 100 step 1 | summarize h=hll(x % 7) by b = x % 2 | summarize m=hll_merge(h) | project dcount_hll(m)`, `[[7]]`},
		{`range x from 1 to 100 step 1 | summarize e=dcount_hll(hll_if(x, x % 2 == 0)), all=dcount_hll(hll(x, 0))`, `[[50 100]]`},
		{`range x from 1 to 20000 step 1 | summarize d=dcount_hll(hll(x)) | project ok = d between (19700 .. 20300)`, `[[1]]`},
		{`range x from 1 to 50000 step 1 | summarize p=percentile(x, 50), lo=percentile(x, 0), hi=percentile(x, 100) | project ok = p between (24750 .. 25250), lo, hi`, `[[1 1 50000]]`},
		{`datatable(x:long)[] | summarize percentile(x, 50), tdigest(x), hll(x)`, `[[<nil> <nil> <nil>]]`},
		{`print n = dcount_hll(dynamic({"hll":14,"sparse":["zz"]}))`, `[[<nil>]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", tc.query, got, tc.want)
		}
	}
	for _, query := range []string{`print x=1 | summarize percentile(x)`, `print x="a" | summarize percentile(x, 50)`, `print x=1 | summarize hll(x, 1.5)`, `print x=1 | summarize percentilew(x, 1.5, 50)`} {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Fatalf("invalid call accepted: %s", query)
		}
	}
}
