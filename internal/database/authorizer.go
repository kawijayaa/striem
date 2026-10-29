package database

import (
	"database/sql/driver"
	"strings"

	"github.com/mattn/go-sqlite3"
)

// restrictQueryConnection installs an authorizer on connections opened for
// user queries (those with query_only set), so the database itself keeps
// queries away from challenge progress, metadata, schema internals, other
// database files and extensions, whatever SQL the compiler produces.
func restrictQueryConnection(connection *sqlite3.SQLiteConn) error {
	rows, err := connection.Query("PRAGMA query_only", nil)
	if err != nil {
		return err
	}
	values := make([]driver.Value, 1)
	if err := rows.Next(values); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if enabled, ok := values[0].(int64); !ok || enabled == 0 {
		return nil
	}
	connection.RegisterAuthorizer(authorizeQuery)
	return nil
}

// authorizeQuery denies what a user query never needs.
func authorizeQuery(action int, first, second, _ string) int {
	switch action {
	case sqlite3.SQLITE_READ:
		if !queryReadableTable(first) {
			return sqlite3.SQLITE_DENY
		}
	case sqlite3.SQLITE_ATTACH, sqlite3.SQLITE_DETACH, sqlite3.SQLITE_PRAGMA:
		return sqlite3.SQLITE_DENY
	case sqlite3.SQLITE_FUNCTION:
		if strings.EqualFold(second, "load_extension") {
			return sqlite3.SQLITE_DENY
		}
	}
	return sqlite3.SQLITE_OK
}

func queryReadableTable(table string) bool {
	switch name := strings.ToLower(table); {
	case name == "investigation_progress", name == "workspace_metadata":
		return false
	case strings.HasPrefix(name, "sqlite_"), strings.HasPrefix(name, "pragma_"), name == "dbstat":
		// Schema tables and the pragma and dbstat virtual tables.
		return false
	}
	return true
}
