package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// scoreStub answers like Workers AI's native /ai/run endpoint and keeps the
// messages of the last call.
type scoreStub struct {
	mu           sync.Mutex
	system, user string
	answer       string // "" answers "4"
}

func (s *scoreStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Messages []Messages `json:"messages"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	s.mu.Lock()
	s.system, s.user = body.Messages[0].Content, body.Messages[1].Content
	answer := s.answer
	s.mu.Unlock()
	if answer == "" {
		answer = "4"
	}
	out, _ := json.Marshal(answer)
	fmt.Fprintf(w, `{"success":true,"result":{"choices":[{"message":{"content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":1}}}`, out)
}

func (s *scoreStub) messages() (string, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.system, s.user
}

type fakeTemplates struct {
	prompt Prompt
	err    error
	asked  []string
}

func (f *fakeTemplates) Template(_ context.Context, name string) (Prompt, error) {
	f.asked = append(f.asked, name)
	return f.prompt, f.err
}

type scoreRun struct {
	provider *CloudflareProvider
	stub     *scoreStub
	spans    *tracetest.InMemoryExporter
}

func newRun(t *testing.T, tags TagLister, opts ...CloudflareOption) scoreRun {
	t.Helper()
	stub := &scoreStub{}
	server := httptest.NewServer(stub)
	t.Cleanup(server.Close)
	spans := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(spans))
	opts = append([]CloudflareOption{WithBaseURL(server.URL), WithTracerProvider(tp)}, opts...)
	return scoreRun{
		provider: NewCloudflareProvider("acct", "token", "@cf/model", 0, 0, tags, opts...),
		stub:     stub,
		spans:    spans,
	}
}

func (r scoreRun) score(t *testing.T) {
	t.Helper()
	got, err := r.provider.ScoreWithPreferences(context.Background(), "Go 1.26", "Released today.", "likes Go")
	if err != nil {
		t.Fatalf("ScoreWithPreferences() error = %v", err)
	}
	if got != 4 {
		t.Fatalf("ScoreWithPreferences() = %d, want 4", got)
	}
}

func (r scoreRun) generation(t *testing.T) map[string]string {
	t.Helper()
	return r.generationOf(t, OpPreferenceScore)
}

func (r scoreRun) generationOf(t *testing.T, op string) map[string]string {
	t.Helper()
	spans := r.spans.GetSpans()
	if len(spans) != 1 || spans[0].Name != op {
		t.Fatalf("spans = %v, want one %s generation", spans, op)
	}
	attrs := map[string]string{}
	for _, kv := range spans[0].Attributes {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	return attrs
}

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// recordHandler keeps slog records so a test can read their attributes
// instead of matching rendered text.
type recordHandler struct {
	mu      *sync.Mutex
	records *[]slog.Record
}

func (h recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h recordHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, r)
	return nil
}
func (h recordHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h recordHandler) WithGroup(string) slog.Handler      { return h }

func TestEveryCallStillEmitsTheUsageLogLine(t *testing.T) {
	var records []slog.Record
	prev := slog.Default()
	slog.SetDefault(slog.New(recordHandler{mu: &sync.Mutex{}, records: &records}))
	t.Cleanup(func() { slog.SetDefault(prev) })

	newRun(t, nil).score(t)

	for _, r := range records {
		if r.Message != "ai: usage" {
			continue
		}
		attrs := map[string]any{}
		r.Attrs(func(a slog.Attr) bool { attrs[a.Key] = a.Value.Any(); return true })
		if attrs["operation"] != OpPreferenceScore || attrs["prompt_tokens"] != int64(100) || attrs["completion_tokens"] != int64(1) {
			t.Fatalf("ai: usage attrs = %v, want preference_score with 100 prompt and 1 completion tokens", attrs)
		}
		return
	}
	t.Fatalf("no ai: usage line among %d log records", len(records))
}

const wantDefaultUser = "PREFERENCES.md:\nlikes Go\n\nTitle: Go 1.26\n\nContent: Released today."

func TestScoreWithoutLangfuseSendsTheSameMessagesAsBefore(t *testing.T) {
	run := newRun(t, nil)
	run.score(t)

	system, user := run.stub.messages()
	if system != DefaultPrompts[OpPreferenceScore] || user != wantDefaultUser {
		t.Fatalf("messages = %q, %q, want the in-code default and %q", system, user, wantDefaultUser)
	}
	attrs := run.generation(t)
	if _, ok := attrs["langfuse.observation.prompt.name"]; ok {
		t.Fatalf("an in-code default is linked to a Langfuse prompt: %v", attrs)
	}
	if got, want := attrs["langfuse.observation.input"], `{"content":"Released today.","preferences":"likes Go","title":"Go 1.26"}`; got != want {
		t.Fatalf("generation input = %s, want the variables %s", got, want)
	}
}

func TestScoreUsesTheLangfusePromptAndLinksTheGeneration(t *testing.T) {
	templates := &fakeTemplates{prompt: Prompt{
		Name: "agregado/preference_score", Version: 2,
		System: "v2 system",
		User:   "Profile: {{preferences}} | {{title}} | {{content}}",
	}}
	run := newRun(t, nil, WithTemplates(templates))
	run.score(t)

	if len(templates.asked) != 1 || templates.asked[0] != "agregado/preference_score" {
		t.Fatalf("templates asked for %v, want agregado/preference_score", templates.asked)
	}
	system, user := run.stub.messages()
	if system != "v2 system" || user != "Profile: likes Go | Go 1.26 | Released today." {
		t.Fatalf("messages = %q, %q, want the compiled v2 prompt", system, user)
	}
	attrs := run.generation(t)
	if attrs["langfuse.observation.prompt.name"] != "agregado/preference_score" || attrs["langfuse.observation.prompt.version"] != "2" {
		t.Fatalf("generation prompt = %q v%q, want agregado/preference_score v2",
			attrs["langfuse.observation.prompt.name"], attrs["langfuse.observation.prompt.version"])
	}
}

func TestScoreFallsBackToTheDefaultWhenTheLangfusePromptIsInvalid(t *testing.T) {
	warnings := captureWarnings(t)
	templates := &fakeTemplates{prompt: Prompt{
		Name: "agregado/preference_score", Version: 3,
		System: "v3", User: "{{preferences}} {{title}} {{content}} {{author}}",
	}}
	run := newRun(t, nil, WithTemplates(templates))
	run.score(t)

	system, user := run.stub.messages()
	if system != DefaultPrompts[OpPreferenceScore] || user != wantDefaultUser {
		t.Fatalf("messages = %q, %q, want the in-code default", system, user)
	}
	if _, ok := run.generation(t)["langfuse.observation.prompt.version"]; ok {
		t.Fatal("generation is linked to the invalid Langfuse version")
	}
	if log := warnings.String(); !strings.Contains(log, "level=WARN") || !strings.Contains(log, "author") {
		t.Fatalf("no warning naming the undefined variable, log = %q", log)
	}
}

func TestScoreUsesTheDefaultWhenLangfuseHasNoPrompt(t *testing.T) {
	run := newRun(t, nil, WithTemplates(&fakeTemplates{err: errors.New("langfuse unreachable")}))
	run.score(t)

	system, user := run.stub.messages()
	if system != DefaultPrompts[OpPreferenceScore] || user != wantDefaultUser {
		t.Fatalf("messages = %q, %q, want the in-code default", system, user)
	}
}
