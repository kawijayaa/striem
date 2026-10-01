package kql

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestProjectionWidthLimitCannotBeBypassed(t *testing.T) {
	columns := make([]string, 33)
	for i := range columns {
		columns[i] = fmt.Sprintf("c%d=%d", i, i)
	}
	chain := "print " + columns[0]
	for _, column := range columns[1:] {
		chain += " | extend " + column
	}
	for _, query := range []string{
		"print " + strings.Join(columns, ","),
		chain,
		"print " + strings.Join(columns[:32], ",") + " | project *,extra=1",
		"print " + strings.Join(columns[:32], ",") + " | project extra=1,*",
	} {
		if _, err := Compile(query, time.Now()); err == nil || !strings.Contains(err.Error(), "MaxProjectionItems") {
			t.Fatalf("expected width diagnostic for %s: %v", query, err)
		}
	}
	for _, query := range []string{
		"print " + strings.Join(columns[:32], ","),
		"print " + strings.Join(columns[:32], ",") + " | extend c0=99",
	} {
		result, err := Compile(query, time.Now())
		if err != nil || len(result.Columns) != 32 {
			t.Fatalf("boundary query: result=%#v err=%v", result, err)
		}
	}
}
