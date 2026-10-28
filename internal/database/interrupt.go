package database

/*
// sqlite3_interrupt is compiled into the binary by github.com/mattn/go-sqlite3.
void sqlite3_interrupt(void *db);
static void striem_interrupt(void *db) { sqlite3_interrupt(db); }
*/
import "C"

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sync"
	"time"
	"unsafe"

	"github.com/mattn/go-sqlite3"
)

// interruptInterval is how often an expired query is interrupted again.
const interruptInterval = 20 * time.Millisecond

// RunQuery runs a user query on a dedicated read-only connection and hands
// its rows to read. go-sqlite3 interrupts a query only once when its
// context ends, and SQLite drops an interrupt that lands before the first
// step, so a query could outlive its deadline and hold a connection forever.
// RunQuery interrupts the connection repeatedly after the deadline until the
// query returns.
func (s *Store) RunQuery(ctx context.Context, query string, args []any, read func(*sql.Rows) error) error {
	conn, err := s.queryDB.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	var handle unsafe.Pointer
	if err := conn.Raw(func(driverConn any) error {
		handle = sqliteHandle(driverConn)
		return nil
	}); err != nil {
		return err
	}
	if handle == nil {
		return errors.New("query connection does not expose its SQLite handle")
	}
	done := make(chan struct{})
	var watchdog sync.WaitGroup
	watchdog.Add(1)
	go func() {
		defer watchdog.Done()
		select {
		case <-done:
			return
		case <-ctx.Done():
		}
		ticker := time.NewTicker(interruptInterval)
		defer ticker.Stop()
		for {
			C.striem_interrupt(handle)
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
	// The watchdog stops before the connection returns to the pool, so it
	// never interrupts a later query.
	defer func() {
		close(done)
		watchdog.Wait()
	}()
	rows, err := conn.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	return read(rows)
}

// sqliteHandle reads the sqlite3* handle of a go-sqlite3 connection, which
// the driver keeps unexported.
func sqliteHandle(driverConn any) unsafe.Pointer {
	conn, ok := driverConn.(*sqlite3.SQLiteConn)
	if !ok || conn == nil {
		return nil
	}
	field := reflect.ValueOf(conn).Elem().FieldByName("db")
	if !field.IsValid() || field.Kind() != reflect.Pointer {
		return nil
	}
	return field.UnsafePointer()
}
