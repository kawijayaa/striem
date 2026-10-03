package dialect

import "strings"

type sqlite struct {
	regexFunction            string
	regexCaseInsensitiveFlag string
}

// SQLiteOption configures optional SQLite capabilities supplied by an
// application (usually through registered UDFs).
type SQLiteOption func(*sqlite)

// WithRegexFunction configures a two-argument regex UDF accepting pattern,
// value. Invalid SQL identifiers are ignored.
func WithRegexFunction(name string) SQLiteOption {
	return func(d *sqlite) {
		if validFunctionName(name) {
			d.regexFunction = name
		}
	}
}

// WithRegexCaseInsensitiveFlag configures a regex pattern prefix used for
// case-insensitive matches instead of lowercasing the pattern and value.
func WithRegexCaseInsensitiveFlag(flag string) SQLiteOption {
	return func(d *sqlite) { d.regexCaseInsensitiveFlag = flag }
}

// SQLite returns the SQLite renderer with JSON1-compatible access.
func SQLite(options ...SQLiteOption) Dialect {
	d := sqlite{}
	for _, option := range options {
		if option != nil {
			option(&d)
		}
	}
	return d
}

func (sqlite) Name() string                       { return "sqlite" }
func (sqlite) SupportsMaterializedBindings() bool { return true }
func (sqlite) QuoteIdentifier(s string) string    { return quoteDouble(s) }
func (sqlite) Placeholder(index int) string       { return "?" }
func (sqlite) Boolean(value bool) string {
	if value {
		return "1"
	}
	return "0"
}
func (d sqlite) Regex(left, right string, caseSensitive bool) (string, bool) {
	if d.regexFunction == "" {
		return "", false
	}
	if !caseSensitive {
		if d.regexCaseInsensitiveFlag != "" {
			right = quoteString(d.regexCaseInsensitiveFlag) + " || " + right
		} else {
			left, right = "lower("+left+")", "lower("+right+")"
		}
	}
	return d.regexFunction + "(" + right + ", " + left + ")", true
}
func (d sqlite) JSONIndex(value, index string) string {
	return d.JSONIndexExpression(value, index, false)
}
func (sqlite) JSONIndexExpression(value, index string, numeric bool) string {
	if numeric {
		// Keep each expression (and each bound placeholder) in the SQL once.
		// Adding an array length to a negative int64 is safe even at MinInt64.
		return "(SELECT ksql_index_values.value FROM (SELECT " + value + " AS document, " + index + " AS subscript) AS ksql_index_bounds, " +
			"json_each(ksql_index_bounds.document) AS ksql_index_values WHERE ksql_index_values.key = " +
			"CASE WHEN ksql_index_bounds.subscript < 0 THEN json_array_length(ksql_index_bounds.document) + ksql_index_bounds.subscript " +
			"ELSE ksql_index_bounds.subscript END LIMIT 1)"
	}
	if key, ok := decodeSQLStringLiteral(index); ok && safeSQLiteJSONKey(key) {
		return sqliteJSONExtract(value, []string{key})
	}
	// json_each key comparison works for every object key, including quotes
	// and backslashes, on SQLite versions whose JSON path parser cannot address
	// those keys directly.
	return "(SELECT value FROM json_each(" + value + ") WHERE key = " + index + " LIMIT 1)"
}
func (sqlite) JSONIndexPath(value string, keys []string) (string, bool) {
	for _, key := range keys {
		if !safeSQLiteJSONKey(key) {
			return "", false
		}
	}
	return sqliteJSONExtract(value, keys), true
}
func (sqlite) SafeCast(_, _ string) (string, bool) { return "", false }
func (sqlite) Series(column, from, to, step, alias string) (string, bool) {
	// Evaluate each bound once: duplicating '?' placeholders would change the
	// parameter order. A derived recursive CTE also remains lazy under LIMIT.
	return "(WITH RECURSIVE ksql_bounds(lo, hi, stride) AS (SELECT " + from + ", " + to + ", " + step + "), " +
		"ksql_range(value) AS (SELECT lo FROM ksql_bounds WHERE (stride > 0 AND lo <= hi) OR (stride < 0 AND lo >= hi) " +
		"UNION ALL SELECT value + stride FROM ksql_range, ksql_bounds WHERE " +
		"((stride > 0 AND value + stride > value AND value + stride <= hi) OR " +
		"(stride < 0 AND value + stride < value AND value + stride >= hi)) AND " +
		"(typeof(lo) = 'real' OR typeof(hi) = 'real' OR typeof(stride) = 'real' OR typeof(value + stride) = 'integer')) " +
		"SELECT value AS " + column + " FROM ksql_range) AS " + alias, true
}
func (sqlite) JSONEach(value, alias string) (string, bool) {
	value = "CASE WHEN " + value + " IS NULL THEN json_array(NULL) ELSE " + value + " END"
	return "json_each(" + value + ") AS " + alias, true
}
func (sqlite) Random() (string, bool)              { return "RANDOM()", true }
func (sqlite) ApplyLimit(sql, limit string) string { return sql + " LIMIT " + limit }
func (sqlite) SupportsNullOrdering() bool          { return true }
func (sqlite) SupportsJoinUsing() bool             { return true }
func (sqlite) Concat(parts []string) (string, bool) {
	return "(" + strings.Join(parts, " || ") + ")", true
}

func validFunctionName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (i == 0 || r < '0' || r > '9') {
			return false
		}
	}
	return true
}

func decodeSQLStringLiteral(value string) (string, bool) {
	if len(value) < 2 || value[0] != '\'' || value[len(value)-1] != '\'' {
		return "", false
	}
	var decoded strings.Builder
	for i := 1; i < len(value)-1; i++ {
		if value[i] != '\'' {
			decoded.WriteByte(value[i])
			continue
		}
		if i+1 >= len(value)-1 || value[i+1] != '\'' {
			return "", false
		}
		decoded.WriteByte('\'')
		i++
	}
	return decoded.String(), true
}

func safeSQLiteJSONKey(key string) bool {
	return !strings.ContainsAny(key, `"\`)
}

func sqliteJSONExtract(value string, keys []string) string {
	var path strings.Builder
	path.WriteByte('$')
	for _, key := range keys {
		path.WriteString(`."`)
		path.WriteString(key)
		path.WriteByte('"')
	}
	return "json_extract(" + value + ", " + quoteString(path.String()) + ")"
}
