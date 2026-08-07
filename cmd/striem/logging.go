package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
)

type consoleHandler struct {
	output io.Writer
	level  slog.Leveler
	mutex  *sync.Mutex
	attrs  []slog.Attr
	groups []string
}

func newConsoleLogger(output io.Writer) *slog.Logger {
	return slog.New(newConsoleHandler(output, slog.LevelInfo))
}

func newConsoleHandler(output io.Writer, level slog.Leveler) *consoleHandler {
	return &consoleHandler{output: output, level: level, mutex: &sync.Mutex{}}
}

func (h *consoleHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level.Level()
}

func (h *consoleHandler) Handle(_ context.Context, record slog.Record) error {
	var line strings.Builder
	fmt.Fprintf(&line, "%s %-5s %s", record.Time.Format("15:04:05"), record.Level.String(), record.Message)
	for _, attr := range h.attrs {
		h.appendAttr(&line, attr, h.groups)
	}
	record.Attrs(func(attr slog.Attr) bool {
		h.appendAttr(&line, attr, h.groups)
		return true
	})
	line.WriteByte('\n')

	h.mutex.Lock()
	defer h.mutex.Unlock()
	_, err := io.WriteString(h.output, line.String())
	return err
}

func (h *consoleHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	clone.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &clone
}

func (h *consoleHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.groups = append(append([]string(nil), h.groups...), name)
	return &clone
}

func (h *consoleHandler) appendAttr(line *strings.Builder, attr slog.Attr, groups []string) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	key := strings.Join(append(append([]string(nil), groups...), attr.Key), ".")
	if attr.Value.Kind() == slog.KindGroup {
		childGroups := groups
		if attr.Key != "" {
			childGroups = append(append([]string(nil), groups...), attr.Key)
		}
		for _, child := range attr.Value.Group() {
			h.appendAttr(line, child, childGroups)
		}
		return
	}
	if key == "error" {
		line.WriteString("\n    error: ")
		line.WriteString(strings.ReplaceAll(formatLogValue(attr.Value), "\n", "\n           "))
		return
	}
	line.WriteString("  ")
	line.WriteString(key)
	line.WriteByte('=')
	line.WriteString(formatLogValue(attr.Value))
}

func formatLogValue(value slog.Value) string {
	switch value.Kind() {
	case slog.KindString:
		text := value.String()
		if strings.ContainsAny(text, " \t\r\n=\"") {
			return strconv.Quote(text)
		}
		return text
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().Format("2006-01-02T15:04:05Z07:00")
	default:
		return fmt.Sprint(value.Any())
	}
}
