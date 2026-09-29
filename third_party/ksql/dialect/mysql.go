package dialect

import "strings"

type mysql struct{}

// MySQL returns the MySQL 8 renderer.
func MySQL() Dialect { return mysql{} }

func (mysql) Name() string { return "mysql" }
func (mysql) QuoteIdentifier(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}
func (mysql) Placeholder(index int) string { return "?" }
func (mysql) Boolean(value bool) string {
	if value {
		return "TRUE"
	}
	return "FALSE"
}
func (mysql) Regex(left, right string, caseSensitive bool) (string, bool) {
	matchType := "'i'"
	if caseSensitive {
		matchType = "'c'"
	}
	return "REGEXP_LIKE(" + left + ", " + right + ", " + matchType + ")", true
}
func (mysql) JSONIndex(value, index string) string {
	return "JSON_EXTRACT(" + value + ", CONCAT('$.', " + index + "))"
}
func (mysql) SafeCast(_, _ string) (string, bool)        { return "", false }
func (mysql) Series(_, _, _, _, _ string) (string, bool) { return "", false }
func (mysql) JSONEach(_, _ string) (string, bool)        { return "", false }
func (mysql) Random() (string, bool)                     { return "RAND()", true }
func (mysql) ApplyLimit(sql, limit string) string        { return sql + " LIMIT " + limit }
func (mysql) SupportsNullOrdering() bool                 { return false }
func (mysql) SupportsJoinUsing() bool                    { return true }
