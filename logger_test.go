package apm

import (
	"strings"
	"testing"

	"github.com/rs/zerolog"
)

func TestNewLoggerPlain(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	out := captureStdout(t, func() {
		logger := NewLoggerPlain()
		logger.Error().Msg("plain message")
	})

	if !strings.Contains(out, "plain message") {
		t.Fatalf("expected log output, got %q", out)
	}
	if checker.count() != 0 {
		t.Fatalf("expected no healthchecks from plain logger, got %d", checker.count())
	}
}

func TestNewLogger(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	out := captureStdout(t, func() {
		logger := NewLogger(t.Context())
		logger.Error().Msg("logger message")
	})

	if !strings.Contains(out, "logger message") {
		t.Fatalf("expected log output, got %q", out)
	}
	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
}

func TestNewLoggerWithSentry(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	sentryDSN = "https://key@example.com/1"

	out := captureStdout(t, func() {
		logger := NewLogger(f.context(t))
		logger.Error().Msg("sentry logger message")
	})

	if !strings.Contains(out, "sentry logger message") {
		t.Fatalf("expected log output, got %q", out)
	}
	events := f.tr.recorded()
	if len(events) != 1 {
		t.Fatalf("expected 1 sentry event, got %d", len(events))
	}
	if events[0].Message != "sentry logger message" {
		t.Fatalf("unexpected sentry message: %q", events[0].Message)
	}
}

func TestLog(t *testing.T) {
	resetAPM(t)
	ctx := NewContext()

	logger := Log(ctx)
	if logger != zerolog.Ctx(ctx) {
		t.Fatal("expected logger from context")
	}
	if logger.GetLevel() == zerolog.Disabled {
		t.Fatal("expected enabled logger")
	}

	fallback := Log()
	if fallback == nil {
		t.Fatal("expected fallback logger")
	}
	if fallback.GetLevel() == zerolog.Disabled {
		t.Fatal("expected enabled fallback logger")
	}
}

func TestNewSentryWriter(t *testing.T) {
	resetAPM(t)

	if _, err := newSentryWriter(t.Context()); err == nil {
		t.Fatal("expected error without sentry DSN")
	}

	sentryDSN = "https://key@example.com/1"
	writer, err := newSentryWriter(t.Context())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if writer == nil {
		t.Fatal("expected sentry writer")
	}
}
