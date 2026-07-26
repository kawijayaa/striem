package kql

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/kawijayaa/striem/internal/database"
)

func TestParseAndCompileHunt(t *testing.T) {
	query, err := Parse(`Events
| where TimeGenerated > ago(15m) and Source == "sysmon"
| extend CommandLine = tostring(RawData.process.command_line)
| where CommandLine contains "powershell"
| project TimeGenerated, Host, CommandLine
| order by TimeGenerated desc
| take 100`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	compiled, err := Compile(query, time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if !strings.Contains(compiled.SQL, "json_extract") || !strings.Contains(compiled.SQL, "ORDER BY") {
		t.Fatalf("compiled SQL lacks expected operations: %s", compiled.SQL)
	}
	if got, want := compiled.Columns, []string{"TimeGenerated", "Host", "CommandLine"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("columns = %v, want %v", got, want)
	}
	if len(compiled.Args) != 6 {
		t.Fatalf("args = %v, want 6 values", compiled.Args)
	}
}

func TestCompileUnquotedRFC3339Datetime(t *testing.T) {
	query, err := Parse(`UAL
| where TimeGenerated >= datetime(2026-08-05T02:01:40.000Z) and TimeGenerated < datetime(2027-08-05T05:38:00.001Z)
| where EventType contains "User"
| project TimeGenerated, RawData.AuditData.Workload, RawData.AuditData.Operation`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	compiled, err := Compile(query, time.Now(), TableCatalog{"UAL": 1})
	if err != nil {
		t.Fatalf("Compile() error = %v", err)
	}
	if got, want := strings.Join(compiled.Columns, ","), "TimeGenerated,Workload,Operation"; got != want {
		t.Fatalf("columns = %q, want %q", got, want)
	}
	for _, want := range []string{"2026-08-05T02:01:40.000000000Z", "2027-08-05T05:38:00.001000000Z"} {
		found := false
		for _, argument := range compiled.Args {
			if argument == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("compiled args = %#v, want %q", compiled.Args, want)
		}
	}
}

func TestNotIncludesComparison(t *testing.T) {
	query, err := Parse(`Events | where not Source == "sysmon" and Host == "pc-1"`)
	if err != nil {
		t.Fatal(err)
	}
	where := query.Operators[0].(WhereOperator)
	root := where.Expression.(BinaryExpression)
	if root.Operator != "and" {
		t.Fatalf("root operator = %q, want and", root.Operator)
	}
	unary, ok := root.Left.(UnaryExpression)
	if !ok {
		t.Fatalf("left = %#v, want unary expression", root.Left)
	}
	if comparison, ok := unary.Operand.(BinaryExpression); !ok || comparison.Operator != "==" {
		t.Fatalf("not operand = %#v, want equality", unary.Operand)
	}
}

func TestCompileSummarize(t *testing.T) {
	query, err := Parse(`Events | summarize Events=count(), Hosts=dcount(Host) by Source | order by Events desc`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SQL, "COUNT(DISTINCT") || !strings.Contains(compiled.SQL, "GROUP BY 1") {
		t.Fatalf("compiled SQL = %s", compiled.SQL)
	}
}

func TestCompilePriorityInvestigationFeatures(t *testing.T) {
	query, err := Parse(`let suspicious = Events | search "powershell" | project Host, User, TimeGenerated, RawData;
suspicious
| summarize arg_max(TimeGenerated, *) by Host`)
	if err != nil {
		t.Fatal(err)
	}
	if query.Bindings[0].Tabular == nil {
		t.Fatal("tabular binding was parsed as a scalar")
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"kql_has", "ROW_NUMBER() OVER", "ORDER BY (", "DESC"} {
		if !strings.Contains(compiled.SQL, fragment) {
			t.Fatalf("compiled SQL lacks %q: %s", fragment, compiled.SQL)
		}
	}
	if got := strings.Join(compiled.Columns, ","); got != "Host,User,TimeGenerated,RawData" {
		t.Fatalf("columns = %q", got)
	}
	if _, dynamic := compiled.DynamicColumns["RawData"]; !dynamic {
		t.Fatal("RawData lost its dynamic result type")
	}
}

func TestCompileCollectionAggregates(t *testing.T) {
	query, err := Parse(`Events | summarize Users=make_set(User), Sequence=make_list(User), Sample=take_any(User) by Host`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"kql_make_set(", "kql_make_list(", "MIN("} {
		if !strings.Contains(compiled.SQL, fragment) {
			t.Fatalf("compiled SQL lacks %q: %s", fragment, compiled.SQL)
		}
	}
	for _, column := range []string{"Users", "Sequence"} {
		if _, dynamic := compiled.DynamicColumns[column]; !dynamic {
			t.Fatalf("%s is not marked dynamic", column)
		}
	}
}

func TestCompileDatasetTable(t *testing.T) {
	query, err := Parse(`Sysmon | where EventType == "1" | take 10`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now(), TableCatalog{"Sysmon": 42})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SQL, "WHERE dataset_id = ?1") || compiled.Args[0] != int64(42) {
		t.Fatalf("compiled table query = %s, %#v", compiled.SQL, compiled.Args)
	}
}

func TestCompileUnionAlignsColumnsByName(t *testing.T) {
	query, err := Parse(`UAL | project Host, Source | union (Sysmon | project Source, Host)`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now(), TableCatalog{"UAL": 1, "Sysmon": 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SQL, "UNION ALL") || strings.Join(compiled.Columns, ",") != "Host,Source" {
		t.Fatalf("compiled union = %s, columns %v", compiled.SQL, compiled.Columns)
	}
	if got := compiled.Args[:2]; got[0] != int64(1) || got[1] != int64(2) {
		t.Fatalf("union table arguments = %#v", got)
	}
}

func TestCompileJoinSuffixesRightColumns(t *testing.T) {
	query, err := Parse(`UAL
| project User, Host
| join kind=leftouter (Sysmon | project User, Host, Message) on User
| project Host, Host1, Message`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now(), TableCatalog{"UAL": 1, "Sysmon": 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SQL, "LEFT OUTER JOIN") || strings.Join(compiled.Columns, ",") != "Host,Host1,Message" {
		t.Fatalf("compiled join = %s, columns %v", compiled.SQL, compiled.Columns)
	}
}

func TestCompileParsedDynamicProperty(t *testing.T) {
	query, err := Parse(`Events | extend Audit=parse_json(RawData.AuditData) | extend ClientIP=tostring(Audit.ClientIP) | project ClientIP`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SQL, "json(") || strings.Count(compiled.SQL, "json_extract") < 2 {
		t.Fatalf("compiled SQL does not parse nested JSON: %s", compiled.SQL)
	}
}

func TestCompileBracketDynamicProperty(t *testing.T) {
	query, err := Parse(`Events | extend Value=tostring(RawData["field.with.dots"]) | project Value`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, argument := range compiled.Args {
		if argument == `$."field.with.dots"` {
			found = true
		}
	}
	if !found {
		t.Fatalf("compiled args = %#v, want quoted JSON path", compiled.Args)
	}
}

func TestExpressionPrecedence(t *testing.T) {
	query, err := Parse(`Events | where Source == "a" or Source == "b" and Host == "c"`)
	if err != nil {
		t.Fatal(err)
	}
	where := query.Operators[0].(WhereOperator)
	root, ok := where.Expression.(BinaryExpression)
	if !ok || root.Operator != "or" {
		t.Fatalf("root = %#v, want or expression", where.Expression)
	}
	right, ok := root.Right.(BinaryExpression)
	if !ok || right.Operator != "and" {
		t.Fatalf("right = %#v, want and expression", root.Right)
	}
}

func TestArithmeticPrecedenceAndUnarySign(t *testing.T) {
	query, err := Parse(`Events | extend Score=-1 + 2 * 3`)
	if err != nil {
		t.Fatal(err)
	}
	extend := query.Operators[0].(ExtendOperator)
	root, ok := extend.Items[0].Expression.(BinaryExpression)
	if !ok || root.Operator != "+" {
		t.Fatalf("root = %#v, want addition", extend.Items[0].Expression)
	}
	if unary, ok := root.Left.(UnaryExpression); !ok || unary.Operator != "-" {
		t.Fatalf("left = %#v, want unary minus", root.Left)
	}
	if product, ok := root.Right.(BinaryExpression); !ok || product.Operator != "*" {
		t.Fatalf("right = %#v, want multiplication", root.Right)
	}
}

func TestCompileTopAndNullComparison(t *testing.T) {
	query, err := Parse(`Events | where User != null | top 5 by TimeGenerated desc`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SQL, `q."User" IS NOT NULL`) || !strings.Contains(compiled.SQL, `ORDER BY q."TimeGenerated" DESC LIMIT`) {
		t.Fatalf("compiled SQL = %s", compiled.SQL)
	}
	for _, argument := range compiled.Args {
		if argument == nil {
			t.Fatalf("null comparison unnecessarily bound a nil argument: %#v", compiled.Args)
		}
	}
}

func TestDayAndWeekDurations(t *testing.T) {
	query, err := Parse(`Events | where TimeGenerated > ago(1w)`)
	if err != nil {
		t.Fatal(err)
	}
	where := query.Operators[0].(WhereOperator)
	ago := where.Expression.(BinaryExpression).Right.(FunctionExpression)
	duration, ok := ago.Arguments[0].(DurationExpression)
	if !ok || duration.Value != 7*24*time.Hour {
		t.Fatalf("duration = %#v, want one week", ago.Arguments[0])
	}
}

func TestCompileScalarHelpers(t *testing.T) {
	query, err := Parse(`Events | extend Label=strcat(coalesce(User, "unknown"), ":", substring(Message, 0, 4)), Size=strlen(Message), State=iff(Message == null, "missing", "present")`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"COALESCE(", " || ", "substr(", "length(", "CASE WHEN", "IS NULL"} {
		if !strings.Contains(compiled.SQL, fragment) {
			t.Fatalf("compiled SQL lacks %q: %s", fragment, compiled.SQL)
		}
	}
}

func TestCompileArrayIndexAndMVExpand(t *testing.T) {
	query, err := Parse(`Events | extend First=RawData.items[0] | mv-expand Item=RawData.items limit 16 | project First, Item`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SQL, "json_each") || strings.Join(compiled.Columns, ",") != "First,Item" {
		t.Fatalf("compiled mv-expand = %s, columns=%v", compiled.SQL, compiled.Columns)
	}
	foundIndex := false
	for _, argument := range compiled.Args {
		if argument == `$."items"[0]` {
			foundIndex = true
		}
	}
	if !foundIndex {
		t.Fatalf("compiled args = %#v, want array JSON path", compiled.Args)
	}
}

func TestParseMVApply(t *testing.T) {
	query, err := Parse(`Events | mv-apply Item=RawData.items on (where Item > 1 | where Item < 10 | summarize Matches=count(), Total=sum(Item))`)
	if err != nil {
		t.Fatal(err)
	}
	operator, ok := query.Operators[0].(MVApplyOperator)
	if !ok {
		t.Fatalf("operator = %T, want MVApplyOperator", query.Operators[0])
	}
	if operator.Alias != "Item" || len(operator.Wheres) != 2 || len(operator.Aggregates) != 2 {
		t.Fatalf("operator = %#v", operator)
	}
	limit, ok := operator.Limit.(LiteralExpression)
	if !ok || limit.Value != float64(128) {
		t.Fatalf("default limit = %#v, want 128", operator.Limit)
	}
}

func TestCompileMVApplyPreservesInputRows(t *testing.T) {
	query, err := Parse(`Events
| project Host, RawData
| mv-apply Item=RawData limit 2 on (where Item > 1 | summarize Matches=count(), Total=sum(Item))`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(compiled.Columns, ","); got != "Host,RawData,Matches,Total" {
		t.Fatalf("columns = %q", got)
	}
	for _, fragment := range []string{"json_each", "LIMIT", "COUNT(*)", "SUM("} {
		if !strings.Contains(compiled.SQL, fragment) {
			t.Fatalf("compiled SQL lacks %q: %s", fragment, compiled.SQL)
		}
	}
	if _, dynamic := compiled.DynamicColumns["RawData"]; !dynamic {
		t.Fatal("RawData lost its dynamic result type")
	}

	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE events (
		time_generated TEXT, source TEXT, event_type TEXT, host TEXT,
		username TEXT, message TEXT, raw_data TEXT, dataset_id INTEGER
	)`); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		host string
		data string
	}{
		{host: "first", data: `[1,2,3]`},
		{host: "second", data: `[10,20]`},
		{host: "empty", data: `[]`},
	} {
		if _, err = db.Exec(`INSERT INTO events (host, raw_data) VALUES (?, ?)`, row.host, row.data); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.Query(compiled.SQL, compiled.Args...)
	if err != nil {
		t.Fatalf("query compiled SQL: %v\n%s", err, compiled.SQL)
	}
	defer rows.Close()
	want := map[string]struct {
		matches int64
		total   sql.NullFloat64
	}{
		"first":  {matches: 1, total: sql.NullFloat64{Float64: 2, Valid: true}},
		"second": {matches: 2, total: sql.NullFloat64{Float64: 30, Valid: true}},
		"empty":  {matches: 0},
	}
	seen := 0
	for rows.Next() {
		var host, rawData string
		var matches int64
		var total sql.NullFloat64
		if err = rows.Scan(&host, &rawData, &matches, &total); err != nil {
			t.Fatal(err)
		}
		expected, exists := want[host]
		if !exists || matches != expected.matches || total != expected.total {
			t.Fatalf("row %q = matches %d, total %#v", host, matches, total)
		}
		seen++
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != len(want) {
		t.Fatalf("rows = %d, want %d", seen, len(want))
	}
}

func TestCompileMVApplyCollectionAggregateIsDynamic(t *testing.T) {
	query, err := Parse(`Events | mv-apply Item=RawData.items on (summarize Items=make_list(Item))`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, dynamic := compiled.DynamicColumns["Items"]; !dynamic {
		t.Fatal("mv-apply make_list output is not dynamic")
	}
	if got := compiled.Args[len(compiled.Args)-3]; got != int64(128) {
		t.Fatalf("default expansion limit = %#v, want 128", got)
	}
	if got := compiled.Args[len(compiled.Args)-2]; got != int64(1000) {
		t.Fatalf("input row limit = %#v, want 1000", got)
	}
}

func TestCompileSecurityOperatorsAndLeftAnti(t *testing.T) {
	query, err := Parse(`UAL
| where Message has "powershell" and User in~ ("ADMIN", "analyst") and Source !in ("test")
| project Host
| join kind=leftanti (Sysmon | project Host) on Host`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now(), TableCatalog{"UAL": 1, "Sysmon": 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"kql_has", "lower(CAST", "NOT IN", "NOT EXISTS"} {
		if !strings.Contains(compiled.SQL, fragment) {
			t.Fatalf("compiled SQL lacks %q: %s", fragment, compiled.SQL)
		}
	}
}

func TestCompileAggregateInsideScalarFunction(t *testing.T) {
	query, err := Parse(`Events | summarize Total=tostring(count())`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SQL, "CAST(COUNT(*) AS TEXT)") {
		t.Fatalf("compiled SQL = %s", compiled.SQL)
	}
}

func TestCompileScalarVariables(t *testing.T) {
	query, err := Parse(`let base = 2;
let threshold = base + 3;
let sourceName = "sysmon";
let fallback = coalesce(null, "unknown");
Events
| where Source == sourceName
| extend Display = coalesce(User, fallback)
| top threshold by TimeGenerated`)
	if err != nil {
		t.Fatal(err)
	}
	if len(query.Bindings) != 4 {
		t.Fatalf("bindings = %d, want 4", len(query.Bindings))
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"COALESCE", `q."Source" = (?`, `ORDER BY q."TimeGenerated" DESC LIMIT`} {
		if !strings.Contains(compiled.SQL, fragment) {
			t.Fatalf("compiled SQL lacks %q: %s", fragment, compiled.SQL)
		}
	}
	if got := compiled.Args[len(compiled.Args)-2]; got != int64(5) {
		t.Fatalf("top argument = %#v, want 5", got)
	}
}

func TestCompileScalarVariableWithAggregation(t *testing.T) {
	query, err := Parse(`let multiplier = 2; Events | summarize Adjusted=count() * multiplier`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SQL, `COUNT(*) * (?1)`) {
		t.Fatalf("compiled SQL = %s", compiled.SQL)
	}
}

func TestUsefulDiagnostics(t *testing.T) {
	tests := []struct {
		query   string
		message string
	}{
		{`Other | take 1`, "unknown table"},
		{`Events | join Other`, "expected '(' before join query"},
		{`Events | where Missing == 1`, "unknown column"},
		{`Events | extend RawData.foo`, "require a column name"},
		{`Events | take 1001`, "between 1 and 1000"},
		{`Events | mv-expand Item=RawData.items limit 129`, "cannot exceed 128"},
		{`Events | mv-apply RawData on (summarize Count=count())`, "requires an alias"},
		{`Events | mv-apply Item=RawData, Other=RawData on (summarize Count=count())`, "exactly one"},
		{`Events | mv-apply Item=RawData limit 129 on (summarize Count=count())`, "cannot exceed 128"},
		{`Events | mv-apply Item=RawData on (where Item > 0)`, "followed by summarize"},
		{`Events | mv-apply Item=RawData on (extend Value=Item | summarize Count=count())`, "supports only where"},
		{`Events | mv-apply Item=RawData on (summarize Count=count() by Item)`, "does not support 'by'"},
		{`Events | mv-apply Item=RawData on (summarize arg_max(Item, *))`, "not supported in mv-apply"},
		{`Events | mv-apply Item=RawData on (summarize Host=count())`, "conflicts with an input column"},
		{`Events | mv-apply Item=RawData on (summarize host=count())`, "conflicts with an input column"},
		{`Events | mv-apply Item=RawData on (summarize Total=count(), Total=sum(Item))`, "specified more than once"},
		{`Events | top 10 TimeGenerated`, "expected 'by'"},
		{`Events | summarize Host`, "grouped or aggregated"},
		{`Events | summarize Value=sum(count())`, "cannot be nested"},
		{`Events | extend Value=substring(Message)`, "expects 2 or 3"},
		{`let value = 1 Events | take 1`, "expected ';'"},
		{`let subset = Missing | where Source == "x"; subset | take 1`, "unknown table"},
		{`Events | summarize arg_max(TimeGenerated, Host)`, "requires '*'"},
		{`Events | summarize Latest=arg_max(TimeGenerated, *)`, "cannot have a column alias"},
		{`let value = 1; let value = 2; Events | take 1`, "declared more than once"},
		{`let Host = "x"; Events | take 1`, "conflicts with a table column"},
		{`let first = second; let second = 2; Events | take first`, "unknown column or variable"},
		{`let rows = 1.5; Events | take rows`, "constant integer"},
	}
	for _, test := range tests {
		t.Run(test.message, func(t *testing.T) {
			query, err := Parse(test.query)
			if err == nil {
				_, err = Compile(query, time.Now())
			}
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want message containing %q", err, test.message)
			}
			var diagnostic *Error
			if !errors.As(err, &diagnostic) || diagnostic.Line < 1 || diagnostic.Column < 1 {
				t.Fatalf("error has no source position: %v", err)
			}
		})
	}
}

func TestMVApplyAggregateLimit(t *testing.T) {
	aggregates := make([]string, 33)
	for index := range aggregates {
		aggregates[index] = fmt.Sprintf("Value%d=count()", index)
	}
	query, err := Parse("Events | mv-apply Item=RawData on (summarize " + strings.Join(aggregates, ", ") + ")")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Compile(query, time.Now()); err == nil || !strings.Contains(err.Error(), "at most 32") {
		t.Fatalf("aggregate limit error = %v", err)
	}
}

func TestParseSimplePatterns(t *testing.T) {
	query, err := Parse(`Events | parse-where kind=simple Message with * "user=" ParsedUser:string ", count=" Total:long ", ratio=" Ratio:real`)
	if err != nil {
		t.Fatal(err)
	}
	operator, ok := query.Operators[0].(ParseOperator)
	if !ok || !operator.Where || len(operator.Pattern) != 7 {
		t.Fatalf("operator = %#v", query.Operators[0])
	}

	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(compiled.Columns[len(compiled.Columns)-3:], ","); got != "ParsedUser,Total,Ratio" {
		t.Fatalf("capture columns = %q", got)
	}
	for _, fragment := range []string{"kql_parse(", "json_valid", "json_extract", "IS NOT NULL"} {
		if !strings.Contains(compiled.SQL, fragment) {
			t.Fatalf("compiled SQL lacks %q: %s", fragment, compiled.SQL)
		}
	}
	if compiled.Args[1] != "string,long,real" {
		t.Fatalf("parse type descriptor = %#v", compiled.Args[1])
	}
}

func TestCompileAndExecuteParseAndParseWhere(t *testing.T) {
	db := openKQLTestDatabase(t)
	defer db.Close()
	for _, message := range []string{"user=alice, count=42, ratio=1.5", "does not match"} {
		if _, err := db.Exec(`INSERT INTO events (message) VALUES (?)`, message); err != nil {
			t.Fatal(err)
		}
	}

	for _, test := range []struct {
		operator string
		rows     int
	}{
		{operator: "parse", rows: 2},
		{operator: "parse-where", rows: 1},
	} {
		query, err := Parse(`Events | ` + test.operator + ` Message with "user=" ParsedUser:string ", count=" Total:long ", ratio=" Ratio:real | project ParsedUser, Total, Ratio`)
		if err != nil {
			t.Fatal(err)
		}
		compiled, err := Compile(query, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		rows, err := db.Query(compiled.SQL, compiled.Args...)
		if err != nil {
			t.Fatalf("execute %s: %v\n%s", test.operator, err, compiled.SQL)
		}
		count := 0
		for rows.Next() {
			var user sql.NullString
			var total sql.NullInt64
			var ratio sql.NullFloat64
			if err = rows.Scan(&user, &total, &ratio); err != nil {
				t.Fatal(err)
			}
			if count == 0 && (!user.Valid || user.String != "alice" || !total.Valid || total.Int64 != 42 || !ratio.Valid || ratio.Float64 != 1.5) {
				t.Fatalf("parsed row = %#v, %#v, %#v", user, total, ratio)
			}
			count++
		}
		if err = rows.Close(); err != nil {
			t.Fatal(err)
		}
		if count != test.rows {
			t.Fatalf("%s rows = %d, want %d", test.operator, count, test.rows)
		}
	}
}

func TestCompileProjectAwayAndSimultaneousRename(t *testing.T) {
	query, err := Parse(`Events | project-away Raw*, *Type, Mess* | project-rename User=Host, Host=User`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(compiled.Columns, ","); got != "TimeGenerated,Source,User,Host" {
		t.Fatalf("columns = %q", got)
	}
	if !strings.Contains(compiled.SQL, `q."Host" AS "User"`) || !strings.Contains(compiled.SQL, `q."User" AS "Host"`) {
		t.Fatalf("rename is not simultaneous: %s", compiled.SQL)
	}
	if len(compiled.DynamicColumns) != 0 {
		t.Fatalf("dynamic columns = %#v", compiled.DynamicColumns)
	}
}

func TestCompileBagUnpackSchemaAndDynamicMetadata(t *testing.T) {
	query, err := Parse(`Events | evaluate bag_unpack(RawData) : (*, Name:string, Count:long, Ratio:real, Details:dynamic) | project Host, Name, Count, Ratio, Details`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(compiled.Columns, ","); got != "Host,Name,Count,Ratio,Details" {
		t.Fatalf("columns = %q", got)
	}
	if _, dynamic := compiled.DynamicColumns["Details"]; !dynamic {
		t.Fatal("dynamic bag output is not marked dynamic")
	}
	for _, fragment := range []string{"CASE WHEN json_valid", "json_extract", "json_type", "json_quote"} {
		if !strings.Contains(compiled.SQL, fragment) {
			t.Fatalf("compiled SQL lacks %q: %s", fragment, compiled.SQL)
		}
	}

	prefixed, err := Parse(`Events | evaluate bag_unpack(RawData, "bag_") : (*, bag_Name:string)`)
	if err != nil {
		t.Fatal(err)
	}
	prefixedCompiled, err := Compile(prefixed, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range prefixedCompiled.Columns {
		if column == "RawData" {
			t.Fatal("bag_unpack retained its input dynamic column")
		}
	}
	if prefixedCompiled.Args[0] != `$."Name"` {
		t.Fatalf("prefixed property path = %#v", prefixedCompiled.Args[0])
	}
}

func TestExecuteBagUnpackGuardsMalformedJSON(t *testing.T) {
	db := openKQLTestDatabase(t)
	defer db.Close()
	for _, row := range []struct {
		host string
		data string
	}{
		{host: "valid", data: `{"Name":"alice","Count":7,"Ratio":2.5,"Details":{"active":true}}`},
		{host: "invalid", data: `{broken`},
	} {
		if _, err := db.Exec(`INSERT INTO events (host, raw_data) VALUES (?, ?)`, row.host, row.data); err != nil {
			t.Fatal(err)
		}
	}
	query, err := Parse(`Events | evaluate bag_unpack(RawData) : (*, Name:string, Count:long, Ratio:real, Details:dynamic) | project Host, Name, Count, Ratio, Details | order by Host desc`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(compiled.SQL, compiled.Args...)
	if err != nil {
		t.Fatalf("execute bag_unpack: %v\n%s", err, compiled.SQL)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("missing valid row")
	}
	var host string
	var name, details sql.NullString
	var count sql.NullInt64
	var ratio sql.NullFloat64
	if err = rows.Scan(&host, &name, &count, &ratio, &details); err != nil {
		t.Fatal(err)
	}
	if host != "valid" || name.String != "alice" || count.Int64 != 7 || ratio.Float64 != 2.5 || details.String != `{"active":true}` {
		t.Fatalf("valid row = %q, %#v, %#v, %#v, %#v", host, name, count, ratio, details)
	}
	if !rows.Next() {
		t.Fatal("missing invalid row")
	}
	if err = rows.Scan(&host, &name, &count, &ratio, &details); err != nil {
		t.Fatal(err)
	}
	if host != "invalid" || name.Valid || count.Valid || ratio.Valid || details.Valid {
		t.Fatalf("malformed JSON row was not null guarded: %q, %#v, %#v, %#v, %#v", host, name, count, ratio, details)
	}
}

func TestExecuteBagUnpackRejectsUnsafeNumbers(t *testing.T) {
	db := openKQLTestDatabase(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO events (raw_data) VALUES (?)`, `{"TooLarge":9223372036854775808,"Infinite":1e400}`); err != nil {
		t.Fatal(err)
	}
	query, err := Parse(`Events | evaluate bag_unpack(RawData) : (*, TooLarge:long, Infinite:real) | project TooLarge, Infinite`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var tooLarge sql.NullInt64
	var infinite sql.NullFloat64
	if err = db.QueryRow(compiled.SQL, compiled.Args...).Scan(&tooLarge, &infinite); err != nil {
		t.Fatal(err)
	}
	if tooLarge.Valid || infinite.Valid {
		t.Fatalf("unsafe numbers = %#v, %#v", tooLarge, infinite)
	}
}

func TestCompileNewScalarFunctions(t *testing.T) {
	query, err := Parse(`Events | extend Length=array_length(RawData.items), Keys=bag_keys(RawData), Empty=isempty(User), Present=isnotempty(Message), Date=todatetime(Message) | project Length, Keys, Empty, Present, Date`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"kql_array_length", "kql_bag_keys", "COALESCE(length", "kql_todatetime"} {
		if !strings.Contains(compiled.SQL, fragment) {
			t.Fatalf("compiled SQL lacks %q: %s", fragment, compiled.SQL)
		}
	}
	if _, dynamic := compiled.DynamicColumns["Keys"]; !dynamic {
		t.Fatal("bag_keys output is not marked dynamic")
	}
}

func TestExecuteNewScalarFunctions(t *testing.T) {
	db := openKQLTestDatabase(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO events (username, message, raw_data) VALUES (?, ?, ?)`, "", "2026-07-20T12:34:56Z", `{"items":[1,2],"value":3}`); err != nil {
		t.Fatal(err)
	}
	query, err := Parse(`Events | extend Length=array_length(RawData.items), Keys=bag_keys(RawData), Empty=isempty(User), Present=isnotempty(Message), Date=todatetime(Message) | project Length, Keys, Empty, Present, Date`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var length, empty, present int64
	var keys, date string
	if err = db.QueryRow(compiled.SQL, compiled.Args...).Scan(&length, &keys, &empty, &present, &date); err != nil {
		t.Fatalf("execute scalar functions: %v\n%s", err, compiled.SQL)
	}
	if length != 2 || keys != `["items","value"]` || empty != 1 || present != 1 || date != "2026-07-20T12:34:56.000000000Z" {
		t.Fatalf("scalar results = %d, %q, %d, %d, %q", length, keys, empty, present, date)
	}
}

func TestCompileAndExecuteSecurityScalarFunctions(t *testing.T) {
	db := openKQLTestDatabase(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO events(time_generated, source, event_type, host, username, message, raw_data, dataset_id)
		VALUES ('2024-01-01T00:00:00Z', 'test', 'event', 'host', 'user', 'message', '{}', 1)`); err != nil {
		t.Fatal(err)
	}

	query, err := Parse(`Events | extend Decoded=base64_decode_tostring("cG93ZXJzaGVsbA=="), URL=url_decode("a%2Fb+c"), Has=bag_has_key(parse_json('{"context":{"admin":true}}'), "$.context.admin"), Member=set_has_element(parse_json('["alpha",2]'), 2), Private=ipv4_is_private("10.1.2.3"), InRange=ipv4_is_in_range("192.168.1.2", "10.0.0.0/8,192.168.1.2") | project Decoded, URL, Has, Member, Private, InRange`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"kql_base64_decode_tostring", "kql_url_decode", "kql_bag_has_key", "kql_set_has_element", "kql_ipv4_is_private", "kql_ipv4_is_in_range"} {
		if !strings.Contains(compiled.SQL, fragment) {
			t.Fatalf("compiled SQL lacks %q: %s", fragment, compiled.SQL)
		}
	}

	var decoded, decodedURL string
	var has, member, private, inRange int64
	if err = db.QueryRow(compiled.SQL, compiled.Args...).Scan(&decoded, &decodedURL, &has, &member, &private, &inRange); err != nil {
		t.Fatalf("execute scalar helpers: %v\n%s", err, compiled.SQL)
	}
	if decoded != "powershell" || decodedURL != "a/b+c" || has != 1 || member != 1 || private != 1 || inRange != 1 {
		t.Fatalf("scalar helper values = %q, %q, %d, %d, %d, %d", decoded, decodedURL, has, member, private, inRange)
	}
}

func TestBoundedBatchLimits(t *testing.T) {
	captures := make([]string, 17)
	for index := range captures {
		captures[index] = fmt.Sprintf("C%d:string", index)
	}
	projections := make([]string, 33)
	awayPatterns := make([]string, 33)
	bagItems := make([]string, 33)
	for index := range projections {
		projections[index] = fmt.Sprintf("C%d=Host", index)
		awayPatterns[index] = fmt.Sprintf("C%d", index)
		bagItems[index] = fmt.Sprintf("C%d:string", index)
	}
	tests := []struct {
		query   string
		message string
	}{
		{query: "Events | parse Message with " + strings.Join(captures, ` "," `), message: "at most 16"},
		{query: "Events | project " + strings.Join(projections, ", "), message: "at most 32"},
		{query: "Events | project-away " + strings.Join(awayPatterns, ", "), message: "at most 32"},
		{query: "Events | project-rename " + strings.Join(projections, ", "), message: "at most 32"},
		{query: "Events | evaluate bag_unpack(RawData) : (*, " + strings.Join(bagItems, ", ") + ")", message: "at most 32"},
	}
	for _, test := range tests {
		if _, err := Parse(test.query); err == nil || !strings.Contains(err.Error(), test.message) {
			t.Fatalf("Parse() error = %v, want %q", err, test.message)
		}
	}
}

func TestParsePatternLimit(t *testing.T) {
	query, err := Parse(`Events | parse Message with "` + strings.Repeat("x", maxParsePattern) + `" Value:string`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Compile(query, time.Now()); err == nil || !strings.Contains(err.Error(), "cannot exceed") {
		t.Fatalf("pattern limit error = %v", err)
	}
}

func TestBoundedBatchDiagnostics(t *testing.T) {
	tests := []struct {
		query   string
		message string
	}{
		{query: `Events | parse kind=regex Message with Value:string`, message: "parse kind"},
		{query: `Events | parse Message with Value:dynamic`, message: "not supported"},
		{query: `Events | parse Message with Value`, message: "explicit type"},
		{query: `Events | parse Message with "literal"`, message: "at least one capture"},
		{query: `Events | parse Message with Value:string *`, message: "literal separator"},
		{query: `Events | project Host, host=Source`, message: "specified more than once"},
		{query: `Events | project-rename Source=Host`, message: "conflicts with another output"},
		{query: `Events | project-away Missing`, message: "unknown column"},
		{query: `Events | evaluate bag_unpack(RawData)`, message: "explicit output schema"},
		{query: `Events | evaluate bag_unpack(Message) : (*, Value:string)`, message: "requires a dynamic column"},
		{query: `Events | evaluate bag_unpack(RawData) : (*, Host:string)`, message: "conflicts with another output"},
		{query: `Events | evaluate bag_unpack(RawData, "bag_") : (*, Name:string)`, message: "must start with prefix"},
		{query: `Events | evaluate bag_unpack(RawData) : (*, Value:bool)`, message: "not supported"},
	}
	for _, test := range tests {
		t.Run(test.message+test.query, func(t *testing.T) {
			query, err := Parse(test.query)
			if err == nil {
				_, err = Compile(query, time.Now())
			}
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
		})
	}
}

func TestParseCompileAndExecuteLookup(t *testing.T) {
	query, err := Parse(`UAL
| project Host, Message
| lookup (Sysmon | project Host, User, Message) on Host
| order by Host`)
	if err != nil {
		t.Fatal(err)
	}
	lookup, ok := query.Operators[1].(LookupOperator)
	if !ok || lookup.Kind != JoinLeftOuter || len(lookup.Keys) != 1 || lookup.Keys[0].Name != "Host" {
		t.Fatalf("lookup = %#v", query.Operators[1])
	}
	compiled, err := Compile(query, time.Now(), TableCatalog{"UAL": 1, "Sysmon": 2})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SQL, "LEFT OUTER JOIN") || !strings.Contains(compiled.SQL, "AS lookup_right LIMIT") {
		t.Fatalf("compiled lookup is not bounded left outer join: %s", compiled.SQL)
	}
	if got := strings.Join(compiled.Columns, ","); got != "Host,Message,User,Message1" {
		t.Fatalf("columns = %q", got)
	}

	db := openKQLTestDatabase(t)
	defer db.Close()
	for _, row := range []struct {
		dataset int
		host    string
		user    string
		message string
	}{
		{dataset: 1, host: "a", message: "left-a"},
		{dataset: 1, host: "b", message: "left-b"},
		{dataset: 2, host: "a", user: "alice", message: "right-a"},
	} {
		if _, err = db.Exec(`INSERT INTO events (dataset_id, host, username, message) VALUES (?, ?, ?, ?)`, row.dataset, row.host, row.user, row.message); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := db.Query(compiled.SQL, compiled.Args...)
	if err != nil {
		t.Fatalf("execute lookup: %v\n%s", err, compiled.SQL)
	}
	defer rows.Close()
	want := []struct {
		host, left  string
		user, right sql.NullString
	}{
		{host: "a", left: "left-a", user: sql.NullString{String: "alice", Valid: true}, right: sql.NullString{String: "right-a", Valid: true}},
		{host: "b", left: "left-b"},
	}
	for index, expected := range want {
		if !rows.Next() {
			t.Fatalf("missing lookup row %d", index)
		}
		var host, left string
		var user, right sql.NullString
		if err = rows.Scan(&host, &left, &user, &right); err != nil {
			t.Fatal(err)
		}
		if host != expected.host || left != expected.left || user != expected.user || right != expected.right {
			t.Fatalf("row %d = %q, %q, %#v, %#v", index, host, left, user, right)
		}
	}
}

func TestExecuteLookupBoundsRightSide(t *testing.T) {
	db := openKQLTestDatabase(t)
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO events (dataset_id, host) VALUES (1, 'same')`); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 1001; index++ {
		if _, err = tx.Exec(`INSERT INTO events (dataset_id, host, message) VALUES (2, 'same', ?)`, fmt.Sprintf("right-%d", index)); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	query, err := Parse(`UAL | project Host | lookup kind=inner (Sysmon | project Host, Message) on Host | summarize Matches=count()`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now(), TableCatalog{"UAL": 1, "Sysmon": 2})
	if err != nil {
		t.Fatal(err)
	}
	var matches int64
	if err = db.QueryRow(compiled.SQL, compiled.Args...).Scan(&matches); err != nil {
		t.Fatal(err)
	}
	if matches != 1000 {
		t.Fatalf("lookup matches = %d, want bounded 1000", matches)
	}
}

func TestExecuteMVExpandWithItemIndexAndDynamicMetadata(t *testing.T) {
	db := openKQLTestDatabase(t)
	defer db.Close()
	for _, row := range []struct {
		host string
		data string
	}{
		{host: "array", data: `[{"x":1},[2],3,"true"]`},
		{host: "object", data: `{"a":{"x":1},"b":[2],"c":4,"d":5}`},
	} {
		if _, err := db.Exec(`INSERT INTO events (host, raw_data) VALUES (?, ?)`, row.host, row.data); err != nil {
			t.Fatal(err)
		}
	}
	query, err := Parse(`Events | mv-expand with_itemindex=Index Item=RawData limit 4 | where Item != "missing" | project Host, Item, Index | order by Host, Index`)
	if err != nil {
		t.Fatal(err)
	}
	operator, ok := query.Operators[0].(MVExpandOperator)
	if !ok || operator.ItemIndex != "Index" || operator.Name != "Item" {
		t.Fatalf("mv-expand = %#v", query.Operators[0])
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(compiled.Columns, ","); got != "Host,Item,Index" {
		t.Fatalf("columns = %q", got)
	}
	if _, dynamic := compiled.DynamicColumns["Item"]; !dynamic {
		t.Fatal("expanded dynamic value lost its metadata")
	}
	if _, dynamic := compiled.DynamicColumns["Index"]; dynamic {
		t.Fatal("item index must be scalar")
	}
	rows, err := db.Query(compiled.SQL, compiled.Args...)
	if err != nil {
		t.Fatalf("execute mv-expand: %v\n%s", err, compiled.SQL)
	}
	defer rows.Close()
	want := []struct {
		host  string
		value any
		index int64
	}{
		{host: "array", value: `{"x":1}`, index: 0},
		{host: "array", value: `[2]`, index: 1},
		{host: "array", value: int64(3), index: 2},
		{host: "array", value: `"true"`, index: 3},
		{host: "object", value: `{"x":1}`, index: 0},
		{host: "object", value: `[2]`, index: 1},
		{host: "object", value: int64(4), index: 2},
		{host: "object", value: int64(5), index: 3},
	}
	for index, expected := range want {
		if !rows.Next() {
			t.Fatalf("missing expanded row %d", index)
		}
		var host string
		var value any
		var itemIndex int64
		if err = rows.Scan(&host, &value, &itemIndex); err != nil {
			t.Fatal(err)
		}
		if bytes, ok := value.([]byte); ok {
			value = string(bytes)
		}
		if host != expected.host || value != expected.value || itemIndex != expected.index {
			t.Fatalf("expanded row %d = %q, %#v, %d", index, host, value, itemIndex)
		}
	}
}

func TestCompileAndExecuteProjectReorder(t *testing.T) {
	query, err := Parse(`Events | project-reorder Host, *Type, Time*, Host`)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	want := "Host,EventType,TimeGenerated,Source,User,Message,RawData"
	if got := strings.Join(compiled.Columns, ","); got != want {
		t.Fatalf("columns = %q, want %q", got, want)
	}
	if _, dynamic := compiled.DynamicColumns["RawData"]; !dynamic {
		t.Fatal("project-reorder lost dynamic metadata")
	}
	db := openKQLTestDatabase(t)
	defer db.Close()
	if _, err = db.Exec(`INSERT INTO events (host) VALUES ('host-1')`); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(compiled.SQL, compiled.Args...)
	if err != nil {
		t.Fatalf("execute project-reorder: %v\n%s", err, compiled.SQL)
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(columns, ","); got != want {
		t.Fatalf("SQLite columns = %q, want %q", got, want)
	}
}

func TestParseCompileAndExecuteParseKV(t *testing.T) {
	query, err := Parse(`Events | parse-kv Message as (Name:string, Count:long, Ratio:real) with (pair_delimiter=";", kv_delimiter="=") | project Name, Count, Ratio`)
	if err != nil {
		t.Fatal(err)
	}
	operator, ok := query.Operators[0].(ParseKVOperator)
	if !ok || len(operator.Items) != 3 || operator.PairDelimiter != ";" || operator.KVDelimiter != "=" {
		t.Fatalf("parse-kv = %#v", query.Operators[0])
	}
	compiled, err := Compile(query, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(compiled.SQL, "kql_parse_kv(") || strings.Join(compiled.Columns, ",") != "Name,Count,Ratio" {
		t.Fatalf("compiled parse-kv = %s, columns %v", compiled.SQL, compiled.Columns)
	}
	if got := compiled.Args[0]; got != `[{"name":"Name","type":"string"},{"name":"Count","type":"long"},{"name":"Ratio","type":"real"}]` {
		t.Fatalf("schema JSON = %#v", got)
	}

	db := openKQLTestDatabase(t)
	defer db.Close()
	if _, err = db.Exec(`INSERT INTO events (message) VALUES ('Name=alice;Count=7;Ratio=2.5')`); err != nil {
		t.Fatal(err)
	}
	var name string
	var count int64
	var ratio float64
	if err = db.QueryRow(compiled.SQL, compiled.Args...).Scan(&name, &count, &ratio); err != nil {
		t.Fatalf("execute parse-kv: %v\n%s", err, compiled.SQL)
	}
	if name != "alice" || count != 7 || ratio != 2.5 {
		t.Fatalf("parse-kv values = %q, %d, %v", name, count, ratio)
	}
}

func TestBoundedTabularDiagnostics(t *testing.T) {
	keys := make([]string, 17)
	outputs := make([]string, 17)
	patterns := make([]string, 33)
	for index := range keys {
		keys[index] = fmt.Sprintf("K%d", index)
		outputs[index] = fmt.Sprintf("K%d:string", index)
	}
	for index := range patterns {
		patterns[index] = fmt.Sprintf("K%d", index)
	}
	tests := []struct {
		query   string
		message string
	}{
		{query: `Events | lookup kind=leftanti (Events) on Host`, message: "lookup kind"},
		{query: `Events | lookup (Events | project Host) on Missing`, message: "does not exist on the left"},
		{query: `Events | lookup (Events | project Host) on Host, Host`, message: "specified more than once"},
		{query: `Events | lookup (Events) on ` + strings.Join(keys, ", "), message: "at most 16"},
		{query: `Events | mv-expand with_itemindex=Host Item=RawData`, message: "conflicts with another output"},
		{query: `Events | mv-expand with_itemindex=Item Item=RawData`, message: "conflicts with another output"},
		{query: `Events | parse-kv Message as (Host:string) with (pair_delimiter=";", kv_delimiter="=")`, message: "conflicts with another output"},
		{query: `Events | parse-kv Message as (` + strings.Join(outputs, ", ") + `) with (pair_delimiter=";", kv_delimiter="=")`, message: "at most 16"},
		{query: `Events | parse-kv Message as (Value:string) with (pair_delimiter="", kv_delimiter="=")`, message: "between 1 and 16 bytes"},
		{query: `Events | parse-kv Message as (Value:string) with (pair_delimiter=";", kv_delimiter="12345678901234567")`, message: "between 1 and 16 bytes"},
		{query: `Events | parse-kv Message as (` + strings.Repeat("K", maxParseKVSchema) + `:string) with (pair_delimiter=";", kv_delimiter="=")`, message: "schema cannot exceed"},
		{query: `Events | project-reorder ` + strings.Join(patterns, ", "), message: "at most 32"},
		{query: `Events | project-reorder Missing`, message: "unknown column"},
	}
	for _, test := range tests {
		t.Run(test.message+test.query, func(t *testing.T) {
			query, err := Parse(test.query)
			if err == nil {
				_, err = Compile(query, time.Now())
			}
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("error = %v, want %q", err, test.message)
			}
		})
	}
}

func openKQLTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE events (
		time_generated TEXT, source TEXT, event_type TEXT, host TEXT,
		username TEXT, message TEXT, raw_data TEXT, dataset_id INTEGER
	)`); err != nil {
		db.Close()
		t.Fatal(err)
	}
	return db
}
