package database

import (
	"strings"
	"testing"
)

func TestRecoverFunction(t *testing.T) {
	withError := recoverFunction("boom", func(values ...any) (any, error) { panic("bad input") }).(func(...any) (any, error))
	if value, err := withError(1, 2); value != nil || err == nil || !strings.Contains(err.Error(), "boom failed: bad input") {
		t.Fatalf("value %v err %v", value, err)
	}
	plain := recoverFunction("plain", func(a, b any) any { panic("x") }).(func(any, any) any)
	if value := plain(1, 2); value != nil {
		t.Fatalf("value %v", value)
	}
	normal := recoverFunction("normal", func(a int64) int64 { return a * 2 }).(func(int64) int64)
	if normal(21) != 42 {
		t.Fatal("wrapped function changed its result")
	}
}
