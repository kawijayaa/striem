package kql

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
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
