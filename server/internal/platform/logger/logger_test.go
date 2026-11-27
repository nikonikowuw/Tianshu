package logger_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"tianshu/internal/platform/logger"
)

func TestNewFormatsAndLevels(t *testing.T) {
	t.Run("production emits JSON logs and respects level", func(t *testing.T) {
		var buf bytes.Buffer
		log := logger.New("production", "warn", &buf)
		log.Info("should-be-filtered")
		log.Warn("visible-warning", "component", "auth")

		lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
		if len(lines) != 1 {
			t.Fatalf("line count = %d, want 1 (%q)", len(lines), buf.String())
		}

		var entry map[string]any
		if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
			t.Fatalf("expected valid JSON log, got %q: %v", lines[0], err)
		}
		if entry["msg"] != "visible-warning" || entry["level"] != "WARN" || entry["component"] != "auth" {
			t.Fatalf("unexpected JSON log entry: %v", entry)
		}
	})

	t.Run("development emits text logs with debug/error/fallback levels", func(t *testing.T) {
		tests := []struct {
			level     string
			emitDebug bool
			wantDebug bool
		}{
			{level: "debug", emitDebug: true, wantDebug: true},
			{level: "warning", emitDebug: true, wantDebug: false},
			{level: "error", emitDebug: true, wantDebug: false},
			{level: "unknown", emitDebug: true, wantDebug: false},
		}
		for _, tc := range tests {
			var buf bytes.Buffer
			log := logger.New("development", tc.level, &buf)
			if tc.emitDebug {
				log.Debug("dbg")
			}
			log.Error("err")
			out := buf.String()
			if strings.Contains(out, "msg=dbg") != tc.wantDebug {
				t.Fatalf("level=%q out=%q, wantDebug=%v", tc.level, out, tc.wantDebug)
			}
			if !strings.Contains(out, "msg=err") {
				t.Fatalf("level=%q missing error log: %q", tc.level, out)
			}
		}
	})

	t.Run("nil writer falls back without panic", func(t *testing.T) {
		log := logger.New("test", "error", nil)
		if log == nil {
			t.Fatal("expected non-nil logger")
		}
	})
}

func TestRequestIDContextPropagation(t *testing.T) {
	if got := logger.RequestIDFromContext(nil); got != "" { //nolint:staticcheck // intentionally testing nil context fallback
		t.Fatalf("RequestIDFromContext(nil) = %q, want empty", got)
	}

	var buf bytes.Buffer
	base := logger.New("production", "info", &buf)

	ctxWithoutID := t.Context()
	logger.FromContext(ctxWithoutID, base).Info("no-req-id")
	if strings.Contains(buf.String(), "request_id") {
		t.Fatalf("unexpected request_id in log without context value: %s", buf.String())
	}

	buf.Reset()
	ctxWithID := logger.WithRequestID(t.Context(), "req-abc-123")
	if got := logger.RequestIDFromContext(ctxWithID); got != "req-abc-123" {
		t.Fatalf("RequestIDFromContext = %q, want req-abc-123", got)
	}
	logger.FromContext(ctxWithID, base).Info("with-req-id")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("unmarshal log: %v", err)
	}
	if entry["request_id"] != "req-abc-123" {
		t.Fatalf("request_id = %v, want req-abc-123", entry["request_id"])
	}

	// Nil base logger must fall back to slog.Default without panic.
	fallback := logger.FromContext(ctxWithID, nil)
	if fallback == nil || fallback == slog.Default() {
		t.Fatalf("expected derived logger with request_id from slog.Default, got %v", fallback)
	}
}
