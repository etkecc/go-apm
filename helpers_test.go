package apm

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

type testTransport struct {
	mu      sync.Mutex
	events  []*sentry.Event
	flushes int
}

type hubFixture struct {
	hub *sentry.Hub
	tr  *testTransport
}

type mockHealthchecks struct {
	mu     sync.Mutex
	bodies []string
}

type contextKey struct{}

func (t *testTransport) Flush(time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.flushes++
	return true
}

func (t *testTransport) FlushWithContext(context.Context) bool {
	return t.Flush(0)
}

func (t *testTransport) Configure(sentry.ClientOptions) {}

func (t *testTransport) SendEvent(event *sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, event)
}

func (t *testTransport) Close() {}

func (t *testTransport) flushCount() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.flushes
}

func (t *testTransport) recorded() []*sentry.Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]*sentry.Event(nil), t.events...)
}

func (m *mockHealthchecks) Fail(optionalBody ...io.Reader) {
	parts := make([]string, 0, len(optionalBody))
	for _, body := range optionalBody {
		data, err := io.ReadAll(body)
		if err != nil {
			parts = append(parts, "read error: "+err.Error())
			continue
		}
		parts = append(parts, string(data))
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.bodies = append(m.bodies, strings.Join(parts, "\n"))
}

func (m *mockHealthchecks) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.bodies)
}

func (m *mockHealthchecks) last() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.bodies) == 0 {
		return ""
	}
	return m.bodies[len(m.bodies)-1]
}

func newHubFixture(t *testing.T) *hubFixture {
	t.Helper()

	tr := &testTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:              "https://key@example.com/1",
		Transport:        tr,
		EnableTracing:    true,
		TracesSampleRate: 1.0,
	})
	if err != nil {
		t.Fatalf("create sentry client: %v", err)
	}
	t.Cleanup(client.Close)

	hub := sentry.NewHub(client, sentry.NewScope())
	return &hubFixture{
		hub: hub,
		tr:  tr,
	}
}

func (f *hubFixture) context(t *testing.T) context.Context {
	t.Helper()
	return sentry.SetHubOnContext(t.Context(), f.hub)
}

func resetAPM(t *testing.T) {
	t.Helper()

	prevHC := hc
	prevLevel := loglevel.Level()
	prevInitialized := initialized
	prevDSN := sentryDSN
	prevName := sentryName
	prevClient := sentry.CurrentHub().Client()

	hc = nil
	loglevel.Set(slog.LevelInfo)
	initialized = false
	sentryDSN = ""
	sentryName = ""

	t.Cleanup(func() {
		hc = prevHC
		loglevel.Set(prevLevel)
		initialized = prevInitialized
		sentryDSN = prevDSN
		sentryName = prevName
		sentry.CurrentHub().BindClient(prevClient)
	})
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	file, err := os.CreateTemp(t.TempDir(), "stdout-*")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}

	prev := os.Stdout
	os.Stdout = file
	defer func() {
		os.Stdout = prev
		if err := file.Close(); err != nil {
			t.Errorf("close temp file: %v", err)
		}
	}()

	fn()

	os.Stdout = prev
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		t.Fatalf("seek temp file: %v", err)
	}
	out, err := io.ReadAll(file)
	if err != nil {
		t.Fatalf("read temp file: %v", err)
	}

	return string(out)
}

func catchPanic(fn func()) (recovered any) {
	defer func() { recovered = recover() }()
	fn()
	return nil
}
