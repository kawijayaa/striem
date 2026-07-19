package eventtime

import "time"

// Layout is fixed-width so timestamps retain chronological ordering in SQLite.
const Layout = "2006-01-02T15:04:05.000000000Z"

func Format(value time.Time) string {
	return value.UTC().Format(Layout)
}
