package kql

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/kawijayaa/striem/internal/database"
)

func TestCompileUsesKSQLWithStriemTable(t *testing.T) {
	compiled, err := Compile(`Sysmon | where EventType == "1" | project Host | take 10`, time.Now(), TableCatalog{"Sysmon": 42})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{`FROM "main"."events"`, `"dataset_id" = 42`, `LIMIT 10`, `LIMIT 1000`} {
		if !strings.Contains(compiled.SQL, fragment) {
			t.Fatalf("compiled SQL lacks %q: %s", fragment, compiled.SQL)
		}
	}
	if len(compiled.Args) != 0 {
		t.Fatalf("args = %#v", compiled.Args)
	}
	if strings.Join(compiled.Columns, ",") != "Host" {
		t.Fatalf("columns = %v", compiled.Columns)
	}
}

func TestCompiledQueryExecutes(t *testing.T) {
	database, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE events (time_generated TEXT, source TEXT, event_type TEXT, host TEXT, username TEXT, message TEXT, raw_data TEXT, dataset_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(`Events | take 1`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Query(compiled.SQL, compiled.Args...); err != nil {
		t.Fatalf("query error = %v\nSQL: %s\nArgs: %#v", err, compiled.SQL, compiled.Args)
	}
}

func TestCompiledUnionExecutes(t *testing.T) {
	database, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE events (time_generated TEXT, source TEXT, event_type TEXT, host TEXT, username TEXT, message TEXT, raw_data TEXT, dataset_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(`Events | project Host | union (Events | project Host)`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Query(compiled.SQL, compiled.Args...); err != nil {
		t.Fatalf("query error = %v\nSQL: %s", err, compiled.SQL)
	}
}

func TestCompiledJoinExecutes(t *testing.T) {
	database, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE events (time_generated TEXT, source TEXT, event_type TEXT, host TEXT, username TEXT, message TEXT, raw_data TEXT, dataset_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(`Events | project User, Host | join kind=inner (Events | project User, Host, Message) on User`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Query(compiled.SQL, compiled.Args...); err != nil {
		t.Fatalf("query error = %v\nSQL: %s", err, compiled.SQL)
	}
	if got := strings.Join(compiled.Columns, ","); got != "User,Host,User1,Host1,Message" {
		t.Fatalf("columns = %q", got)
	}
}

func TestCompileLowersDynamicPropertyAccess(t *testing.T) {
	compiled, err := Compile(`Events | project Command=tostring(RawData.process.name)`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SQL, `json_extract("RawData", '$."process"."name"')`) || strings.Contains(compiled.SQL, `json_each("RawData")`) {
		t.Fatalf("compiled SQL = %s", compiled.SQL)
	}
}

func TestDynamicArrayIndexExecutes(t *testing.T) {
	database, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE events (time_generated TEXT, source TEXT, event_type TEXT, host TEXT, username TEXT, message TEXT, raw_data TEXT, dataset_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO events(raw_data) VALUES ('{"tags":["first"],"field.with.dots":"value"}')`); err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(`Events | project First=tostring(RawData.tags[0]), Value=tostring(RawData["field.with.dots"])`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	row := database.QueryRow(compiled.SQL, compiled.Args...)
	var first, value string
	if err := row.Scan(&first, &value); err != nil {
		t.Fatalf("query error = %v\nSQL: %s", err, compiled.SQL)
	}
	if first != "first" || value != "value" {
		t.Fatalf("values = %q, %q", first, value)
	}
}

func TestMultiValueOperatorsExecute(t *testing.T) {
	database, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE events (time_generated TEXT, source TEXT, event_type TEXT, host TEXT, username TEXT, message TEXT, raw_data TEXT, dataset_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO events(host, raw_data) VALUES ('pc-1', '{"items":[1,2,3]}')`); err != nil {
		t.Fatal(err)
	}

	expanded, err := Compile(`Events | mv-expand with_itemindex=ItemIndex Item=RawData.items limit 2 | project Item, ItemIndex | order by ItemIndex asc`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := database.Query(expanded.SQL, expanded.Args...)
	if err != nil {
		t.Fatalf("mv-expand error = %v\nSQL: %s", err, expanded.SQL)
	}
	defer rows.Close()
	var expandedValues []int64
	for rows.Next() {
		var item, index int64
		if err := rows.Scan(&item, &index); err != nil {
			t.Fatal(err)
		}
		expandedValues = append(expandedValues, item, index)
	}
	if got := fmt.Sprint(expandedValues); got != "[1 0 2 1]" {
		t.Fatalf("expanded values = %s", got)
	}

	applied, err := Compile(`Events | mv-apply Item=RawData.items on (where Item > 1 | extend Doubled=Item * 2) | project Item, Doubled | order by Item asc`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rows, err = database.Query(applied.SQL, applied.Args...)
	if err != nil {
		t.Fatalf("mv-apply error = %v\nSQL: %s", err, applied.SQL)
	}
	defer rows.Close()
	var appliedValues []int64
	for rows.Next() {
		var item, doubled int64
		if err := rows.Scan(&item, &doubled); err != nil {
			t.Fatal(err)
		}
		appliedValues = append(appliedValues, item, doubled)
	}
	if got := fmt.Sprint(appliedValues); got != "[2 4 3 6]" {
		t.Fatalf("applied values = %s", got)
	}
}

func TestNewKSQLScalarOperatorsExecute(t *testing.T) {
	database, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE events (time_generated TEXT, source TEXT, event_type TEXT, host TEXT, username TEXT, message TEXT, raw_data TEXT, dataset_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO events(username, message, raw_data) VALUES ('Admin', 'PowerShell alpha-user', '{}'), ('guest', 'benign', '{}')`); err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(`Events | where User in~ ("ADMIN") and Message has_all ("powershell", "alpha") | project User`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var user string
	if err := database.QueryRow(compiled.SQL, compiled.Args...).Scan(&user); err != nil {
		t.Fatalf("query error = %v\nSQL: %s", err, compiled.SQL)
	}
	if user != "Admin" {
		t.Fatalf("user = %q", user)
	}
}

func TestNewKSQLPipelineOperatorsExecute(t *testing.T) {
	database, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE events (time_generated TEXT, source TEXT, event_type TEXT, host TEXT, username TEXT, message TEXT, raw_data TEXT, dataset_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO events(host, raw_data) VALUES ('pc-1', '{}'), ('pc-2', '{}')`); err != nil {
		t.Fatal(err)
	}

	sampled, err := Compile(`Events | sample 1 | project Host`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var host string
	if err := database.QueryRow(sampled.SQL, sampled.Args...).Scan(&host); err != nil {
		t.Fatalf("sample error = %v\nSQL: %s", err, sampled.SQL)
	}

	aliased, err := Compile(`Events | as LeftSide | join kind=inner (LeftSide) on Host | project Host`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var count int
	rows, err := database.Query(aliased.SQL, aliased.Args...)
	if err != nil {
		t.Fatalf("as error = %v\nSQL: %s", err, aliased.SQL)
	}
	defer rows.Close()
	for rows.Next() {
		count++
	}
	if count != 2 {
		t.Fatalf("aliased rows = %d", count)
	}
}

func TestSchemaAwareOperatorsExecute(t *testing.T) {
	database, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE events (time_generated TEXT, source TEXT, event_type TEXT, host TEXT, username TEXT, message TEXT, raw_data TEXT, dataset_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO events(host, username, message, raw_data) VALUES ('old', 'alice', 'PowerShell launched', '{}')`); err != nil {
		t.Fatal(err)
	}

	replaced, err := Compile(`Events | extend Host="new" | project Host`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var host string
	if err := database.QueryRow(replaced.SQL, replaced.Args...).Scan(&host); err != nil {
		t.Fatalf("extend error = %v\nSQL: %s", err, replaced.SQL)
	}
	if host != "new" {
		t.Fatalf("Host = %q", host)
	}

	searched, err := Compile(`Events | search "powershell" | project Host`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(searched.SQL, searched.Args...).Scan(&host); err != nil || host != "old" {
		t.Fatalf("search result = %q, %v\nSQL: %s", host, err, searched.SQL)
	}

	projected, err := Compile(`Events | project-away Source, EventType | project-rename Computer=Host | project-keep TimeGenerated, Computer, RawData`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(projected.Columns, ","); got != "TimeGenerated,Computer,RawData" {
		t.Fatalf("columns = %q", got)
	}
	if _, dynamic := projected.DynamicColumns["RawData"]; !dynamic {
		t.Fatal("RawData lost its dynamic type")
	}

	aggregated, err := Compile(`Events | summarize Users=make_list(User), UniqueUsers=make_set(User)`, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var users, uniqueUsers string
	if err := database.QueryRow(aggregated.SQL, aggregated.Args...).Scan(&users, &uniqueUsers); err != nil {
		t.Fatalf("aggregate error = %v\nSQL: %s", err, aggregated.SQL)
	}
	if users != `["alice"]` || uniqueUsers != `["alice"]` {
		t.Fatalf("aggregate values = %s, %s", users, uniqueUsers)
	}
	if _, dynamic := aggregated.DynamicColumns["Users"]; !dynamic {
		t.Fatal("make_list output is not dynamic")
	}
}

func TestStriemScalarFunctionsExecute(t *testing.T) {
	database, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE events (time_generated TEXT, source TEXT, event_type TEXT, host TEXT, username TEXT, message TEXT, raw_data TEXT, dataset_id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO events(event_type, host, username, message, raw_data) VALUES (NULL, 'pc-1', 'alice', '', '{}')`); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, time.August, 7, 12, 30, 0, 0, time.FixedZone("test", 10*60*60))
	compiled, err := Compile(`Events
| extend Current=now(), Prior=ago(1d), Fixed=todatetime("2026-01-02T03:04:05Z"), Integer=toint("42"), Real=toreal("2.5"), Missing=isnull(EventType), Present=isnotnull(Host), Blank=isempty(Message), Named=isnotempty(User)
| project Current, Prior, Fixed, Integer, Real, Missing, Present, Blank, Named`, now)
	if err != nil {
		t.Fatal(err)
	}
	var current, prior, fixed string
	var integer int64
	var real float64
	var missing, present, blank, named bool
	if err := database.QueryRow(compiled.SQL, compiled.Args...).Scan(&current, &prior, &fixed, &integer, &real, &missing, &present, &blank, &named); err != nil {
		t.Fatalf("query error = %v\nSQL: %s", err, compiled.SQL)
	}
	if current != "2026-08-07T02:30:00.000000000Z" || prior != "2026-08-06T02:30:00.000000000Z" || fixed != "2026-01-02T03:04:05.000000000Z" {
		t.Fatalf("datetimes = %q, %q, %q", current, prior, fixed)
	}
	if integer != 42 || real != 2.5 || !missing || !present || !blank || !named {
		t.Fatalf("scalar values = %d, %f, %t, %t, %t, %t", integer, real, missing, present, blank, named)
	}
}

func TestStriemScalarFunctionsRejectInvalidArguments(t *testing.T) {
	queries := []string{
		`Events | extend Value=now(1)`,
		`Events | extend Value=ago()`,
		`Events | extend Value=ago(Host)`,
		`Events | extend Value=ago(1x)`,
		`Events | extend Value=toint()`,
		`Events | extend Value=isnull()`,
		`Events | extend Value=isempty()`,
	}
	for _, query := range queries {
		if _, err := Compile(query, time.Now()); err == nil {
			t.Errorf("Compile(%q) succeeded", query)
		}
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		value string
		want  time.Duration
	}{
		{value: "90m", want: 90 * time.Minute},
		{value: "1d", want: 24 * time.Hour},
		{value: "2.5d", want: 60 * time.Hour},
		{value: "1w", want: 7 * 24 * time.Hour},
	}
	for _, test := range tests {
		got, err := parseDuration(test.value)
		if err != nil || got != test.want {
			t.Errorf("parseDuration(%q) = %s, %v, want %s", test.value, got, err, test.want)
		}
	}
	for _, value := range []string{"", "1x", "999999999999999999999w"} {
		if _, err := parseDuration(value); err == nil {
			t.Errorf("parseDuration(%q) succeeded", value)
		}
	}
}

func TestSchemaBindingRejectsUnknownColumn(t *testing.T) {
	_, err := Compile(`Events | where Missing == 1`, time.Now())
	if err == nil || !strings.Contains(err.Error(), "unknown column Missing") {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileReturnsLibraryDiagnostic(t *testing.T) {
	_, err := Compile(`Events | where`, time.Now())
	queryError, ok := err.(*Error)
	if !ok || queryError.Line < 1 || queryError.Column < 1 {
		t.Fatalf("error = %#v", err)
	}
}

func TestCompileRejectsUnknownTable(t *testing.T) {
	_, err := Compile(`Missing | take 1`, time.Now())
	if err == nil || !strings.Contains(err.Error(), `unknown table "Missing"`) {
		t.Fatalf("error = %v", err)
	}
}

func TestCompileAddsLeadingSearchFullTextPrefilter(t *testing.T) {
	compiled, err := Compile(`Suricata | search "10.10.1.9" | where EventType == "alert" | project Host`, time.Now(), CompileConfig{
		Tables: TableCatalog{"Suricata": 42}, FullTextIndex: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"dataset_id" = 42`,
		`"rowid" IN (SELECT "rowid" FROM "events_fts" WHERE "events_fts" MATCH ?)`,
		`kql_regex`,
	} {
		if !strings.Contains(compiled.SQL, fragment) {
			t.Fatalf("compiled SQL lacks %q: %s", fragment, compiled.SQL)
		}
	}
	if got := fmt.Sprint(compiled.Args); got != `["10.10.1.9"]` {
		t.Fatalf("args = %s", got)
	}
}

func TestCompileSkipsUnsupportedFullTextPrefilters(t *testing.T) {
	for _, query := range []string{
		`Events | where Source == "fixture" | search "powershell"`,
		`Events | search "ab"`,
		`Events | search "a\u0000bc"`,
		`Events | project Host | union (Events | search "powershell" | project Host)`,
	} {
		compiled, err := Compile(query, time.Now(), CompileConfig{FullTextIndex: true})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(compiled.SQL, "events_fts") {
			t.Fatalf("unsupported query received FTS prefilter:\n%s\nSQL: %s", query, compiled.SQL)
		}
	}
}

func TestCompileOrdersMultipleFullTextArgumentsBySQLPosition(t *testing.T) {
	compiled, err := Compile(`let First = Events | search "alpha"; Events | search "bravo" | union (First)`, time.Now(), CompileConfig{FullTextIndex: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(compiled.Args); got != `["bravo" "alpha"]` {
		t.Fatalf("args = %s, want SQL rendering order", got)
	}
}
