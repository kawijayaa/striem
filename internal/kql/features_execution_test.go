package kql

import (
	"database/sql"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func featureDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`CREATE TABLE events(time_generated TEXT, source TEXT, raw_data TEXT, dataset_id INTEGER);
 INSERT INTO events VALUES
 ('2026-01-01T00:00:00.000000000Z', 'left', '{"host":"a","user":"alice","n":1,"items":[1,2],"message":"PowerShell alpha"}', 1),
 ('2026-01-02T00:00:00.000000000Z', 'left', '{"host":"b","user":"bob","n":2,"items":[3],"message":"benign"}', 1),
 ('2026-01-03T00:00:00.000000000Z', 'right', '{"host":"a","user":"carol","n":3,"items":[],"message":"PowerShell beta"}', 2);`)
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func featureCatalog() TableCatalog {
	fields := []Field{{Name: "host", Type: "string"}, {Name: "user", Type: "string"}, {Name: "n", Type: "long"}, {Name: "items", Type: "dynamic"}, {Name: "message", Type: "string"}}
	return TableCatalog{"LeftEvents": {ID: 1, Fields: fields}, "RightEvents": {ID: 2, Fields: fields}}
}

func featureRows(t *testing.T, db *sql.DB, query string) [][]any {
	t.Helper()
	compiled, err := Compile(query, time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC), featureCatalog())
	if err != nil {
		t.Fatalf("compile %s: %v", query, err)
	}
	rows, err := db.Query(compiled.SQL, compiled.Args...)
	if err != nil {
		t.Fatalf("execute %s: %v\n%s", query, err, compiled.SQL)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(columns, compiled.Columns) {
		t.Fatalf("column metadata=%v, actual=%v", compiled.Columns, columns)
	}
	var result [][]any
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}
		if err := rows.Scan(pointers...); err != nil {
			t.Fatal(err)
		}
		result = append(result, values)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate %s: %v\n%s", query, err, compiled.SQL)
	}
	return result
}

func TestDocumentedTabularFeaturesExecute(t *testing.T) {
	db := featureDatabase(t)
	cases := []struct{ name, query, want string }{
		{"filter", `Events | filter n > 1 | project n | sort by n asc`, `[[2] [3]]`},
		{"search", `Events | search "powershell" | project n | order by n asc`, `[[1] [3]]`},
		{"project variants", `Events | project host, user, n | project-away user | project-rename Value=n | project-reorder Value, host | where Value == 1`, `[[1 a]]`},
		{"project keep", `Events | project-keep host, n | where n == 2`, `[[b 2]]`},
		{"extend serialize", `Events | extend doubled=n * 2 | serialize | where n == 2 | project doubled`, `[[4]]`},
		{"summarize", `Events | summarize C=count(), CI=countif(n > 1), S=sum(n), SI=sumif(n,n > 1), Lo=min(n), Hi=max(n), A=avg(n)`, `[[3 2 6 5 1 3 2]]`},
		{"distinct", `Events | distinct host | order by host asc`, `[[a] [b]]`},
		{"count", `Events | count`, `[[3]]`},
		{"top", `Events | top 2 by n desc | project n`, `[[3] [2]]`},
		{"take", `Events | order by n asc | take 1 | project n`, `[[1]]`},
		{"limit", `Events | order by n asc | limit 2 | project n`, `[[1] [2]]`},
		{"sample", `Events | sample 2 | count`, `[[2]]`},
		{"sample-distinct", `Events | sample-distinct 5 of host | order by host asc`, `[[a] [b]]`},
		{"as", `LeftEvents | as L | join kind=inner (L) on host | project host | order by host asc`, `[[a] [b]]`},
		{"mv-expand typed", `Events | mv-expand with_itemindex=idx item=items to typeof(long) | project item,idx | order by item asc`, `[[1 0] [2 1] [3 0]]`},
		{"mv-apply", `Events | mv-apply item=items on (where item > 1 | extend doubled=item * 2 | serialize) | project doubled | order by doubled asc`, `[[4] [6]]`},
		{"union", `LeftEvents | project n | union (RightEvents | project n) | order by n asc`, `[[1] [2] [3]]`},
		{"join inner", `LeftEvents | join kind=inner (RightEvents) on host | project user,user1`, `[[alice carol]]`},
		{"join leftouter", `LeftEvents | join kind=leftouter (RightEvents) on host | project user,user1 | order by user asc`, `[[alice carol] [bob <nil>]]`},
		{"join rightouter", `LeftEvents | join kind=rightouter (RightEvents) on host | project user,user1`, `[[alice carol]]`},
		{"join fullouter", `LeftEvents | join kind=fullouter (RightEvents) on host | project user,user1 | order by user asc`, `[[alice carol] [bob <nil>]]`},
		{"join leftsemi", `LeftEvents | join kind=leftsemi (RightEvents) on host | project user`, `[[alice]]`},
		{"join leftanti", `LeftEvents | join kind=leftanti (RightEvents) on host | project user`, `[[bob]]`},
		{"lookup inner", `LeftEvents | lookup kind=inner (RightEvents | project host, Peer=user) on host | project user,Peer`, `[[alice carol]]`},
		{"lookup leftouter", `LeftEvents | lookup kind=leftouter (RightEvents | project host, Peer=user) on host | project user,Peer | order by user asc`, `[[alice carol] [bob <nil>]]`},
		{"let", `let threshold=2; let Selected=Events | where n >= threshold; selected | project n | order by n asc`, `[[2] [3]]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, c.query)); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestDocumentedScalarFeaturesExecute(t *testing.T) {
	db := featureDatabase(t)
	cases := []struct{ name, expression, want string }{
		{"arithmetic", `1+2, 5-2, 3*2, 6/2, 7%3`, `[[3 3 6 3 1]]`},
		{"membership", `n in (1,2), n !in (2,3), user in~ ("ALICE"), user !in~ ("BOB")`, `[[1 1 1 1]]`},
		{"range boolean", `n between (1 .. 2), n >= 1 and n <= 2, n == 0 or n == 1`, `[[1 1 1]]`},
		{"strings", `message contains "shell", message startswith "power", message endswith "alpha"`, `[[1 1 1]]`},
		{"terms", `message has "powershell", message has_cs "PowerShell", message hasprefix "power", message hassuffix "shell", message !has "missing", message has_any ("missing","alpha"), message has_all ("alpha","powershell")`, `[[1 1 1 1 1 1 1]]`},
		{"null checks", `isnull(RawData.missing), isnotnull(host), isempty(""), isnotempty(host)`, `[[1 1 1 1]]`},
		{"casts", `toint("42"), tolong("42"), toreal("2.5"), todouble("2.5"), tostring(n)`, `[[42 42 2.5 2.5 1]]`},
		{"conditionals", `iff(n == 1,"yes","no"), coalesce(RawData.missing,"fallback")`, `[[yes fallback]]`},
		{"dynamic", `array_length(items), bag_keys(parse_json('{"a":1}')), bag_has_key(RawData,"host"), set_has_element(items,2)`, `[[2 ["a"] 1 1]]`},
		{"decoding", `base64_decode_tostring("aGk="), url_decode("hello%20world")`, `[[hi hello world]]`},
		{"ipv4", `ipv4_is_private("10.1.2.3"), ipv4_is_in_range("10.1.2.3","10.0.0.0/8")`, `[[1 1]]`},
		{"string helpers", `split("a,b",","), extract("([0-9]+)",1,"x42"), trim(" +"," hi "), replace_string("abc","b","X")`, `[[["a","b"] 42 hi aXc]]`},
		{"clock", `now(), ago(1d), todatetime("2026-01-01")`, `[[2026-01-04T00:00:00.000000000Z 2026-01-03T00:00:00.000000000Z 2026-01-01T00:00:00.000000000Z]]`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			query := `Events | where n == 1 | project ` + c.expression
			if got := fmt.Sprint(featureRows(t, db, query)); got != c.want {
				t.Fatalf("got %s, want %s", got, c.want)
			}
		})
	}
}

func TestExtendedJoinsUseBundledCompiler(t *testing.T) {
	db := featureDatabase(t)
	if _, err := db.Exec(`INSERT INTO events VALUES
 ('2026-01-04T00:00:00.000000000Z','left','{"host":"a","user":"duplicate","n":4}',1),
 ('2026-01-05T00:00:00.000000000Z','right','{"host":"z","user":"unmatched","n":5}',2);`); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ query, want string }{
		{`LeftEvents | join (RightEvents) on host | count`, `[[1]]`},
		{`LeftEvents | join kind=innerunique (RightEvents) on host | count`, `[[1]]`},
		{`LeftEvents | join kind=inner (RightEvents) on host | count`, `[[2]]`},
		{`LeftEvents | join kind=rightsemi (RightEvents) on host | project user`, `[[carol]]`},
		{`LeftEvents | join kind=rightanti (RightEvents) on host | project user`, `[[unmatched]]`},
		{`LeftEvents | join (RightEvents) on $left.host == $right.host | project user,user1`, `[[alice carol]]`},
	} {
		t.Run(c.query, func(t *testing.T) {
			rows := featureRows(t, db, c.query)
			// innerunique may choose either complete duplicate-left representative.
			if c.want == `[[alice carol]]` && fmt.Sprint(rows) == `[[duplicate carol]]` {
				return
			}
			if got := fmt.Sprint(rows); got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestAlignedUnionsUseBundledCompiler(t *testing.T) {
	db := featureDatabase(t)
	for _, c := range []struct{ query, want string }{
		{`LeftEvents | project user,n | union (RightEvents | project n,user) | order by n asc`, `[[alice 1] [bob 2] [carol 3]]`},
		{`union (LeftEvents | project host), (RightEvents | project user) | count`, `[[3]]`},
		{`LeftEvents | project host,n | union kind=inner (RightEvents | project user,n) | order by n asc`, `[[1] [2] [3]]`},
		{`let U=union (LeftEvents | project host), (RightEvents | project user); U | where isnull(host) | project user`, `[[carol]]`},
		{`LeftEvents | project value=n | union (RightEvents | project value=user) | where isnotnull(value_string) | project value_long,value_string`, `[[<nil> carol]]`},
		{`union (LeftEvents | project ok=not(n == 1)), (RightEvents | project n) | where isnull(n) | summarize countif(ok)`, `[[1]]`},
	} {
		t.Run(c.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, c.query)); got != c.want {
				t.Fatalf("got %s want %s", got, c.want)
			}
		})
	}
}

func TestColumnPatternsUseBundledCompiler(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`Events | where n == 1 | project-keep h*,u*`, `[[a alice]]`},
		{`Events | where n == 1 | project host,user,n | project-away h*,u*`, `[[1]]`},
		{`Events | where n == 1 | project host,user,n | project-reorder * desc`, `[[alice 1 a]]`},
		{`Events | where n == 1 | project z=host,attr20=n,attr3=user,attr100=message | project-reorder attr* granny-asc`, `[[alice 1 PowerShell alpha a]]`},
		{`Events | where n == 1 | project ok=not(n == 2), num=n | project-keep o* | where ok | count`, `[[1]]`},
		{`Events | project host,user,n | project-reorder u*,*,u* | where n == 3`, `[[carol a 3]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestSchemaIntrospection(t *testing.T) {
	db := featureDatabase(t)
	for _, tc := range []struct{ query, want string }{
		{`Events | project user,n,items | getschema`, `[[user 0 System.String string] [n 1 System.Int64 long] [items 2 System.Object dynamic]]`},
		{`Events | where n < 0 | project Small=toint(n),Flag=not(false) | getschema | project ColumnName,ColumnType`, `[[Small int] [Flag bool]]`},
		{`datatable(n:long,label:string)[1,"one",2,"two"] | where n == 2 | project label`, `[[two]]`},
		{`datatable(n:int)[] | getschema`, `[[n 0 System.Int32 int]]`},
	} {
		t.Run(tc.query, func(t *testing.T) {
			if got := fmt.Sprint(featureRows(t, db, tc.query)); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}
