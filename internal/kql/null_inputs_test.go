package kql

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

// Null inputs from SQLite must never fail a query (mattn passes NULL as
// []byte(nil), which breaks UDFs with typed parameters).
func TestNullInputsNeverFailQueries(t *testing.T) {
	db := featureDatabase(t)
	for _, q := range []string{
		`datatable(s:string)[""] | extend s=tostring(dynamic(null)) | parse-kv s as (a) with (pair_delimiter=' ', kv_delimiter='=')`,
		`datatable(s:string)[""] | extend s=tostring(dynamic(null)) | parse s with "a" a`,
		`print x=extract("a", 0, tostring(dynamic(null)))`,
		`print x=extract_all("(a)", tostring(dynamic(null)))`,
		`print x=countof(tostring(dynamic(null)), "a")`,
		`print x=strcat_delim(",", tostring(dynamic(null)), "a")`,
		`print x=translate("a","b",tostring(dynamic(null)))`,
		`print x=url_encode(tostring(dynamic(null)))`,
		`print x=hash_md5(tostring(dynamic(null)))`,
		`print x=parse_ipv4(tostring(dynamic(null)))`,
		`print x=split(tostring(dynamic(null)), ",")`,
		`print x=replace_string(tostring(dynamic(null)), "a", "b")`,
		`print x=trim("a", tostring(dynamic(null)))`,
		`print x=base64_encode_tostring(tostring(dynamic(null)))`,
		`print x=strrep(tostring(dynamic(null)), 2)`,
		`print x=reverse(tostring(dynamic(null)))`,
		`print x=toguid(tostring(dynamic(null)))`,
		`print x=todatetime(tostring(dynamic(null)))`,
		`print x=parse_json(tostring(dynamic(null)))`,
		`print x=parse_url(tostring(dynamic(null)))`,
		`print x="a" matches regex tostring(dynamic(null))`,
		`print x=tostring(dynamic(null)) has "a"`,
		`print x=gzip_compress_to_base64_string(tostring(dynamic(null)))`,
		`print x=unicode_codepoints_from_string(tostring(dynamic(null)))`,
		`print x=ipv4_is_in_any_range(tostring(dynamic(null)), "10.0.0.0/8")`,
		`print x=format_ipv4(tostring(dynamic(null)))`,
		`print x=datetime_diff("day", todatetime(tostring(dynamic(null))), now())`,
		`print x=make_datetime(int(null), 1, 1)`,
		`print x=binary_and(long(null), 1)`,
		`print x=range(long(null), 3)`,
		`print x=strcmp(tostring(dynamic(null)), "a")`,
		`print x=max_of(long(null), long(null))`,
		`datatable(s:string)["a"] | extend s=tostring(dynamic(null)) | where s has "a"`,
		`datatable(s:string)["a"] | extend s=tostring(dynamic(null)) | where s has_cs "a"`,
		`datatable(s:string)["a"] | extend s=tostring(dynamic(null)) | where s hasprefix "a" or s has_any ("a")`,
		`print x=bin(datetime(null), 1h)`,
		`print x=bin(now(), timespan(null))`,
		`print x=startofday(datetime(null)), y=endofmonth(datetime(null))`,
		`print x=repeat(tostring(dynamic(null)), long(null))`,
		`print x=array_shift_left(dynamic(null), long(null))`,
		`print x=tohex(long(null))`,
		`print x=bag_set_key(dynamic(null), tostring(dynamic(null)), 1)`,
		`datatable(s:string)["a"] | extend s=tostring(dynamic(null)) | summarize make_bag(parse_json(s)), make_list(s), make_set(s), dcount(s)`,
		`print x=ipv4_is_in_any_range("1.1.1.1", dynamic(null))`,
		`print x=array_index_of(dynamic(null), 1)`,
		`print x=set_union(dynamic(null), dynamic([1]))`,
		`print x=pack_array(int(null), tostring(dynamic(null)))`,
		`print x=bag_pack("a", int(null))`,
		`print x=toupper(tostring(dynamic(null)))`,
		`print x=substring(tostring(dynamic(null)), 1)`,
		`print x=indexof(tostring(dynamic(null)), "a")`,
		`print x=strlen(tostring(dynamic(null)))`,
		`print x=parse_csv(tostring(dynamic(null)))`,
	} {
		rows, err := tryRows(db, q)
		if err != nil {
			t.Errorf("%s: %v", q, err)
		} else {
			t.Logf("%s => %v", q, fmt.Sprint(rows))
		}
	}
	for _, tc := range []struct{ query, want string }{
		{`print n=strlen(tostring(dynamic(null))), e=isempty(tostring(int(null))), c=countof(tostring(dynamic(null)), "a")`, `[[0 1 0]]`},
		{`datatable(n:long)[1] | where n > 5 | summarize s=make_set(n), l=make_list(n)`, `[[[] []]]`},
		{`datatable(n:long)[long(null)] | summarize s=make_set(n), l=make_list(n)`, `[[[] []]]`},
		{`print x=extract("(a)", 1, tostring(dynamic(null)))`, `[[]]`},
	} {
		if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
			t.Errorf("%s got %s want %s", tc.query, got, tc.want)
		}
	}
}

func tryRows(db *sql.DB, q string) ([][]any, error) {
	c, err := Compile(q, time.Now(), featureCatalog())
	if err != nil {
		return nil, fmt.Errorf("compile: %w", err)
	}
	rows, err := db.Query(c.SQL, c.Args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var out [][]any
	for rows.Next() {
		v := make([]any, len(cols))
		p := make([]any, len(cols))
		for i := range v {
			p[i] = &v[i]
		}
		if err := rows.Scan(p...); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
