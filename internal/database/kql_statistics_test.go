package database

import (
	"database/sql"
	"math"
	"strings"
	"testing"
)

func TestStatisticOverflowIsReported(t *testing.T) {
	db, err := sql.Open("striem_sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var value any
	for _, c := range []struct {
		query   string
		args    []any
		message string
	}{
		{`SELECT kql_variance(?)`, []any{math.Inf(1)}, "finite"},
		{`WITH x(v) AS (VALUES (1e308),(-1e308)) SELECT kql_variance(v) FROM x`, nil, "overflow"},
	} {
		err = db.QueryRow(c.query, c.args...).Scan(&value)
		if err == nil || !strings.Contains(err.Error(), c.message) {
			t.Fatalf("query=%s value=%v err=%v", c.query, value, err)
		}
	}
}
