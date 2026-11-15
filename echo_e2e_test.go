package apm

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/labstack/echo/v5"
)

func TestWithSentryE2E(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	sentry.CurrentHub().BindClient(f.hub.Client())
	sentryDSN = "https://key@example.com/1"
	e := newTestEcho()
	e.Use(WithSentry())

	handlerLogger := (*slog.Logger)(nil)
	e.GET("/users/:id", func(c *echo.Context) error {
		handlerLogger = Log(c.Request().Context())
		return c.String(http.StatusOK, "user "+c.Param("id"))
	})
	e.GET("/log-error", func(c *echo.Context) error {
		Log(c.Request().Context()).Error("request failed", "code", 7)
		return echo.NewHTTPError(http.StatusInternalServerError, "failed")
	})
	e.GET("/_health", func(c *echo.Context) error {
		return c.NoContent(http.StatusOK)
	})

	baseURL, stop := startServer(t, e)
	defer stop()

	client := &http.Client{Timeout: 5 * time.Second}
	ctx := sentry.SetHubOnContext(t.Context(), f.hub)

	status, body := get(ctx, t, client, baseURL+"/users/42")
	if status != http.StatusOK || body != "user 42" {
		t.Fatalf("unexpected users response: status=%d body=%q", status, body)
	}
	if handlerLogger == nil {
		t.Fatal("expected a logger in the request context")
	}
	if !handlerLogger.Enabled(ctx, slog.LevelInfo) {
		t.Fatal("expected an enabled logger in the request context")
	}

	if status, _ := get(ctx, t, client, baseURL+"/_health"); status != http.StatusOK {
		t.Fatalf("unexpected health status: %d", status)
	}
	if status, _ := get(ctx, t, client, baseURL+"/missing"); status != http.StatusNotFound {
		t.Fatalf("unexpected missing status: %d", status)
	}
	captureStdout(t, func() {
		if status, _ := get(ctx, t, client, baseURL+"/log-error"); status != http.StatusInternalServerError {
			t.Fatalf("unexpected log-error status: %d", status)
		}
	})

	var transactions []*sentry.Event
	waitFor(t, func() bool {
		transactions = transactions[:0]
		for _, event := range f.tr.recorded() {
			if event.Type == "transaction" {
				transactions = append(transactions, event)
			}
		}
		return len(transactions) == 3
	})

	byName := make(map[string]*sentry.Event, len(transactions))
	for _, event := range transactions {
		byName[event.Transaction] = event
	}
	usersEvent, ok := byName["GET /users/:id"]
	if !ok {
		t.Fatalf("expected transaction for /users/:id, got %+v", byName)
	}
	if usersEvent.Request == nil || !strings.HasSuffix(usersEvent.Request.URL, "/users/42") {
		t.Fatalf("expected request on transaction event, got %+v", usersEvent.Request)
	}
	if got := usersEvent.Contexts["trace"]["status"]; got != sentry.SpanStatusOK {
		t.Fatalf("unexpected users transaction status: %v", got)
	}
	missingEvent, ok := byName["GET /missing"]
	if !ok {
		t.Fatalf("expected transaction for /missing, got %+v", byName)
	}
	if got := missingEvent.Contexts["trace"]["status"]; got != sentry.SpanStatusNotFound {
		t.Fatalf("unexpected missing transaction status: %v", got)
	}

	logged := (*sentry.Event)(nil)
	for _, event := range f.tr.recorded() {
		if event.Message == "request failed" {
			logged = event
		}
	}
	if logged == nil {
		t.Fatal("expected an error event from the logged record")
	}
	if logged.Request == nil || !strings.HasSuffix(logged.Request.URL, "/log-error") {
		t.Fatalf("expected request on the logged error event, got %+v", logged.Request)
	}
	if got := logged.Tags["code"]; got != "7" {
		t.Fatalf("unexpected code tag on the logged error event: %q", got)
	}
}

func TestWithSentryRequestScopeIsolation(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	sentry.CurrentHub().BindClient(f.hub.Client())
	sentryDSN = "https://key@example.com/1"
	e := newTestEcho()
	e.Use(WithSentry())
	e.GET("/:name", func(c *echo.Context) error {
		Log(c.Request().Context()).Error("request failed", "name", c.Param("name"))
		return c.NoContent(http.StatusInternalServerError)
	})

	captureStdout(t, func() {
		var wg sync.WaitGroup
		for _, name := range []string{"alpha", "beta"} {
			wg.Go(func() {
				rec := httptest.NewRecorder()
				req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/"+name, nil)
				e.ServeHTTP(rec, req)
				if rec.Code != http.StatusInternalServerError {
					t.Errorf("unexpected status for %s: %d", name, rec.Code)
				}
			})
		}
		wg.Wait()
	})

	var events []*sentry.Event
	waitFor(t, func() bool {
		events = events[:0]
		for _, event := range f.tr.recorded() {
			if event.Message == "request failed" {
				events = append(events, event)
			}
		}
		return len(events) == 2
	})

	for _, event := range events {
		if event.Request == nil {
			t.Fatal("expected request on the logged error event")
		}
		name := event.Tags["name"]
		if name == "" || !strings.HasSuffix(event.Request.URL, "/"+name) {
			t.Fatalf("request scope leaked between requests: url=%q name=%q", event.Request.URL, name)
		}
	}
}

// startServer boots e on a random loopback port and returns its base URL plus a stop func
func startServer(t *testing.T, e *echo.Echo) (baseURL string, stop func()) {
	t.Helper()
	addrs := make(chan net.Addr, 1)
	sc := echo.StartConfig{
		Address:          "127.0.0.1:0",
		HideBanner:       true,
		HidePort:         true,
		ListenerAddrFunc: func(addr net.Addr) { addrs <- addr },
	}
	ctx, cancel := context.WithCancel(t.Context())
	errCh := make(chan error, 1)
	go func() { errCh <- sc.Start(ctx, e) }()

	var addr net.Addr
	select {
	case addr = <-addrs:
	case err := <-errCh:
		cancel()
		t.Fatalf("server exited before listening: %v", err)
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("timed out waiting for the listener")
	}

	return "http://" + addr.String(), func() {
		cancel()
		select {
		case err := <-errCh:
			if err != nil {
				t.Errorf("server stop: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("timed out waiting for server shutdown")
		}
	}
}

// get performs a GET request and returns the status and body
func get(ctx context.Context, t *testing.T, client *http.Client, url string) (status int, body string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close response body: %v", closeErr)
		}
	}()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return resp.StatusCode, string(data)
}

// waitFor polls cond until it returns true or the deadline expires
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	for {
		if cond() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("condition not met before the deadline")
		case <-time.After(5 * time.Millisecond):
		}
	}
}
