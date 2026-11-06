package digestartifact_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/felipeafreitas/agregado/internal/ai"
	"github.com/felipeafreitas/agregado/internal/digestartifact"
)

const traceModel = "@cf/google/gemma-4-26b-a4b-it"

// selectStub answers like Workers AI's native /ai/run endpoint with a fixed
// selection, or with a Workers AI error when fail is set.
type selectStub struct {
	mu     sync.Mutex
	calls  int
	answer string
	fail   bool
}

func (s *selectStub) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if s.fail {
		fmt.Fprint(w, `{"success":false,"errors":[{"code":3040,"message":"capacity exceeded"}]}`)
		return
	}
	out, _ := json.Marshal(s.answer)
	fmt.Fprintf(w, `{"success":true,"result":{"choices":[{"message":{"content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":300,"completion_tokens":12}}}`, out)
}

var (
	traceDay   = time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	traceSince = time.Date(2026, 10, 29, 6, 0, 0, 0, time.UTC)
	traceUntil = time.Date(2026, 10, 30, 6, 0, 0, 0, time.UTC)
)

// traceArticles yields candidates a, b (pass the floor of 3) and c (an
// exploration top-up), so 3 candidates and 2 floor passes.
func traceArticles() []digestartifact.Article {
	return []digestartifact.Article{
		{ID: "a", CanonicalURL: "https://a", Title: "A", Score: 5, Source: "one"},
		{ID: "b", CanonicalURL: "https://b", Title: "B", Score: 4, Source: "two"},
		{ID: "c", CanonicalURL: "https://c", Title: "C", Score: 2, Source: "three"},
	}
}

type tracedDigest struct {
	service *digestartifact.Service
	stub    *selectStub
	spans   *tracetest.InMemoryExporter
}

func newTracedDigest(t *testing.T, stub *selectStub, articles []digestartifact.Article) tracedDigest {
	t.Helper()
	server := httptest.NewServer(stub)
	t.Cleanup(server.Close)
	spans := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))
	provider := ai.NewCloudflareProvider("acct", "token", traceModel, 0, 0, nil, nil, nil,
		ai.WithBaseURL(server.URL), ai.WithTracerProvider(tp))
	store := digestartifact.NewMemoryStore(articles, traceSince, traceUntil)
	service := digestartifact.NewService(store, provider, "https://agregado.example", 3, 10,
		digestartifact.WithTracerProvider(tp))
	return tracedDigest{service: service, stub: stub, spans: spans}
}

func spanNamed(t *testing.T, spans tracetest.SpanStubs, name string) tracetest.SpanStub {
	t.Helper()
	for _, s := range spans {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no span %q among %v", name, spanNames(spans))
	return tracetest.SpanStub{}
}

func spanNames(spans tracetest.SpanStubs) []string {
	names := make([]string, len(spans))
	for i, s := range spans {
		names[i] = s.Name
	}
	return names
}

func attr(s tracetest.SpanStub, key string) (attribute.Value, bool) {
	for _, kv := range s.Attributes {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func wantAttr(t *testing.T, s tracetest.SpanStub, key, want string) {
	t.Helper()
	v, ok := attr(s, key)
	if !ok {
		t.Fatalf("span %q has no %s attribute", s.Name, key)
	}
	if got := v.Emit(); got != want {
		t.Fatalf("span %q %s = %q, want %q", s.Name, key, got, want)
	}
}

func TestComputedDigestProducesOneTraceKeyedByDate(t *testing.T) {
	run := newTracedDigest(t, &selectStub{answer: `{"article_ids":["c","a"]}`}, traceArticles())
	artifact, created, err := run.service.ForDate(context.Background(), traceDay)
	if err != nil || !created || artifact.SelectedCount != 2 {
		t.Fatalf("ForDate() = %+v, %v, %v", artifact, created, err)
	}

	spans := run.spans.GetSpans()
	if len(spans) != 2 {
		t.Fatalf("spans = %v, want digest and one select generation", spanNames(spans))
	}
	root := spanNamed(t, spans, "digest")
	// sha256("digest:20261030")[:16], computed independently of TraceID.
	const wantTrace = "b4f16bf8f8258d554a9689d65f43151c"
	if got := root.SpanContext.TraceID().String(); got != wantTrace {
		t.Fatalf("digest trace = %s, want %s", got, wantTrace)
	}
	gen := spanNamed(t, spans, ai.OpDigestSelect)
	if gen.SpanContext.TraceID() != root.SpanContext.TraceID() || gen.Parent.SpanID() != root.SpanContext.SpanID() {
		t.Fatalf("select generation is not a child of the digest span")
	}
	wantAttr(t, gen, "langfuse.observation.type", "generation")

	wantAttr(t, root, "langfuse.internal.as_root", "true")
	wantAttr(t, root, "langfuse.trace.name", "digest")
	wantAttr(t, root, "langfuse.observation.input", `["a","b","c"]`)
	wantAttr(t, root, "langfuse.observation.output", `["a","c"]`)
	wantAttr(t, root, "langfuse.trace.metadata.artifact_id", "20261030")
	wantAttr(t, root, "langfuse.trace.metadata.candidate_count", "3")
	wantAttr(t, root, "langfuse.trace.metadata.floor_pass_count", "2")
	wantAttr(t, root, "langfuse.trace.metadata.selected_count", "2")
	wantAttr(t, root, "langfuse.trace.metadata.window_since", "2026-10-29T06:00:00Z")
	wantAttr(t, root, "langfuse.trace.metadata.window_until", "2026-10-30T06:00:00Z")
	if _, ok := attr(root, "langfuse.trace.metadata.empty_reason"); ok {
		t.Fatal("a non-empty Digest must not record an empty reason")
	}
	if root.Status.Code == codes.Error {
		t.Fatalf("computed Digest ended at error: %v", root.Status)
	}
}

func TestReusedDigestProducesNoTrace(t *testing.T) {
	run := newTracedDigest(t, &selectStub{answer: `{"article_ids":["a"]}`}, traceArticles())
	if _, created, err := run.service.ForDate(context.Background(), traceDay); err != nil || !created {
		t.Fatalf("first ForDate() created=%v err=%v", created, err)
	}
	run.spans.Reset()

	if _, created, err := run.service.ForDate(context.Background(), traceDay); err != nil || created {
		t.Fatalf("repeat ForDate() created=%v err=%v", created, err)
	}
	if spans := run.spans.GetSpans(); len(spans) != 0 {
		t.Fatalf("reused Digest produced spans %v", spanNames(spans))
	}
	if run.stub.calls != 1 {
		t.Fatalf("model calls = %d, want 1", run.stub.calls)
	}
}

func TestFailedSelectIsAnErrorGenerationAndFailsTheTrace(t *testing.T) {
	run := newTracedDigest(t, &selectStub{fail: true}, traceArticles())
	_, created, err := run.service.ForDate(context.Background(), traceDay)
	if err == nil || created {
		t.Fatalf("ForDate() created=%v err=%v, want the select error", created, err)
	}

	spans := run.spans.GetSpans()
	gen := spanNamed(t, spans, ai.OpDigestSelect)
	if gen.Status.Code != codes.Error {
		t.Fatalf("select status = %v, want error", gen.Status)
	}
	wantAttr(t, gen, "langfuse.observation.level", "ERROR")

	root := spanNamed(t, spans, "digest")
	if root.Status.Code != codes.Error || root.Status.Description != err.Error() {
		t.Fatalf("digest status = %v, want error %q", root.Status, err)
	}
	wantAttr(t, root, "langfuse.observation.level", "ERROR")
	wantAttr(t, root, "langfuse.observation.input", `["a","b","c"]`)
	wantAttr(t, root, "langfuse.trace.metadata.candidate_count", "3")
	if _, ok := attr(root, "langfuse.observation.output"); ok {
		t.Fatal("a failed Digest must record no chosen IDs")
	}
}

func TestEmptyDigestTraceRecordsItsEmptyReason(t *testing.T) {
	cases := []struct {
		name        string
		articles    []digestartifact.Article
		generations int
	}{
		{"no candidates", nil, 0},
		{"model chose none", traceArticles(), 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newTracedDigest(t, &selectStub{answer: `{"article_ids":[]}`}, tc.articles)
			artifact, _, err := run.service.ForDate(context.Background(), traceDay)
			if err != nil || artifact.EmptyReason == "" {
				t.Fatalf("ForDate() = %+v, %v; want an empty Digest", artifact, err)
			}

			spans := run.spans.GetSpans()
			if len(spans) != 1+tc.generations {
				t.Fatalf("spans = %v, want digest and %d generation(s)", spanNames(spans), tc.generations)
			}
			root := spanNamed(t, spans, "digest")
			if root.Status.Code == codes.Error {
				t.Fatalf("empty Digest ended at error: %v", root.Status)
			}
			wantAttr(t, root, "langfuse.trace.metadata.empty_reason", artifact.EmptyReason)
			wantAttr(t, root, "langfuse.trace.metadata.selected_count", "0")
			wantAttr(t, root, "langfuse.observation.output", `[]`)
		})
	}
}
