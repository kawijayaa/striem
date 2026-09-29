package dialect

import (
	"fmt"
)

type postgres struct{}

// PostgreSQL returns the PostgreSQL renderer.
func PostgreSQL() Dialect { return postgres{} }

func (postgres) Name() string                    { return "postgresql" }
func (postgres) QuoteIdentifier(s string) string { return quoteDouble(s) }
func (postgres) Placeholder(index int) string    { return fmt.Sprintf("$%d", index) }
func (postgres) Boolean(value bool) string {
	if value {
		return "TRUE"
	}
	return "FALSE"
}
func (postgres) Regex(left, right string, caseSensitive bool) (string, bool) {
	op := "~*"
	if caseSensitive {
		op = "~"
	}
	return "(" + left + " " + op + " " + right + ")", true
}
func (postgres) JSONIndex(value, index string) string { return "(" + value + " -> " + index + ")" }
func (postgres) SafeCast(_, _ string) (string, bool)  { return "", false }
func (postgres) Series(column, from, to, step, alias string) (string, bool) {
	return "generate_series(" + from + ", " + to + ", " + step + ") AS " + alias + "(" + column + ")", true
}
func (postgres) JSONEach(_, _ string) (string, bool) { return "", false }
func (postgres) Random() (string, bool)              { return "RANDOM()", true }
func (postgres) ApplyLimit(sql, limit string) string { return sql + " LIMIT " + limit }
func (postgres) SupportsNullOrdering() bool          { return true }
func (postgres) SupportsJoinUsing() bool             { return true }
