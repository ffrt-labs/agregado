package langfuse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/felipeafreitas/agregado/internal/ai"
)

const scoreName = "agregado/preference_score"

// fakeLangfuse serves the parts of Langfuse's public prompts API the client
// uses, for one project with the pk-test/sk-test key pair.
type fakeLangfuse struct {
	mu       sync.Mutex
	versions []json.RawMessage // index i is version i+1; production is the last
	fail     bool              // every request answers 500
	hold     chan struct{}     // when set, prompt fetches wait for it to close
	fetches  int
	creates  []map[string]any
	lastAuth string
	lastPath string
	lastQS   string
}

func chatPrompt(version int, system, user string) json.RawMessage {
	b, _ := json.Marshal(map[string]any{
		"type": "chat", "name": scoreName, "version": version,
		"prompt": []map[string]string{
			{"type": "chatmessage", "role": "system", "content": system},
			{"type": "chatmessage", "role": "user", "content": user},
		},
		"labels": []string{"production"}, "config": map[string]any{},
	})
	return b
}

func (f *fakeLangfuse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.lastAuth = r.Header.Get("Authorization")
	f.lastPath = r.URL.EscapedPath()
	f.lastQS = r.URL.RawQuery
	hold, fail := f.hold, f.fail
	f.mu.Unlock()

	if r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("pk-test:sk-test")) {
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if fail {
		http.Error(w, `{"message":"boom"}`, http.StatusInternalServerError)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.EscapedPath() == "/api/public/v2/prompts/agregado%2Fpreference_score":
		if hold != nil {
			<-hold
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.fetches++
		if len(f.versions) == 0 || r.URL.Query().Get("label") != "production" {
			http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			return
		}
		w.Write(f.versions[len(f.versions)-1])
	case r.Method == http.MethodGet && r.URL.Path == "/api/public/v2/prompts":
		f.mu.Lock()
		defer f.mu.Unlock()
		var data []map[string]any
		if r.URL.Query().Get("name") == scoreName && len(f.versions) > 0 {
			data = append(data, map[string]any{"name": scoreName, "versions": []int{1}})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data, "meta": map[string]int{"page": 1}})
	case r.Method == http.MethodPost && r.URL.Path == "/api/public/v2/prompts":
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.creates = append(f.creates, body)
		f.versions = append(f.versions, chatPrompt(len(f.versions)+1, "", ""))
		w.Write(f.versions[len(f.versions)-1])
	default:
		http.Error(w, "unexpected "+r.Method+" "+r.URL.String(), http.StatusNotFound)
	}
}

func (f *fakeLangfuse) set(fn func(f *fakeLangfuse)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeLangfuse) fetchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fetches
}

// clock is a settable time source for the cache TTL.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newPrompts(t *testing.T, fake *fakeLangfuse) (*Prompts, *clock) {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)
	c := &clock{now: time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC)}
	cfg := Config{BaseURL: server.URL, PublicKey: "pk-test", SecretKey: "sk-test"}
	return NewPrompts(cfg, time.Minute, WithClock(c.Now)), c
}

func mustTemplate(t *testing.T, p *Prompts) ai.Prompt {
	t.Helper()
	got, err := p.Template(context.Background(), scoreName)
	if err != nil {
		t.Fatalf("Template() error = %v", err)
	}
	return got
}

// eventually polls until cond holds, for what a background refresh changes.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTemplateFetchesTheProductionLabelWithBasicAuth(t *testing.T) {
	fake := &fakeLangfuse{versions: []json.RawMessage{chatPrompt(1, "sys {{title}}", "user {{content}}")}}
	p, _ := newPrompts(t, fake)

	got := mustTemplate(t, p)
	want := ai.Prompt{Name: scoreName, Version: 1, System: "sys {{title}}", User: "user {{content}}"}
	if got != want {
		t.Fatalf("Template() = %+v, want %+v", got, want)
	}
	if fake.lastPath != "/api/public/v2/prompts/agregado%2Fpreference_score" || fake.lastQS != "label=production" {
		t.Fatalf("fetched %s?%s, want the URL-encoded name at label production", fake.lastPath, fake.lastQS)
	}
}

func TestTemplateIsServedFromCacheWithinTheTTL(t *testing.T) {
	fake := &fakeLangfuse{versions: []json.RawMessage{chatPrompt(1, "s", "u")}}
	p, clk := newPrompts(t, fake)

	mustTemplate(t, p)
	fake.set(func(f *fakeLangfuse) { f.versions = append(f.versions, chatPrompt(2, "s2", "u2")) })
	clk.advance(59 * time.Second)

	if got := mustTemplate(t, p); got.Version != 1 {
		t.Fatalf("Template() within the TTL = v%d, want the cached v1", got.Version)
	}
	if n := fake.fetchCount(); n != 1 {
		t.Fatalf("fetches = %d, want 1", n)
	}
}

func TestTemplateRefreshesInTheBackgroundAfterTheTTL(t *testing.T) {
	fake := &fakeLangfuse{versions: []json.RawMessage{chatPrompt(1, "s", "u")}}
	p, clk := newPrompts(t, fake)
	mustTemplate(t, p)

	// Promote v2, and hold the refresh so the caller can't be waiting on it.
	hold := make(chan struct{})
	fake.set(func(f *fakeLangfuse) {
		f.versions = append(f.versions, chatPrompt(2, "s2", "u2"))
		f.hold = hold
	})
	clk.advance(61 * time.Second)

	if got := mustTemplate(t, p); got.Version != 1 {
		t.Fatalf("Template() after the TTL = v%d, want the stale v1 while refreshing", got.Version)
	}
	close(hold)
	eventually(t, "the refreshed v2", func() bool { return mustTemplate(t, p).Version == 2 })
	if n := fake.fetchCount(); n != 2 {
		t.Fatalf("fetches = %d, want one refresh", n)
	}
}

func TestTemplateKeepsTheLastGoodCopyWhenLangfuseFails(t *testing.T) {
	fake := &fakeLangfuse{versions: []json.RawMessage{chatPrompt(1, "s", "u")}}
	p, clk := newPrompts(t, fake)
	mustTemplate(t, p)

	fake.set(func(f *fakeLangfuse) { f.fail = true })
	clk.advance(61 * time.Second)
	mustTemplate(t, p) // starts the failing refresh
	eventually(t, "the refresh to finish", func() bool {
		p.mu.Lock()
		defer p.mu.Unlock()
		return !p.cache[scoreName].refreshing
	})

	if got := mustTemplate(t, p); got.Version != 1 {
		t.Fatalf("Template() with Langfuse failing = v%d, want the last good v1", got.Version)
	}
}

func TestTemplateErrorsFromAColdStartWithLangfuseDown(t *testing.T) {
	fake := &fakeLangfuse{versions: []json.RawMessage{chatPrompt(1, "s", "u")}, fail: true}
	p, clk := newPrompts(t, fake)

	if _, err := p.Template(context.Background(), scoreName); err == nil {
		t.Fatal("Template() with nothing ever fetched = nil error, want one so the caller uses its default")
	}
	// Within the TTL a cold failure isn't retried on every call.
	fake.set(func(f *fakeLangfuse) { f.fail = false })
	if _, err := p.Template(context.Background(), scoreName); err == nil {
		t.Fatal("Template() retried within the TTL of a failed fetch")
	}
	clk.advance(61 * time.Second)
	if got := mustTemplate(t, p); got.Version != 1 {
		t.Fatalf("Template() once Langfuse is back = v%d, want v1", got.Version)
	}
}

func TestTemplateRejectsPromptsThatAreNotASystemAndUserMessage(t *testing.T) {
	tests := map[string]string{
		"text prompt":        `{"type":"text","name":"agregado/preference_score","version":1,"prompt":"hi"}`,
		"placeholder":        `{"type":"chat","name":"agregado/preference_score","version":1,"prompt":[{"role":"system","content":"s"},{"type":"placeholder","name":"history"}]}`,
		"user before system": `{"type":"chat","name":"agregado/preference_score","version":1,"prompt":[{"role":"user","content":"u"},{"role":"system","content":"s"}]}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			p, _ := newPrompts(t, &fakeLangfuse{versions: []json.RawMessage{json.RawMessage(body)}})
			if got, err := p.Template(context.Background(), scoreName); err == nil {
				t.Fatalf("Template() = %+v, want an error", got)
			}
		})
	}
}

func TestTemplateErrorsWhenLangfuseIsUnconfigured(t *testing.T) {
	p := NewPrompts(Config{}, time.Minute)
	if _, err := p.Template(context.Background(), scoreName); err == nil {
		t.Fatal("Template() with Langfuse unconfigured = nil error")
	}
}

func TestSeedCreatesVersionOneAtProduction(t *testing.T) {
	fake := &fakeLangfuse{}
	p, _ := newPrompts(t, fake)

	created, err := p.Seed(context.Background(), ai.Prompt{Name: scoreName, System: "sys", User: "{{title}}"})
	if err != nil || !created {
		t.Fatalf("Seed() = %v, %v, want created", created, err)
	}
	if len(fake.creates) != 1 {
		t.Fatalf("creates = %d, want 1", len(fake.creates))
	}
	got, _ := json.Marshal(fake.creates[0])
	want := `{"labels":["production"],"name":"agregado/preference_score","prompt":[{"content":"sys","role":"system"},{"content":"{{title}}","role":"user"}],"type":"chat"}`
	if string(got) != want {
		t.Fatalf("create body = %s, want %s", got, want)
	}
}

func TestSeedNeverOverwritesAnExistingPrompt(t *testing.T) {
	fake := &fakeLangfuse{versions: []json.RawMessage{chatPrompt(1, "edited", "in langfuse")}}
	p, _ := newPrompts(t, fake)

	for i := range 2 {
		created, err := p.Seed(context.Background(), ai.Prompt{Name: scoreName, System: "sys", User: "u"})
		if err != nil || created {
			t.Fatalf("Seed() #%d = %v, %v, want skipped", i+1, created, err)
		}
	}
	if len(fake.creates) != 0 {
		t.Fatalf("creates = %d, want none", len(fake.creates))
	}
}

func TestSeedReportsLangfuseErrors(t *testing.T) {
	p, _ := newPrompts(t, &fakeLangfuse{fail: true})
	if _, err := p.Seed(context.Background(), ai.Prompt{Name: scoreName}); err == nil {
		t.Fatal("Seed() with Langfuse failing = nil error")
	}
}
