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

func TestConsoleHandlerPreservesAttributeGroupScope(t *testing.T) {
	var output bytes.Buffer
	base := slog.New(newConsoleHandler(&output, slog.LevelDebug)).With("service", "striem")
	grouped := base.WithGroup("request").With("id", 7).WithGroup("query")
	grouped.Info("run", "rows", 2)
	base.Info("ready", "port", 8080)
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], "  service=striem  request.id=7  request.query.rows=2") {
		t.Fatalf("attributes moved into later groups: %q", output.String())
	}
	if !strings.Contains(lines[1], "  service=striem  port=8080") || strings.Contains(lines[1], "request.") {
		t.Fatalf("child logger mutated parent: %q", output.String())
	}
}

func TestConsoleHandlerLevelsAndAttributeValues(t *testing.T) {
	var output bytes.Buffer
	logger := newConsoleLogger(&output)
	logger.Debug("hidden")
	logger.Info("visible", "message", "quoted value", "elapsed", 2*time.Second, "at", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), slog.Group("details", "count", 3), slog.Group("", "inline", true), slog.Attr{})
	got := output.String()
	for _, expected := range []string{"visible", `message="quoted value"`, "elapsed=2s", "at=2026-01-02T03:04:05Z", "details.count=3", "inline=true"} {
		if !strings.Contains(got, expected) {
			t.Fatalf("missing %q: %s", expected, got)
		}
	}
	if strings.Contains(got, "hidden") {
		t.Fatalf("debug message escaped level filter: %s", got)
	}
}
