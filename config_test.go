package apm

import (
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"
)

func TestSetName(t *testing.T) {
	resetAPM(t)

	SetName("service")
	if sentryName != "service" {
		t.Fatalf("expected sentry name service, got %q", sentryName)
	}
}

func TestSetLogLevel(t *testing.T) {
	resetAPM(t)

	SetLogLevel("debug")
	if loglevel != zerolog.DebugLevel {
		t.Fatalf("expected debug log level, got %s", loglevel)
	}
	if got := zerolog.GlobalLevel(); got != zerolog.DebugLevel {
		t.Fatalf("expected global debug level, got %s", got)
	}

	SetLogLevel("not-a-level")
	if loglevel != zerolog.InfoLevel {
		t.Fatalf("expected info log level fallback, got %s", loglevel)
	}
	if got := zerolog.GlobalLevel(); got != zerolog.InfoLevel {
		t.Fatalf("expected global info level, got %s", got)
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
