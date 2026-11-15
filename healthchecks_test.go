package apm

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rs/zerolog"
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

func TestHealthchecksHookLevels(t *testing.T) {
	resetAPM(t)
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)
	buffer := &bytes.Buffer{}
	logger := zerolog.New(buffer).Hook(hcHook)

	logger.Debug().Msg("debug message")
	logger.Info().Msg("info message")
	logger.Warn().Msg("warn message")
	logger.Error().Msg("")
	if checker.count() != 0 {
		t.Fatalf("expected no healthcheck fails for non-error levels, got %d", checker.count())
	}

	logger.Error().Int("user_id", 7).Msg("boom")

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
	if !strings.Contains(body, "goroutine") {
		t.Fatalf("expected stack trace in healthcheck body: %q", body)
	}
	if strings.Contains(body, "github.com/rs/zerolog") {
		t.Fatalf("expected zerolog frames to be skipped: %q", body)
	}
}

func TestHealthchecksHookPanicLevel(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)
	buffer := &bytes.Buffer{}
	logger := zerolog.New(buffer).Hook(hcHook)

	recovered := catchPanic(func() {
		logger.Panic().Msg("panic boom")
	})

	if recovered != "panic boom" {
		t.Fatalf("expected panic value panic boom, got %v", recovered)
	}
	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
	if !strings.Contains(checker.last(), "panic boom") {
		t.Fatalf("unexpected healthcheck body: %q", checker.last())
	}
}
