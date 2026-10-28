package kql

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/kawijayaa/ksql"
)

type docExample struct {
	File    string     `json:"file"`
	Query   string     `json:"query"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}

var docTimeLayouts = []string{"2006-01-02T15:04:05.999999999Z", "2006-01-02 15:04:05.9999999", "2006-01-02T15:04:05.9999999", "2006-01-02 15:04:05", "2006-01-02T15:04:05Z", "2006-01-02T15:04:05", "2006-01-02", "1/2/2006 3:04:05 PM", "2006-01-02 15:04"}

func docNormalize(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, `\_`, "_"), `\|`, "|"))
	value = strings.TrimSpace(strings.ReplaceAll(value, "<br>", ""))
	value = strings.Trim(value, "`")
	if value == "(null)" || value == "null" || value == "" {
		return ""
	}
	for _, layout := range docTimeLayouts {
		if parsed, err := time.Parse(layout, value); err == nil {
			return "T" + parsed.UTC().Format(time.RFC3339Nano)
		}
	}
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return "N" + strconv.FormatFloat(number, 'g', 6, 64)
	}
	if lower := strings.ToLower(value); lower == "true" || lower == "false" {
		return "B" + lower
	}
	var decoded any
	if (strings.HasPrefix(value, "[") || strings.HasPrefix(value, "{")) && json.Unmarshal([]byte(value), &decoded) == nil {
		encoded, _ := json.Marshal(decoded)
		return "J" + string(encoded)
	}
	return "S" + value
}

func docCell(value any, column ksql.ScalarType) string {
	switch current := value.(type) {
	case nil:
		return ""
	case []byte:
		value = string(current)
	}
	switch column {
	case ksql.TypeBool:
		if number, ok := value.(int64); ok {
			return docNormalize(strconv.FormatBool(number != 0))
		}
	case ksql.TypeTimespan:
		if ticks, ok := value.(int64); ok {
			return "S" + ksql.FormatTimespan(ticks)
		}
	}
	switch current := value.(type) {
	case int64:
		if column == ksql.TypeUnknown || column == "" {
			return docNormalize(strconv.FormatInt(current, 10))
		}
		return docNormalize(strconv.FormatInt(current, 10))
	case float64:
		if math.IsNaN(current) {
			return "SNaN"
		}
		return docNormalize(strconv.FormatFloat(current, 'g', -1, 64))
	}
	return docNormalize(fmt.Sprint(value))
}

// TestDocumentationExamples runs Microsoft's documented examples, extracted by
// tools/docs-conformance/extract.py, and writes a report of passes, compile
// errors, mismatches and name differences. It is opt-in: set DOC_EXAMPLES to
// the extracted JSON and DOC_REPORT to the report path.
func TestDocumentationExamples(t *testing.T) {
	data, err := os.ReadFile(os.Getenv("DOC_EXAMPLES"))
	if err != nil {
		t.Skip(err)
	}
	var examples []docExample
	if err := json.Unmarshal(data, &examples); err != nil {
		t.Fatal(err)
	}
	db := featureDatabase(t)
	ordered := regexp.MustCompile(`(?i)\b(sort|order|top|serialize|row_number|prev|next|row_cumsum)\b`)
	report := map[string][]string{}
	for _, example := range examples {
		query := example.Query
		bagKeys := func(sql string, args []any) ([]ksql.BagKey, error) {
			rows, err := db.Query(sql, args...)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			var keys []ksql.BagKey
			for rows.Next() {
				var key ksql.BagKey
				if err := rows.Scan(&key.Name, &key.JSONType); err != nil {
					return nil, err
				}
				keys = append(keys, key)
			}
			return keys, rows.Err()
		}
		compiled, err := Compile(query, time.Date(2026, 1, 4, 0, 0, 0, 0, time.UTC), CompileConfig{Tables: featureCatalog(), BagKeys: bagKeys})
		if err != nil {
			message := err.Error()
			key := "compile"
			if strings.Contains(message, "unknown table") || strings.Contains(message, "unknown source") || strings.Contains(message, "unknown column") {
				key = "unknown-input"
			}
			report[key] = append(report[key], example.File+": "+message)
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		rows, err := db.QueryContext(ctx, compiled.SQL, compiled.Args...)
		if err != nil {
			cancel()
			report["runtime"] = append(report["runtime"], example.File+": "+err.Error()+" :: "+strings.ReplaceAll(query, "\n", " "))
			continue
		}
		columns, _ := rows.Columns()
		types := map[string]ksql.ScalarType{}
		for name := range compiled.BooleanColumns {
			types[name] = ksql.TypeBool
		}
		for name := range compiled.DynamicColumns {
			types[name] = ksql.TypeDynamic
		}
		for name := range compiled.TimespanColumns {
			types[name] = ksql.TypeTimespan
		}
		var got []string
		for rows.Next() {
			values := make([]any, len(columns))
			pointers := make([]any, len(columns))
			for i := range values {
				pointers[i] = &values[i]
			}
			rows.Scan(pointers...)
			cells := make([]string, len(values))
			for i, value := range values {
				cells[i] = docCell(value, types[columns[i]])
			}
			got = append(got, strings.Join(cells, " | "))
		}
		rowErr := rows.Err()
		rows.Close()
		cancel()
		if rowErr != nil {
			report["runtime"] = append(report["runtime"], example.File+": "+rowErr.Error())
			continue
		}
		var want []string
		for _, row := range example.Rows {
			cells := make([]string, max(len(row), len(columns)))
			for i, cell := range row {
				if i < len(cells) {
					cells[i] = docNormalize(cell)
					// The docs show some Boolean results as 1 and 0.
					if i < len(columns) && types[columns[i]] == ksql.TypeBool && (cells[i] == "N1" || cells[i] == "N0") {
						cells[i] = map[string]string{"N1": "Btrue", "N0": "Bfalse"}[cells[i]]
					}
				}
			}
			want = append(want, strings.Join(cells, " | "))
		}
		if len(example.Rows) > 0 && strings.Contains(strings.Join(example.Rows[len(example.Rows)-1], ""), "...") {
			report["partial-docs"] = append(report["partial-docs"], example.File)
			continue
		}
		if !ordered.MatchString(query) {
			sort.Strings(got)
			sort.Strings(want)
		}
		wantColumns := make([]string, len(example.Columns))
		for i, column := range example.Columns {
			wantColumns[i] = strings.Trim(strings.ReplaceAll(column, `\_`, "_"), "` ")
		}
		if strings.Join(got, "\n") == strings.Join(want, "\n") {
			if strings.Join(columns, ",") != strings.Join(wantColumns, ",") {
				report["names"] = append(report["names"], fmt.Sprintf("%s: got %v want %v", example.File, columns, wantColumns))
			} else {
				report["pass"] = append(report["pass"], example.File)
			}
			continue
		}
		report["mismatch"] = append(report["mismatch"], fmt.Sprintf("%s\n  Q: %s\n  got:  %q\n  want: %q", example.File, strings.ReplaceAll(query, "\n", " "), got, want))
	}
	encoded, _ := json.MarshalIndent(report, "", " ")
	os.WriteFile(os.Getenv("DOC_REPORT"), encoded, 0o644)
	for key, values := range report {
		t.Logf("%s: %d", key, len(values))
	}
}
