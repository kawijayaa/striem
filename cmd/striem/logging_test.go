package main

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestConsoleHandlerFormatsReadableMessages(t *testing.T) {
	var output bytes.Buffer
	logger := slog.New(newConsoleHandler(&output, slog.LevelDebug))
	recordTime := time.Date(2026, time.August, 7, 23, 37, 36, 0, time.Local)
	record := slog.NewRecord(recordTime, slog.LevelError, "Could not load deployment", 0)
	record.Add("datasets", 3, "error", errors.New("invalid manifest\nline 29: field not found"))
	if err := logger.Handler().Handle(t.Context(), record); err != nil {
		t.Fatal(err)
	}

	got := output.String()
	want := "23:37:36 ERROR Could not load deployment  datasets=3\n" +
		"    error: invalid manifest\n" +
		"           line 29: field not found\n"
	if got != want {
		t.Fatalf("log output = %q, want %q", got, want)
	}
	if strings.Contains(got, `\\n`) {
		t.Fatalf("log output contains escaped newlines: %q", got)
	}
}
