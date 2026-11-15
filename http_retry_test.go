package apm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func TestRoundTripRetriesOn5xx(t *testing.T) {
	resetAPM(t)
	attempts := atomic.Int32{}
	server := newTestHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	rt := WrapRoundTripper(nil, WithMaxRetries(5), WithRetryDelay(time.Millisecond))

	status := mustStatus(t, rt, newTestRequest(t, http.MethodGet, server.URL, nil))

	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestRoundTripStopsAtMaxRetries(t *testing.T) {
	resetAPM(t)
	attempts := atomic.Int32{}
	server := newTestHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	rt := WrapRoundTripper(nil, WithMaxRetries(2), WithRetryDelay(time.Millisecond))

	status := mustStatus(t, rt, newTestRequest(t, http.MethodGet, server.URL, nil))

	if status != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", status)
	}
	if got := attempts.Load(); got != 3 {
		t.Fatalf("expected 3 attempts, got %d", got)
	}
}

func TestRoundTripBackoff(t *testing.T) {
	resetAPM(t)
	mu := sync.Mutex{}
	attemptsAt := make([]time.Time, 0, 3)
	server := newTestHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		attemptsAt = append(attemptsAt, time.Now())
		mu.Unlock()
		w.WriteHeader(http.StatusInternalServerError)
	})
	rt := WrapRoundTripper(nil, WithMaxRetries(2), WithRetryDelay(10*time.Millisecond))

	status := mustStatus(t, rt, newTestRequest(t, http.MethodGet, server.URL, nil))

	if status != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", status)
	}
	mu.Lock()
	times := append([]time.Time(nil), attemptsAt...)
	mu.Unlock()
	if len(times) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(times))
	}
	if gap := times[1].Sub(times[0]); gap < 10*time.Millisecond {
		t.Fatalf("expected at least 10ms before the first retry, got %s", gap)
	}
	if gap := times[2].Sub(times[1]); gap < 20*time.Millisecond {
		t.Fatalf("expected at least 20ms before the second retry, got %s", gap)
	}
}

func TestRoundTripNoRetryCases(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		opts         []RoundTripperOption
		wantAttempts int32
	}{
		{name: "client error", status: http.StatusBadRequest, wantAttempts: 1},
		{name: "retries disabled", status: http.StatusInternalServerError, opts: []RoundTripperOption{WithMaxRetries(0)}, wantAttempts: 1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetAPM(t)
			attempts := atomic.Int32{}
			server := newTestHTTPServer(t, func(w http.ResponseWriter, _ *http.Request) {
				attempts.Add(1)
				w.WriteHeader(tc.status)
			})
			opts := []RoundTripperOption{WithRetryDelay(time.Millisecond)}
			opts = append(opts, tc.opts...)
			rt := WrapRoundTripper(nil, opts...)

			status := mustStatus(t, rt, newTestRequest(t, http.MethodGet, server.URL, nil))

			if status != tc.status {
				t.Fatalf("expected status %d, got %d", tc.status, status)
			}
			if got := attempts.Load(); got != tc.wantAttempts {
				t.Fatalf("expected %d attempts, got %d", tc.wantAttempts, got)
			}
		})
	}
}

func TestRoundTripReplaysBody(t *testing.T) {
	resetAPM(t)
	mu := sync.Mutex{}
	bodies := make([]string, 0, 3)
	server := newTestHTTPServer(t, func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(data))
		attempt := len(bodies)
		mu.Unlock()
		if err != nil || attempt < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	rt := WrapRoundTripper(nil, WithMaxRetries(5), WithRetryDelay(time.Millisecond))
	req := newTestRequest(t, http.MethodPost, server.URL, strings.NewReader("payload"))

	status := mustStatus(t, rt, req)

	if status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", status)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(bodies))
	}
	for _, body := range bodies {
		if body != "payload" {
			t.Fatalf("unexpected request body: %q", body)
		}
	}
}

func TestRoundTripRetryBodyReadError(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)
	attempts := atomic.Int32{}
	rt := WrapRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts.Add(1)
		return nil, nil
	}), WithRetryDelay(time.Millisecond))
	req := newTestRequest(t, http.MethodPost, "http://example.invalid/resource", failingReader{})

	resp, err := rt.RoundTrip(req)

	if resp != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close response body: %v", closeErr)
		}
		t.Fatal("expected no response")
	}
	if err == nil || err.Error() != "read failed" {
		t.Fatalf("expected body read error, got %v", err)
	}
	if got := attempts.Load(); got != 0 {
		t.Fatalf("expected no transport attempts, got %d", got)
	}
	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
}

func TestRoundTripErrorFailsHealthcheck(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)
	dialErr := errors.New("dial failed")
	attempts := atomic.Int32{}
	rt := WrapRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
		attempts.Add(1)
		return nil, dialErr
	}), WithMaxRetries(3))
	req := newTestRequest(t, http.MethodGet, "http://example.invalid/resource", nil)

	resp, err := rt.RoundTrip(req)

	if resp != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close response body: %v", closeErr)
		}
		t.Fatal("expected no response")
	}
	if !errors.Is(err, dialErr) {
		t.Fatalf("expected dial error, got %v", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("expected no retries on transport error, got %d attempts", got)
	}
	if checker.count() != 1 {
		t.Fatalf("expected 1 healthcheck fail, got %d", checker.count())
	}
	body := checker.last()
	if !strings.Contains(body, "http.RoundTripper: GET http://example.invalid/resource failed") {
		t.Fatalf("unexpected healthcheck body: %q", body)
	}
	if !strings.Contains(body, "dial failed") {
		t.Fatalf("expected error in healthcheck body: %q", body)
	}
}

func TestRoundTripErrorWithoutHealthchecks(t *testing.T) {
	resetAPM(t)
	checker := &mockHealthchecks{}
	SetHealthchecks(checker)
	rt := WrapRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	}), WithHealthchecks(false))
	req := newTestRequest(t, http.MethodGet, "http://example.invalid/resource", nil)

	resp, err := rt.RoundTrip(req)

	if resp != nil {
		if closeErr := resp.Body.Close(); closeErr != nil {
			t.Errorf("close response body: %v", closeErr)
		}
		t.Fatal("expected no response")
	}
	if err == nil {
		t.Fatal("expected error")
	}
	if checker.count() != 0 {
		t.Fatalf("expected no healthcheck fails, got %d", checker.count())
	}
}

func TestRoundTripContextErrorsSkipHealthcheck(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{name: "canceled", err: context.Canceled},
		{name: "deadline exceeded", err: context.DeadlineExceeded},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetAPM(t)
			checker := &mockHealthchecks{}
			SetHealthchecks(checker)
			rt := WrapRoundTripper(roundTripFunc(func(*http.Request) (*http.Response, error) {
				return nil, fmt.Errorf("transport: %w", tc.err)
			}))
			req := newTestRequest(t, http.MethodGet, "http://example.invalid/resource", nil)

			resp, err := rt.RoundTrip(req)

			if resp != nil {
				if closeErr := resp.Body.Close(); closeErr != nil {
					t.Errorf("close response body: %v", closeErr)
				}
				t.Fatal("expected no response")
			}
			if !errors.Is(err, tc.err) {
				t.Fatalf("expected %v, got %v", tc.err, err)
			}
			if checker.count() != 0 {
				t.Fatalf("expected no healthcheck fails, got %d", checker.count())
			}
		})
	}
}
