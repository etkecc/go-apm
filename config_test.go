package apm

import (
	"log/slog"
	"math"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestSetName(t *testing.T) {
	resetAPM(t)

	SetName("service")
	if sentryName != "service" {
		t.Fatalf("expected sentry name service, got %q", sentryName)
	}
}

func TestSetLogLevel(t *testing.T) {
	cases := []struct {
		name  string
		level string
		want  slog.Level
	}{
		{name: "trace", level: "trace", want: slog.LevelDebug - 4},
		{name: "debug", level: "debug", want: slog.LevelDebug},
		{name: "info", level: "INFO", want: slog.LevelInfo},
		{name: "empty", level: "", want: slog.LevelInfo},
		{name: "warn", level: "warn", want: slog.LevelWarn},
		{name: "warning", level: " warning ", want: slog.LevelWarn},
		{name: "error", level: "error", want: slog.LevelError},
		{name: "fatal", level: "fatal", want: slog.LevelError + 4},
		{name: "panic", level: "panic", want: slog.LevelError + 8},
		{name: "disabled", level: "disabled", want: slog.Level(math.MaxInt)},
		{name: "unknown", level: "not-a-level", want: slog.LevelInfo},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetAPM(t)
			SetLogLevel(tc.level)
			if got := loglevel.Level(); got != tc.want {
				t.Fatalf("SetLogLevel(%q) = %s, want %s", tc.level, got, tc.want)
			}
		})
	}
}

func TestSetSentryDSNEmpty(t *testing.T) {
	resetAPM(t)

	SetSentryDSN("")
	if initialized {
		t.Fatal("expected sentry to stay uninitialized without DSN")
	}
}

func TestSetSentryDSNInvalid(t *testing.T) {
	resetAPM(t)

	out := captureStdout(t, func() {
		SetSentryDSN("not-a-dsn")
	})

	if initialized {
		t.Fatal("expected sentry init to fail for an invalid DSN")
	}
	if !strings.Contains(out, "sentry initialization failed") {
		t.Fatalf("expected initialization error log, got %q", out)
	}
}

func TestSetSentryDSN(t *testing.T) {
	resetAPM(t)
	const firstDSN = "https://key@example.com/1"
	const secondDSN = "https://key@example.com/2"

	SetName("service")
	SetSentryDSN(firstDSN)

	if !initialized {
		t.Fatal("expected sentry to be initialized")
	}
	client := sentry.CurrentHub().Client()
	t.Cleanup(client.Close)
	if got := client.Options().Environment; got != "service" {
		t.Fatalf("expected environment service, got %q", got)
	}
	if got := client.Options().Release; got != sentryVersion {
		t.Fatalf("expected release %q, got %q", sentryVersion, got)
	}

	SetSentryDSN(secondDSN)
	if got := sentry.CurrentHub().Client().Options().Dsn; got != firstDSN {
		t.Fatalf("expected initialization to be skipped, got DSN %q", got)
	}
}

func TestSetHealthchecks(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}

	SetHealthchecks(checker)
	if hc != checker {
		t.Fatal("expected healthchecks client to be set")
	}

	SetHealthchecks(nil)
	if hc != nil {
		t.Fatal("expected healthchecks client to be reset")
	}
}
