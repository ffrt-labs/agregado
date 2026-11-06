package articleindex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/felipeafreitas/agregado/internal/ai"
	"github.com/felipeafreitas/agregado/internal/telemetry"
)

const traceModel = "@cf/google/gemma-4-26b-a4b-it"

// workersAIStub answers like Workers AI's native /ai/run endpoint, picking the
// answer from the messages so one stub serves all three enrichment calls.
// failCategorize makes the categorize call return a Workers AI error.
type workersAIStub struct {
	mu             sync.Mutex
	calls          int
	failCategorize bool
	category       string // categorize's answer; "" means "tech"
}

func (s *workersAIStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
	if !strings.HasSuffix(r.URL.Path, "/accounts/acct/ai/run/"+traceModel) {
		http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		return
	}
	var body struct {
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	system, user := body.Messages[0].Content, body.Messages[1].Content
	var answer string
	switch {
	case strings.Contains(system, "Allowed slugs"):
		if s.failCategorize {
			fmt.Fprint(w, `{"success":false,"errors":[{"code":3040,"message":"capacity exceeded"}]}`)
			return
		}
		answer = s.category
		if answer == "" {
			answer = "tech"
		}
	case strings.HasPrefix(user, "PREFERENCES.md:"):
		answer = "4"
	default:
		answer = `{"kind":"news","claims":["c1","c2","c3","c4"],"entities":["a","b","c","d","e","f","g"]}`
	}
	out, _ := json.Marshal(answer)
	fmt.Fprintf(w, `{"success":true,"result":{"choices":[{"message":{"content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":120,"completion_tokens":7}}}`, out)
}

type tracedRun struct {
	service *Service
	store   *fakeStore
	stub    *workersAIStub
	spans   *tracetest.InMemoryExporter
	scores  *fakeScoreSink
}

func newTracedRun(t *testing.T, stub *workersAIStub, fetcher Fetcher, opts ...ai.CloudflareOption) tracedRun {
	t.Helper()
	server := httptest.NewServer(stub)
	t.Cleanup(server.Close)
	spans := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))
	opts = append([]ai.CloudflareOption{ai.WithBaseURL(server.URL), ai.WithTracerProvider(tp)}, opts...)
	provider := ai.NewCloudflareProvider("acct", "token", traceModel, 0, 0, nil, nil, nil, opts...)
	store := &fakeStore{records: map[string]Record{}}
	scores := &fakeScoreSink{}
	service := NewService(store, fetcher, NewCloudflareModel(provider), fakePreferences{text: "likes Go"},
		WithTracerProvider(tp), WithScoreSink(scores))
	return tracedRun{service: service, store: store, stub: stub, spans: spans, scores: scores}
}

func tracedRequest() Request {
	return Request{MinifluxEntryID: 7, SourceID: "src-42", CanonicalURL: "https://example.com/a", Title: "A title"}
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

func TestEnrichmentProducesOneTraceKeyedByRecordID(t *testing.T) {
	run := newTracedRun(t, &workersAIStub{}, &fakeFetcher{text: "fetched body"})
	record, created, err := run.service.Process(context.Background(), tracedRequest())
	if err != nil || !created || record.Score != 4 || record.Tags[0] != "tech" {
		t.Fatalf("Process() = %#v, %v, %v", record, created, err)
	}

	spans := run.spans.GetSpans()
	if len(spans) != 5 {
		t.Fatalf("spans = %v, want article-enrichment, fetch and three generations", spanNames(spans))
	}
	root := spanNamed(t, spans, "article-enrichment")
	wantTrace := telemetry.TraceID(telemetry.ArticleIndexKey("index-1"))
	for _, s := range spans {
		if [16]byte(s.SpanContext.TraceID()) != wantTrace {
			t.Fatalf("span %q trace = %s, want %x", s.Name, s.SpanContext.TraceID(), wantTrace)
		}
		if s.Name != root.Name && s.Parent.SpanID() != root.SpanContext.SpanID() {
			t.Fatalf("span %q is not a child of article-enrichment", s.Name)
		}
	}
	wantAttr(t, root, "langfuse.internal.as_root", "true")
	wantAttr(t, root, "langfuse.trace.name", "article-enrichment")
	wantAttr(t, root, "langfuse.trace.tags", `["src-42"]`)
	wantAttr(t, root, "langfuse.trace.metadata.record_id", "index-1")
	if root.Status.Code == codes.Error {
		t.Fatalf("successful enrichment ended at error: %v", root.Status)
	}

	fetch := spanNamed(t, spans, "fetch")
	wantAttr(t, fetch, "langfuse.observation.metadata.content_source", "fetch")
	wantAttr(t, fetch, "langfuse.observation.metadata.partial", "false")

	for _, op := range []string{ai.OpArticleSummary, ai.OpCategorize, ai.OpPreferenceScore} {
		gen := spanNamed(t, spans, op)
		wantAttr(t, gen, "langfuse.observation.type", "generation")
		wantAttr(t, gen, "langfuse.observation.metadata.operation", op)
		wantAttr(t, gen, "gen_ai.request.model", traceModel)
		wantAttr(t, gen, "gen_ai.usage.input_tokens", "120")
		wantAttr(t, gen, "gen_ai.usage.output_tokens", "7")
		// Every enrichment call is a template operation and records its
		// variables (#201, #203).
		input, _ := attr(gen, "langfuse.observation.input")
		var vars map[string]string
		if err := json.Unmarshal([]byte(input.AsString()), &vars); err != nil ||
			vars["title"] != "A title" || vars["content"] != "fetched body" {
			t.Fatalf("%s input = %q, want the prompt variables", op, input.AsString())
		}
	}
	wantAttr(t, spanNamed(t, spans, ai.OpCategorize), "langfuse.observation.output", "tech")
	wantAttr(t, spanNamed(t, spans, ai.OpPreferenceScore), "langfuse.observation.output", "4")
}

func TestFetchSpanRecordsContentSource(t *testing.T) {
	cases := []struct {
		name            string
		fetcher         Fetcher
		bridge, feed    string
		source, partial string
	}{
		{"bridge", &fakeFetcher{}, "bridge body", "", "bridge", "false"},
		{"fetch", &fakeFetcher{text: "fetched body"}, "", "", "fetch", "false"},
		{"feed", errFetcher{}, "", "feed body", "feed", "true"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run := newTracedRun(t, &workersAIStub{}, tc.fetcher)
			req := tracedRequest()
			req.BridgeContent, req.FeedContent = tc.bridge, tc.feed
			if _, _, err := run.service.Process(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			fetch := spanNamed(t, run.spans.GetSpans(), "fetch")
			wantAttr(t, fetch, "langfuse.observation.metadata.content_source", tc.source)
			wantAttr(t, fetch, "langfuse.observation.metadata.partial", tc.partial)
		})
	}
}

func TestFailedModelCallIsAnErrorGenerationAndFailsTheTrace(t *testing.T) {
	run := newTracedRun(t, &workersAIStub{failCategorize: true}, &fakeFetcher{text: "fetched body"})
	record, created, err := run.service.Process(context.Background(), tracedRequest())
	if err == nil || !created || record.Status != Failed || !strings.Contains(record.FailureReason, "capacity exceeded") {
		t.Fatalf("Process() = %#v, %v, %v; want today's failed record", record, created, err)
	}

	spans := run.spans.GetSpans()
	gen := spanNamed(t, spans, ai.OpCategorize)
	if gen.Status.Code != codes.Error || !strings.Contains(gen.Status.Description, "capacity exceeded") {
		t.Fatalf("categorize status = %v, want error with the Workers AI message", gen.Status)
	}
	wantAttr(t, gen, "langfuse.observation.level", "ERROR")
	wantAttr(t, gen, "langfuse.observation.status_message", gen.Status.Description)

	root := spanNamed(t, spans, "article-enrichment")
	if root.Status.Code != codes.Error || root.Status.Description != record.FailureReason {
		t.Fatalf("root status = %v, want error %q", root.Status, record.FailureReason)
	}
	wantAttr(t, root, "langfuse.observation.level", "ERROR")
}

func TestDuplicateEnrichmentProducesNoTrace(t *testing.T) {
	run := newTracedRun(t, &workersAIStub{}, &fakeFetcher{text: "fetched body"})
	if _, _, err := run.service.Process(context.Background(), tracedRequest()); err != nil {
		t.Fatal(err)
	}
	run.spans.Reset()
	calls := run.stub.calls

	if _, created, err := run.service.Process(context.Background(), tracedRequest()); err != nil || created {
		t.Fatalf("duplicate Process() created=%v err=%v", created, err)
	}
	if spans := run.spans.GetSpans(); len(spans) != 0 {
		t.Fatalf("duplicate produced spans %v", spanNames(spans))
	}
	if run.stub.calls != calls {
		t.Fatalf("duplicate made %d model calls", run.stub.calls-calls)
	}
}

func TestAllowedCategoryScoresSlugAllowed(t *testing.T) {
	run := newTracedRun(t, &workersAIStub{}, &fakeFetcher{text: "fetched body"})
	if _, _, err := run.service.Process(context.Background(), tracedRequest()); err != nil {
		t.Fatal(err)
	}
	wantScores(t, run.scores, map[string]float64{"summary_valid": 1, "summary_fallback": 0, "slug_allowed": 1})
}

func TestInventedCategoryScoresSlugDisallowedAndIsStoredAsToday(t *testing.T) {
	run := newTracedRun(t, &workersAIStub{category: "astrology"}, &fakeFetcher{text: "fetched body"})
	record, _, err := run.service.Process(context.Background(), tracedRequest())
	if err != nil || record.Status != Complete || len(record.Tags) != 1 || record.Tags[0] != "astrology" {
		t.Fatalf("Process() = %#v, %v; want the invented tag stored as today", record, err)
	}
	wantScores(t, run.scores, map[string]float64{"slug_allowed": 0})
}

func TestFailedCategorizeEmitsNoSlugScore(t *testing.T) {
	run := newTracedRun(t, &workersAIStub{failCategorize: true}, &fakeFetcher{text: "fetched body"})
	run.service.Process(context.Background(), tracedRequest())
	for _, e := range run.scores.events {
		if e.Name == "slug_allowed" {
			t.Fatalf("no answer, nothing to check: %+v", e)
		}
	}
}

// seededTemplates serves each operation's in-code default as Langfuse v1,
// which is what the seeding command creates.
type seededTemplates struct{}

func (seededTemplates) Template(ctx context.Context, name string) (ai.Prompt, error) {
	op, _ := strings.CutPrefix(name, "agregado/")
	p, ok := ai.DefaultTemplate(ctx, nil, op)
	if !ok {
		return ai.Prompt{}, fmt.Errorf("no prompt %s", name)
	}
	p.Version = 1
	return p, nil
}

func TestSeededPromptsLinkEveryEnrichmentGenerationToVersionOne(t *testing.T) {
	run := newTracedRun(t, &workersAIStub{}, &fakeFetcher{text: "fetched body"}, ai.WithTemplates(seededTemplates{}))
	record, _, err := run.service.Process(context.Background(), tracedRequest())
	if err != nil || record.Score != 4 || record.Tags[0] != "tech" {
		t.Fatalf("Process() = %#v, %v; want the same enrichment as without Langfuse", record, err)
	}
	spans := run.spans.GetSpans()
	for _, op := range []string{ai.OpArticleSummary, ai.OpCategorize, ai.OpPreferenceScore} {
		gen := spanNamed(t, spans, op)
		wantAttr(t, gen, "langfuse.observation.prompt.name", "agregado/"+op)
		wantAttr(t, gen, "langfuse.observation.prompt.version", "1")
	}
	input, _ := attr(spanNamed(t, spans, ai.OpCategorize), "langfuse.observation.input")
	var vars map[string]string
	if err := json.Unmarshal([]byte(input.AsString()), &vars); err != nil || vars["allowed_slugs"] == "" {
		t.Fatalf("categorize input = %q, want allowed_slugs among the variables", input.AsString())
	}
}
