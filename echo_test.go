package apm

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getsentry/sentry-go"
	"github.com/labstack/echo/v5"
)

func TestWithSentryFallbackPath(t *testing.T) {
	resetAPM(t)
	e := newTestEcho()
	transaction := (*sentry.Span)(nil)
	handler := WithSentry()(func(c *echo.Context) error {
		transaction = sentry.TransactionFromContext(c.Request().Context())
		return c.NoContent(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/direct", nil)

	if err := handler(e.NewContext(req, rec)); err != nil {
		t.Fatalf("unexpected handler error: %v", err)
	}

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}
	if transaction == nil {
		t.Fatal("expected transaction in request context")
	}
	if transaction.Name != "GET /direct" {
		t.Fatalf("unexpected transaction name: %q", transaction.Name)
	}
}

func TestWithSentryTransaction(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	e := newTestEcho()
	transaction := (*sentry.Span)(nil)

	e.Use(WithSentry())
	e.GET("/users/:id", func(c *echo.Context) error {
		transaction = sentry.TransactionFromContext(c.Request().Context())
		return c.String(http.StatusOK, "ok")
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/users/42", nil)
	req = req.WithContext(sentry.SetHubOnContext(req.Context(), f.hub))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if transaction == nil {
		t.Fatal("expected transaction in request context")
	}
	if transaction.Name != "GET /users/:id" {
		t.Fatalf("unexpected transaction name: %q", transaction.Name)
	}
	if transaction.Op != "http.server" {
		t.Fatalf("unexpected transaction op: %q", transaction.Op)
	}
	if transaction.Source != sentry.SourceURL {
		t.Fatalf("unexpected transaction source: %q", transaction.Source)
	}
	if transaction.Status != sentry.SpanStatusOK {
		t.Fatalf("unexpected transaction status: %s", transaction.Status)
	}

	events := f.tr.recorded()
	if len(events) != 1 {
		t.Fatalf("expected 1 transaction event, got %d", len(events))
	}
	event := events[0]
	if event.Type != "transaction" || event.Transaction != "GET /users/:id" {
		t.Fatalf("unexpected transaction event: type=%q name=%q", event.Type, event.Transaction)
	}
	trace, ok := event.Contexts["trace"]
	if !ok {
		t.Fatal("expected trace context on transaction event")
	}
	if trace["op"] != "http.server" {
		t.Fatalf("unexpected transaction event op: %v", trace["op"])
	}
	if trace["status"] != sentry.SpanStatusOK {
		t.Fatalf("unexpected transaction event status: %v", trace["status"])
	}
}

func TestWithSentrySkipsHealthPath(t *testing.T) {
	resetAPM(t)
	e := newTestEcho()
	noTransaction := false

	e.Use(WithSentry())
	e.GET("/_health", func(c *echo.Context) error {
		noTransaction = sentry.TransactionFromContext(c.Request().Context()) == nil
		return c.NoContent(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/_health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	if !noTransaction {
		t.Fatal("expected health path to skip transaction creation")
	}
}

func TestWithSentrySetsErrorStatus(t *testing.T) {
	resetAPM(t)
	e := newTestEcho()
	transaction := (*sentry.Span)(nil)

	e.Use(WithSentry())
	e.GET("/fail", func(c *echo.Context) error {
		transaction = sentry.TransactionFromContext(c.Request().Context())
		if err := c.String(http.StatusBadGateway, "bad"); err != nil {
			return err
		}
		return errors.New("handler failed")
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/fail", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", rec.Code)
	}
	if transaction == nil {
		t.Fatal("expected transaction in request context")
	}
	if transaction.Status != sentry.SpanStatusInternalError {
		t.Fatalf("unexpected transaction status: %s", transaction.Status)
	}
}

func TestWithSentryUsesHTTPErrorStatus(t *testing.T) {
	resetAPM(t)
	e := newTestEcho()
	transaction := (*sentry.Span)(nil)

	e.Use(WithSentry())
	e.GET("/missing", func(c *echo.Context) error {
		transaction = sentry.TransactionFromContext(c.Request().Context())
		return echo.ErrNotFound
	})

	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/missing", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rec.Code)
	}
	if transaction == nil {
		t.Fatal("expected transaction in request context")
	}
	if transaction.Status != sentry.SpanStatusNotFound {
		t.Fatalf("unexpected transaction status: %s", transaction.Status)
	}
}

func TestWithSentryRepanics(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)
	e := newTestEcho()

	e.Use(WithSentry())
	e.GET("/panic", func(*echo.Context) error {
		panic("boom")
	})

	recovered := catchPanic(func() {
		e.ServeHTTP(
			httptest.NewRecorder(),
			httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/panic", nil),
		)
	})

	if recovered != "boom" {
		t.Fatalf("expected panic value boom, got %v", recovered)
	}
	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
	if !strings.Contains(checker.last(), "panic recovered: boom") {
		t.Fatalf("unexpected healthcheck body: %q", checker.last())
	}
}

func TestWithSentrySetsRequestScope(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	e := newTestEcho()
	usedHub := (*sentry.Hub)(nil)

	e.Use(WithSentry())
	e.GET("/scoped", func(c *echo.Context) error {
		usedHub = GetHub(c.Request().Context())
		usedHub.CaptureMessage("scoped message")
		return c.NoContent(http.StatusOK)
	})

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/scoped", nil)
	req = req.WithContext(sentry.SetHubOnContext(req.Context(), f.hub))
	e.ServeHTTP(httptest.NewRecorder(), req)

	if usedHub != f.hub {
		t.Fatal("expected request hub to be preserved")
	}
	messageEvent := (*sentry.Event)(nil)
	for _, event := range f.tr.recorded() {
		if event.Message == "scoped message" {
			messageEvent = event
		}
	}
	if messageEvent == nil {
		t.Fatal("expected captured message event")
	}
	if messageEvent.Request == nil {
		t.Fatal("expected request to be attached by middleware")
	}
	if !strings.HasSuffix(messageEvent.Request.URL, "/scoped") {
		t.Fatalf("unexpected request URL: %q", messageEvent.Request.URL)
	}
}

func newTestEcho() *echo.Echo {
	e := echo.New()
	e.Logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return e
}
