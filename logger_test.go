package apm

import (
	"encoding/json"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestNewLoggerPlain(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	out := captureStdout(t, func() {
		NewLoggerPlain().Error("plain message")
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
		NewLogger(t.Context()).Error("logger message")
	})

	if !strings.Contains(out, "logger message") {
		t.Fatalf("expected log output, got %q", out)
	}
	if !strings.Contains(out, "source=") {
		t.Fatalf("expected caller metadata, got %q", out)
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
		logger.Warn("breadcrumb message")
		logger.Error("sentry logger message", "user_id", 7, "error", errors.New("boom"))
	})

	if !strings.Contains(out, "sentry logger message") {
		t.Fatalf("expected log output, got %q", out)
	}
	events := f.tr.recorded()
	if len(events) != 1 {
		t.Fatalf("expected 1 sentry event, got %d", len(events))
	}
	event := events[0]
	if event.Message != "sentry logger message" {
		t.Fatalf("unexpected sentry message: %q", event.Message)
	}
	if event.Level != sentry.LevelError {
		t.Fatalf("unexpected sentry level: %q", event.Level)
	}
	if event.Logger != "slog" {
		t.Fatalf("unexpected sentry logger: %q", event.Logger)
	}
	if got := event.Tags["user_id"]; got != "7" {
		t.Fatalf("unexpected sentry tags: %v", event.Tags)
	}
	if len(event.Exception) != 1 || event.Exception[0].Value != "boom" {
		t.Fatalf("unexpected sentry exception: %+v", event.Exception)
	}
	if len(event.Breadcrumbs) != 1 || event.Breadcrumbs[0].Message != "breadcrumb message" {
		t.Fatalf("unexpected breadcrumbs: %+v", event.Breadcrumbs)
	}
	if event.Breadcrumbs[0].Level != sentry.LevelWarning {
		t.Fatalf("unexpected breadcrumb level: %q", event.Breadcrumbs[0].Level)
	}
}

func TestApplyEventFields(t *testing.T) {
	event := sentry.NewEvent()

	applyEventFields(event, map[string]any{
		slog.MessageKey: "boom",
		slog.LevelKey:   "ERROR",
		"error":         "boom",
		"transaction":   "tx",
		"fingerprint":   []any{"one", "two"},
		"user":          map[string]any{"id": "42"},
		"order_id":      json.Number("9007199254740993"),
		"nested":        map[string]any{"path": "/x", "count": json.Number("2")},
	})

	if len(event.Exception) != 1 || event.Exception[0].Value != "boom" {
		t.Fatalf("unexpected exception: %+v", event.Exception)
	}
	if event.Transaction != "tx" {
		t.Fatalf("unexpected transaction: %q", event.Transaction)
	}
	if !slices.Equal(event.Fingerprint, []string{"one", "two"}) {
		t.Fatalf("unexpected fingerprint: %v", event.Fingerprint)
	}
	if event.User.ID != "42" {
		t.Fatalf("unexpected user: %+v", event.User)
	}
	if got := event.Tags["order_id"]; got != "9007199254740993" {
		t.Fatalf("expected large integer to keep precision, got %q", got)
	}
	if got := event.Tags["nested.path"]; got != "/x" {
		t.Fatalf("unexpected nested tag: %q", got)
	}
	if _, ok := event.Tags[slog.MessageKey]; ok {
		t.Fatal("expected message to stay out of tags")
	}
	if _, ok := event.Tags["error"]; ok {
		t.Fatal("expected error to stay out of tags")
	}
}

func TestReportHandlerOpsAreIndependent(t *testing.T) {
	base := &reportHandler{next: slog.DiscardHandler, ops: make([]handlerOp, 1, 4)}

	first, ok := base.WithAttrs([]slog.Attr{slog.Int("first", 1)}).(*reportHandler)
	if !ok {
		t.Fatal("expected a report handler")
	}
	second, ok := base.WithAttrs([]slog.Attr{slog.Int("second", 2)}).(*reportHandler)
	if !ok {
		t.Fatal("expected a report handler")
	}

	if got := first.ops[1].attrs[0].Key; got != "first" {
		t.Fatalf("expected the first handler to keep its attrs, got %q", got)
	}
	if got := second.ops[1].attrs[0].Key; got != "second" {
		t.Fatalf("expected the second handler to keep its attrs, got %q", got)
	}
}

func TestNewLoggerWithoutDSN(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)

	out := captureStdout(t, func() {
		NewLogger(f.context(t)).Error("no dsn")
	})

	if !strings.Contains(out, "no dsn") {
		t.Fatalf("expected log output, got %q", out)
	}
	if events := f.tr.recorded(); len(events) != 0 {
		t.Fatalf("expected no sentry events without a DSN, got %d", len(events))
	}
}

func TestLog(t *testing.T) {
	resetAPM(t)
	ctx := NewContext()

	logger := Log(ctx)
	if logger != ctx.Value(loggerKey{}) {
		t.Fatal("expected logger from context")
	}
	if !logger.Enabled(ctx, slog.LevelInfo) {
		t.Fatal("expected enabled logger")
	}
	if Log(t.Context()) != silentLogger {
		t.Fatal("expected a silent logger without an apm logger in context")
	}
	fresh := Log()
	if fresh == silentLogger {
		t.Fatal("expected a fresh logger without a context")
	}
	if !fresh.Enabled(t.Context(), slog.LevelInfo) {
		t.Fatal("expected an enabled fresh logger")
	}
}

func TestNewLoggerHonorsLevel(t *testing.T) {
	resetAPM(t)
	SetLogLevel("fatal")
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	out := captureStdout(t, func() {
		logger := NewLogger(t.Context())
		logger.Info("hidden info")
		logger.Error("hidden error")
		logger.Log(t.Context(), slog.LevelError+4, "shown fatal")
	})

	if strings.Contains(out, "hidden") {
		t.Fatalf("expected records below the level to be filtered, got %q", out)
	}
	if !strings.Contains(out, "shown fatal") {
		t.Fatalf("expected fatal record, got %q", out)
	}
	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
}
