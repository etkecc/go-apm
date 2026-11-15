package apm

import (
	"errors"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestError(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	Error(f.context(t), errors.New("boom"))

	events := f.tr.recorded()
	if len(events) != 1 {
		t.Fatalf("expected 1 sentry event, got %d", len(events))
	}
	if len(events[0].Exception) == 0 || events[0].Exception[0].Value != "boom" {
		t.Fatalf("expected captured exception boom, got %+v", events[0].Exception)
	}
	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
	if !strings.Contains(checker.last(), "error: boom") {
		t.Fatalf("unexpected healthcheck body: %q", checker.last())
	}
}

func TestErrorNil(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	Error(f.context(t), nil)

	if events := f.tr.recorded(); len(events) != 0 {
		t.Fatalf("expected no sentry events, got %d", len(events))
	}
	if checker.count() != 0 {
		t.Fatalf("expected no healthcheck fails, got %d", checker.count())
	}
}

func TestErrorWithoutHub(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	Error(t.Context(), errors.New("boom"))

	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
}

func TestStartSpanCreatesTransaction(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)

	span := StartSpan(f.context(t), "operation")
	if span == nil {
		t.Fatal("expected span")
	}
	if span.Op != "operation" || span.Description != "operation" {
		t.Fatalf("unexpected span op/description: %q/%q", span.Op, span.Description)
	}

	transaction := sentry.TransactionFromContext(span.Context())
	if transaction == nil {
		t.Fatal("expected transaction in span context")
	}
	if transaction.Name != "operation" {
		t.Fatalf("unexpected transaction name: %q", transaction.Name)
	}
	if transaction.TraceID != span.TraceID {
		t.Fatal("span trace ID differs from transaction trace ID")
	}
	if span.ParentSpanID != transaction.SpanID {
		t.Fatal("span parent does not match transaction")
	}
	if transaction.SpanID == span.SpanID {
		t.Fatal("expected a child span, got the transaction itself")
	}
}

func TestStartSpanWithExistingTransaction(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	transaction := sentry.StartTransaction(f.context(t), "parent")

	span := StartSpan(transaction.Context(), "child")

	if sentry.TransactionFromContext(span.Context()) != transaction {
		t.Fatal("expected existing transaction to be reused")
	}
	if span.ParentSpanID != transaction.SpanID {
		t.Fatal("span parent does not match existing transaction")
	}
	if span.TraceID != transaction.TraceID {
		t.Fatal("span trace ID differs from existing transaction")
	}
}

func TestStartSpanWithExistingSpan(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	transaction := sentry.StartTransaction(f.context(t), "parent")
	child := sentry.StartSpan(transaction.Context(), "existing child")

	span := StartSpan(child.Context(), "nested")

	if span.ParentSpanID != child.SpanID {
		t.Fatal("expected span to be parented by the current span")
	}
	if sentry.TransactionFromContext(span.Context()) != transaction {
		t.Fatal("expected transaction to be reused")
	}
}

func TestGetHub(t *testing.T) {
	f := newHubFixture(t)

	if got := GetHub(f.context(t)); got != f.hub {
		t.Fatal("expected hub from context")
	}
	if got := GetHub(t.Context()); got == nil || got == sentry.CurrentHub() {
		t.Fatal("expected a clone of the current hub")
	}
	if got := GetHub(); got == nil || got == sentry.CurrentHub() {
		t.Fatal("expected a clone of the current hub")
	}
	if got := GetHub(t.Context(), f.context(t)); got == f.hub {
		t.Fatal("expected only the first context to be used")
	}
}

func TestFlush(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)

	Flush(f.context(t))
	Flush(f.context(t))

	if got := f.tr.flushCount(); got != 2 {
		t.Fatalf("expected 2 sentry flushes, got %d", got)
	}
}

func TestRecoverNil(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	Recover(nil, false, f.context(t))

	if events := f.tr.recorded(); len(events) != 0 {
		t.Fatalf("expected no sentry events, got %d", len(events))
	}
	if checker.count() != 0 {
		t.Fatalf("expected no healthcheck fails, got %d", checker.count())
	}
	if got := f.tr.flushCount(); got != 1 {
		t.Fatalf("expected 1 sentry flush, got %d", got)
	}
}

func TestRecover(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	Recover(errors.New("boom"), false, f.context(t))

	if events := f.tr.recorded(); len(events) != 1 {
		t.Fatalf("expected 1 sentry event, got %d", len(events))
	}
	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
	if !strings.Contains(checker.last(), "panic recovered: boom") {
		t.Fatalf("unexpected healthcheck body: %q", checker.last())
	}
	if got := f.tr.flushCount(); got != 1 {
		t.Fatalf("expected 1 sentry flush, got %d", got)
	}
}

func TestRecoverNilRepanic(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)

	Recover(nil, true, f.context(t))

	if events := f.tr.recorded(); len(events) != 0 {
		t.Fatalf("expected no sentry events, got %d", len(events))
	}
	if got := f.tr.flushCount(); got != 1 {
		t.Fatalf("expected 1 sentry flush, got %d", got)
	}
}

func TestRecoverWithoutContext(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)

	Recover(errors.New("boom"), false)

	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
}

func TestRecoverRepanic(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)

	recovered := catchPanic(func() {
		Recover("boom", true, f.context(t))
	})

	if recovered != "boom" {
		t.Fatalf("expected panic value boom, got %v", recovered)
	}
	if events := f.tr.recorded(); len(events) != 1 {
		t.Fatalf("expected 1 sentry event, got %d", len(events))
	}
	if got := f.tr.flushCount(); got != 1 {
		t.Fatalf("expected 1 sentry flush, got %d", got)
	}
}

func TestGetStackTrace(t *testing.T) {
	stack := GetStackTrace()
	if stack == "" {
		t.Fatal("expected non-empty stack trace")
	}
	if !strings.Contains(stack, "testing.tRunner") {
		t.Fatalf("expected testing frames in stack trace:\n%s", stack)
	}
	for line := range strings.SplitSeq(stack, "\n") {
		if strings.TrimSpace(line) == "" {
			t.Fatal("stack trace contains empty lines")
		}
		if strings.Contains(line, "go-apm") {
			t.Fatalf("stack trace contains skipped package frame: %q", line)
		}
	}
}
