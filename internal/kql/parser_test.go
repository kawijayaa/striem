package kql

import (
	"errors"
	"strings"
	"testing"
	"time"
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
		{`Events | join Other`, "not supported"},
		{`Events | where Missing == 1`, "unknown column"},
		{`Events | extend RawData.foo`, "require a column name"},
		{`Events | take 1001`, "between 1 and 1000"},
		{`Events | top 10 TimeGenerated`, "expected 'by'"},
		{`Events | summarize Host`, "grouped or aggregated"},
		{`Events | summarize Value=sum(count())`, "cannot be nested"},
		{`Events | extend Value=substring(Message)`, "expects 2 or 3"},
		{`let value = 1 Events | take 1`, "expected ';'"},
		{`let subset = Events | where Source == "x"; subset | take 1`, "tabular let bindings"},
		{`let value = 1; let value = 2; Events | take 1`, "declared more than once"},
		{`let Host = "x"; Events | take 1`, "conflicts with an Events column"},
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
