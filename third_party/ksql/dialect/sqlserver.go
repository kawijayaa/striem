package dialect

import (
	"fmt"
	"strings"
)

type sqlServer struct{}

// SQLServer returns the Microsoft SQL Server renderer.
func SQLServer() Dialect { return sqlServer{} }

func (sqlServer) Name() string { return "sqlserver" }
func (sqlServer) QuoteIdentifier(s string) string {
	return "[" + strings.ReplaceAll(s, "]", "]]") + "]"
}
func (sqlServer) Placeholder(index int) string { return fmt.Sprintf("@p%d", index) }
func (sqlServer) Boolean(value bool) string {
	if value {
		return "CAST(1 AS bit)"
	}
	return "CAST(0 AS bit)"
}
func (sqlServer) Regex(_, _ string, _ bool) (string, bool) { return "", false }
func (sqlServer) JSONIndex(value, index string) string {
	return "JSON_QUERY(" + value + ", '$.' + " + index + ")"
}
func (sqlServer) SafeCast(expression, target string) (string, bool) {
	return "TRY_CAST(" + expression + " AS " + target + ")", true
}
func (sqlServer) Series(column, from, to, step, alias string) (string, bool) {
	return "(SELECT value AS " + column + " FROM GENERATE_SERIES(" + from + ", " + to + ", " + step + ")) AS " + alias, true
}
func (sqlServer) JSONEach(_, _ string) (string, bool) { return "", false }
func (sqlServer) Random() (string, bool)              { return "NEWID()", true }
func (sqlServer) ApplyLimit(sql, limit string) string {
	return strings.Replace(sql, "SELECT ", "SELECT TOP ("+limit+") ", 1)
}
func (sqlServer) SupportsNullOrdering() bool { return false }
func (sqlServer) SupportsJoinUsing() bool    { return false }
