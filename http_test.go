package apm

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestWrapRoundTripperDefaults(t *testing.T) {
	rt := asAPMRoundTripper(t, WrapRoundTripper(nil))

	if rt.rt != http.DefaultTransport {
		t.Fatal("expected default transport")
	}
	if rt.maxRetries != MaxRetries {
		t.Fatalf("expected %d retries, got %d", MaxRetries, rt.maxRetries)
	}
	if rt.retryDelay != RetryDelay {
		t.Fatalf("expected retry delay %s, got %s", RetryDelay, rt.retryDelay)
	}
	if !rt.hcEnabled {
		t.Fatal("expected healthchecks to be enabled by default")
	}
}

func TestWrapRoundTripperOptions(t *testing.T) {
	rt := asAPMRoundTripper(t, WrapRoundTripper(
		nil,
		WithMaxRetries(2),
		WithRetryDelay(time.Millisecond),
		WithHealthchecks(false),
	))

	if rt.maxRetries != 2 {
		t.Fatalf("expected 2 retries, got %d", rt.maxRetries)
	}
	if rt.retryDelay != time.Millisecond {
		t.Fatalf("expected 1ms retry delay, got %s", rt.retryDelay)
	}
	if rt.hcEnabled {
		t.Fatal("expected healthchecks to be disabled")
	}
}

func TestWrapClient(t *testing.T) {
	resetAPM(t)
	client := &http.Client{Transport: http.DefaultTransport}

	wrapped := WrapClient(client)

	if wrapped != client {
		t.Fatal("expected the same client to be returned")
	}
	asAPMRoundTripper(t, wrapped.Transport)
}

func TestWrapClientDefault(t *testing.T) {
	resetAPM(t)
	previous := http.DefaultClient.Transport
	t.Cleanup(func() { http.DefaultClient.Transport = previous })

	wrapped := WrapClient(nil)

	if wrapped != http.DefaultClient {
		t.Fatal("expected the default client to be wrapped")
	}
	asAPMRoundTripper(t, wrapped.Transport)
}

func TestRoundTripSuccess(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	server := newTestHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := newTestRequest(t, http.MethodGet, server.URL, nil).WithContext(f.context(t))

	status := mustStatus(t, WrapRoundTripper(nil), req)

	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	events := f.tr.recorded()
	if len(events) != 1 {
		t.Fatalf("expected 1 transaction event, got %d", len(events))
	}
	event := events[0]
	name := http.MethodGet + " " + server.URL
	if event.Type != "transaction" || event.Transaction != name {
		t.Fatalf("unexpected transaction event: type=%q name=%q", event.Type, event.Transaction)
	}
	trace, ok := event.Contexts["trace"]
	if !ok {
		t.Fatal("expected trace context on transaction event")
	}
	if trace["op"] != "http.client" {
		t.Fatalf("unexpected transaction event op: %v", trace["op"])
	}
	if len(event.Spans) != 1 || event.Spans[0].Op != "http.client" {
		t.Fatalf("expected 1 http.client span, got %+v", event.Spans)
	}
}

func TestRoundTripKeepsExistingTransaction(t *testing.T) {
	resetAPM(t)
	f := newHubFixture(t)
	server := newTestHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	transaction := sentry.StartTransaction(f.context(t), "parent")
	req := newTestRequest(t, http.MethodGet, server.URL, nil).WithContext(transaction.Context())
	status := mustStatus(t, WrapRoundTripper(nil), req)

	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}

	transaction.Finish()
	transactions := 0
	for _, event := range f.tr.recorded() {
		if event.Type != "transaction" {
			continue
		}
		transactions++
		if event.Transaction != "parent" {
			t.Fatalf("unexpected transaction name: %q", event.Transaction)
		}
		if len(event.Spans) == 0 {
			t.Fatal("expected http.client span in transaction")
		}
	}
	if transactions != 1 {
		t.Fatalf("expected 1 transaction event, got %d", transactions)
	}
}

func newTestHTTPServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func newTestRequest(t *testing.T, method, target string, body io.Reader) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, target, body)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	return req
}

func mustStatus(t *testing.T, rt http.RoundTripper, req *http.Request) int {
	t.Helper()
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Errorf("close response body: %v", err)
		}
	}()
	return resp.StatusCode
}

func asAPMRoundTripper(t *testing.T, rt http.RoundTripper) *RoundTripper {
	t.Helper()
	apmRT, ok := rt.(*RoundTripper)
	if !ok {
		t.Fatalf("expected *RoundTripper, got %T", rt)
	}
	return apmRT
}
