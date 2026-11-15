package apm

import (
	"context"
	"errors"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/rs/zerolog"
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
	if logger != zerolog.Ctx(ctx) {
		t.Fatal("expected logger from context")
	}
	if logger.GetLevel() == zerolog.Disabled {
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

func TestNewContextCancellation(t *testing.T) {
	resetAPM(t)
	parent, cancel := context.WithCancel(t.Context())

	ctx := NewContext(parent)
	cancel()

	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("expected canceled context, got %v", ctx.Err())
	}
}
