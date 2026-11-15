package apm

import (
	"log/slog"
	"strings"
	"testing"
)

func TestHealthcheckFailWithoutClient(t *testing.T) {
	resetAPM(t)

	HealthcheckFail(strings.NewReader("body"))
}

func TestHealthcheckFail(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	HealthcheckFail(strings.NewReader("first"), strings.NewReader("second"))

	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
	if got := checker.last(); got != "first\nsecond" {
		t.Fatalf("unexpected healthcheck body: %q", got)
	}
}

func TestLoggerReportsErrorLevels(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	captureStdout(t, func() {
		logger := NewLogger(t.Context()).With("service", "apm").WithGroup("request").With("path", "/x")
		logger.Debug("debug message")
		logger.Info("info message")
		logger.Warn("warn message")
		logger.Error("")
		logger.Error("boom", "user_id", 7, "order_id", int64(9007199254740993))
	})

	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
	body := checker.last()
	if !strings.Contains(body, "boom") {
		t.Fatalf("expected message in healthcheck body: %q", body)
	}
	if !strings.Contains(body, `"user_id": 7`) {
		t.Fatalf("expected event fields in healthcheck body: %q", body)
	}
	if !strings.Contains(body, `"order_id": 9007199254740993`) {
		t.Fatalf("expected large integer to keep precision: %q", body)
	}
	if !strings.Contains(body, `"service": "apm"`) {
		t.Fatalf("expected logger attrs in healthcheck body: %q", body)
	}
	if !strings.Contains(body, `"request": {`) || !strings.Contains(body, `"path": "/x"`) {
		t.Fatalf("expected logger groups in healthcheck body: %q", body)
	}
	if !strings.Contains(body, "goroutine") {
		t.Fatalf("expected stack trace in healthcheck body: %q", body)
	}
	if strings.Contains(body, "log/slog") {
		t.Fatalf("expected slog frames to be skipped: %q", body)
	}
}

func TestLoggerReportsFatalLevel(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	captureStdout(t, func() {
		NewLogger(t.Context()).Log(t.Context(), slog.LevelError+4, "fatal message")
	})

	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
	if !strings.Contains(checker.last(), "fatal message") {
		t.Fatalf("unexpected healthcheck body: %q", checker.last())
	}
}
