package langfuse

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fakeOTLP stands in for Langfuse's OTLP/HTTP traces endpoint.
type fakeOTLP struct {
	mu       sync.Mutex
	requests []*http.Request
	block    chan struct{} // non-nil: hang until closed, like an unreachable Langfuse
}

func (f *fakeOTLP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.requests = append(f.requests, r)
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		case <-r.Context().Done():
		}
	}
	w.WriteHeader(http.StatusOK)
}

func (f *fakeOTLP) received() []*http.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*http.Request(nil), f.requests...)
}

func TestTracingIsOffUnlessFullyConfigured(t *testing.T) {
	fake := &fakeOTLP{}
	server := httptest.NewServer(fake)
	defer server.Close()

	for _, cfg := range []Config{
		{},
		{BaseURL: server.URL, PublicKey: "pk-lf-1"},
		{BaseURL: server.URL, SecretKey: "sk-lf-1"},
		{PublicKey: "pk-lf-1", SecretKey: "sk-lf-1"},
	} {
		tracing, err := NewTracing(context.Background(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		if tracing.Enabled() {
			t.Fatalf("%+v: tracing enabled", cfg)
		}
		_, span := tracing.TracerProvider().Tracer("test").Start(context.Background(), "x")
		span.End()
		if err := tracing.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(fake.received()); got != 0 {
		t.Fatalf("export attempted %d times with Langfuse unconfigured", got)
	}
}

func TestPendingSpansAreFlushedOnShutdownWithLangfuseAuth(t *testing.T) {
	fake := &fakeOTLP{}
	server := httptest.NewServer(fake)
	defer server.Close()

	tracing, err := NewTracing(context.Background(), Config{BaseURL: server.URL + "/", PublicKey: "pk-lf-1", SecretKey: "sk-lf-1"})
	if err != nil {
		t.Fatal(err)
	}
	if !tracing.Enabled() {
		t.Fatal("tracing disabled with Langfuse configured")
	}
	_, span := tracing.TracerProvider().Tracer("test").Start(context.Background(), "x")
	span.End()
	if got := len(fake.received()); got != 0 {
		t.Fatalf("span exported before the batch was due (%d requests)", got)
	}

	if err := tracing.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	requests := fake.received()
	if len(requests) != 1 {
		t.Fatalf("Shutdown sent %d requests, want 1", len(requests))
	}
	r := requests[0]
	if r.Method != http.MethodPost || r.URL.Path != "/api/public/otel/v1/traces" {
		t.Fatalf("request %s %s, want POST /api/public/otel/v1/traces", r.Method, r.URL.Path)
	}
	if want := "Basic " + base64.StdEncoding.EncodeToString([]byte("pk-lf-1:sk-lf-1")); r.Header.Get("Authorization") != want {
		t.Fatalf("Authorization = %q, want %q", r.Header.Get("Authorization"), want)
	}
	if got := r.Header.Get("x-langfuse-ingestion-version"); got != "4" {
		t.Fatalf("x-langfuse-ingestion-version = %q, want 4", got)
	}
}

func TestUnreachableLangfuseNeverBlocksSpans(t *testing.T) {
	fake := &fakeOTLP{block: make(chan struct{})}
	server := httptest.NewServer(fake)
	defer server.Close()
	defer close(fake.block)

	tracing, err := NewTracing(context.Background(), Config{BaseURL: server.URL, PublicKey: "pk", SecretKey: "sk"})
	if err != nil {
		t.Fatal(err)
	}
	tracer := tracing.TracerProvider().Tracer("test")
	start := time.Now()
	for range 5000 { // more than the batch queue holds: overflow is dropped, not waited on
		_, span := tracer.Start(context.Background(), "x")
		span.End()
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("ending spans took %v with Langfuse hanging", elapsed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start = time.Now()
	_ = tracing.Shutdown(ctx) // an error here is fine; returning on time is the point
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Shutdown took %v past its deadline", elapsed)
	}
}
