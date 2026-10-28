package database

import (
	"fmt"
	"reflect"

	"github.com/mattn/go-sqlite3"
)

// registerSafeFunc registers a UDF whose panics become query errors (or a
// null result for functions without an error result) instead of crashing
// the process: SQLite callbacks run on the query's goroutine and a panic
// there would bring down the server.
func registerSafeFunc(connection *sqlite3.SQLiteConn, name string, function any, pure bool) error {
	return connection.RegisterFunc(name, recoverFunction(name, function), pure)
}

var errorType = reflect.TypeOf((*error)(nil)).Elem()

// recovered converts a panic into an error naming the function.
func recovered(name string, err *error) {
	if value := recover(); value != nil && err != nil {
		*err = fmt.Errorf("%s failed: %v", name, value)
	}
}

func recoverFunction(name string, function any) any {
	// Common signatures are wrapped directly; reflection is slower per call.
	switch f := function.(type) {
	case func(any) any:
		return func(a any) (result any) {
			defer func() {
				if recover() != nil {
					result = nil
				}
			}()
			return f(a)
		}
	case func(any, any) any:
		return func(a, b any) (result any) {
			defer func() {
				if recover() != nil {
					result = nil
				}
			}()
			return f(a, b)
		}
	case func(any, any, any) any:
		return func(a, b, c any) (result any) {
			defer func() {
				if recover() != nil {
					result = nil
				}
			}()
			return f(a, b, c)
		}
	case func(...any) any:
		return func(values ...any) (result any) {
			defer func() {
				if recover() != nil {
					result = nil
				}
			}()
			return f(values...)
		}
	case func(any) (any, error):
		return func(a any) (result any, err error) {
			defer recovered(name, &err)
			return f(a)
		}
	case func(any, any) (any, error):
		return func(a, b any) (result any, err error) {
			defer recovered(name, &err)
			return f(a, b)
		}
	case func(any, any, any) (any, error):
		return func(a, b, c any) (result any, err error) {
			defer recovered(name, &err)
			return f(a, b, c)
		}
	case func(any, any) (bool, error):
		return func(a, b any) (result bool, err error) {
			defer recovered(name, &err)
			return f(a, b)
		}
	case func(any, string) any:
		return func(a any, b string) (result any) {
			defer func() {
				if recover() != nil {
					result = nil
				}
			}()
			return f(a, b)
		}
	case func(...any) (any, error):
		return func(values ...any) (result any, err error) {
			defer recovered(name, &err)
			return f(values...)
		}
	}
	value := reflect.ValueOf(function)
	kind := value.Type()
	wrapped := reflect.MakeFunc(kind, func(args []reflect.Value) (results []reflect.Value) {
		defer func() {
			if recovered := recover(); recovered != nil {
				results = make([]reflect.Value, kind.NumOut())
				for i := range results {
					results[i] = reflect.Zero(kind.Out(i))
				}
				if last := kind.NumOut() - 1; last >= 0 && kind.Out(last) == errorType {
					results[last] = reflect.ValueOf(fmt.Errorf("%s failed: %v", name, recovered))
				}
			}
		}()
		if kind.IsVariadic() {
			return value.CallSlice(args)
		}
		return value.Call(args)
	})
	return wrapped.Interface()
}
