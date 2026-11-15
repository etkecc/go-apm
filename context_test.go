package apm

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestNewContext(t *testing.T) {
	resetAPM(t)

	ctx := NewContext()
	if ctx == nil {
		t.Fatal("expected context")
	}

	logger := Log(ctx)
	if logger == nil {
		t.Fatal("expected logger in context")
	}
	if logger != ctx.Value(loggerKey{}) {
		t.Fatal("expected logger from context")
	}
	if !logger.Enabled(ctx, slog.LevelInfo) {
		t.Fatal("expected enabled logger")
	}
}

func TestNewContextPreservesParentValues(t *testing.T) {
	resetAPM(t)
	parent := context.WithValue(t.Context(), contextKey{}, "value")

	ctx := NewContext(parent)

	if got := ctx.Value(contextKey{}); got != "value" {
		t.Fatalf("expected parent value, got %v", got)
	}
}

func TestNewContextPreservesHub(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)

	ctx := NewContext(f.context(t))

	if got := sentry.GetHubFromContext(ctx); got != f.hub {
		t.Fatal("expected parent hub to be preserved")
	}
}

func TestNewContextSetsHub(t *testing.T) {
	resetAPM(t)
	sentryDSN = "https://key@example.com/1"

	current := sentry.CurrentHub()
	ctx := NewContext()
	got := sentry.GetHubFromContext(ctx)

	if got == nil {
		t.Fatal("expected a hub on the context when sentry is configured")
	}
	if got == current {
		t.Fatal("expected NewContext to clone the current hub")
	}
}

func TestNewContextCancellation(t *testing.T) {
	resetAPM(t)
	parent, cancel := context.WithCancel(t.Context())

	ctx := NewContext(parent)
	cancel()

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("expected canceled context, got %v", ctx.Err())
	}
}
