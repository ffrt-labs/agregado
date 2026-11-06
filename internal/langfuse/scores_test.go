package langfuse

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/felipeafreitas/agregado/internal/telemetry"
)

// fakeScoresAPI stands in for Langfuse's POST /api/public/scores.
type fakeScoresAPI struct {
	mu       sync.Mutex
	requests []scoreRequest
	arrived  chan struct{} // signalled once per request, before any blocking
	block    chan struct{} // non-nil: hang until closed, like a slow Langfuse
	status   int
}

type scoreRequest struct {
	method, path, auth, contentType string
	body                            map[string]any
}

func newFakeScoresAPI() *fakeScoresAPI {
	return &fakeScoresAPI{arrived: make(chan struct{}, 100), status: http.StatusOK}
}

func (f *fakeScoresAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	f.mu.Lock()
	f.requests = append(f.requests, scoreRequest{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type"), body})
	f.mu.Unlock()
	f.arrived <- struct{}{}
	if f.block != nil {
		select {
		case <-f.block:
		case <-r.Context().Done():
		}
	}
	w.WriteHeader(f.status)
	w.Write([]byte(`{"id":"x"}`))
}

func (f *fakeScoresAPI) received() []scoreRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]scoreRequest(nil), f.requests...)
}

func waitArrived(t *testing.T, f *fakeScoresAPI) {
	t.Helper()
	select {
	case <-f.arrived:
	case <-time.After(2 * time.Second):
		t.Fatal("no request reached the fake Langfuse")
	}
}

var firstVote = time.Date(2026, 10, 30, 7, 15, 0, 0, time.UTC)

func TestScoresAreOffUnlessFullyConfigured(t *testing.T) {
	fake := newFakeScoresAPI()
	server := httptest.NewServer(fake)
	defer server.Close()

	for _, cfg := range []Config{{}, {BaseURL: server.URL, PublicKey: "pk"}, {PublicKey: "pk", SecretKey: "sk"}} {
		scores := NewScores(cfg)
		if scores.Enabled() {
			t.Fatalf("%+v: scores enabled", cfg)
		}
		if err := scores.Emit(context.Background(), telemetry.CategoricalScore("article-index:index-1", "user_vote", "up", firstVote)); err != nil {
			t.Fatalf("Emit on disabled sink: %v", err)
		}
		if err := scores.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(fake.received()); got != 0 {
		t.Fatalf("%d score requests with Langfuse unconfigured", got)
	}
}

// A changed vote is sent with the same score ID and timestamp, which is what
// makes Langfuse overwrite the earlier user_vote rather than add a second.
func TestUserVoteIsPostedWithAuthAndDeterministicID(t *testing.T) {
	fake := newFakeScoresAPI()
	server := httptest.NewServer(fake)
	defer server.Close()

	scores := NewScores(Config{BaseURL: server.URL + "/", PublicKey: "pk-lf-1", SecretKey: "sk-lf-1"})
	if !scores.Enabled() {
		t.Fatal("scores disabled with Langfuse configured")
	}
	for _, vote := range []string{"up", "down"} {
		if err := scores.Emit(context.Background(), telemetry.CategoricalScore("article-index:index-1", "user_vote", vote, firstVote)); err != nil {
			t.Fatal(err)
		}
	}
	if err := scores.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	requests := fake.received()
	if len(requests) != 2 {
		t.Fatalf("got %d score requests, want 2", len(requests))
	}
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("pk-lf-1:sk-lf-1"))
	for i, vote := range []string{"up", "down"} {
		r := requests[i]
		if r.method != http.MethodPost || r.path != "/api/public/scores" {
			t.Fatalf("request %s %s, want POST /api/public/scores", r.method, r.path)
		}
		if r.auth != wantAuth {
			t.Fatalf("Authorization = %q, want %q", r.auth, wantAuth)
		}
		if r.contentType != "application/json" {
			t.Fatalf("Content-Type = %q", r.contentType)
		}
		want := map[string]any{
			// printf %s 'article-index:index-1/user_vote' | sha256sum | cut -c1-32
			"id": "338d8628cd038b7d3459e9efa6218df3",
			// telemetry.TraceIDHex(ArticleIndexKey("index-1")), see traceid_test.go
			"traceId":   "bd81d27a19295169b9a7b3c12b25ac55",
			"name":      "user_vote",
			"value":     vote,
			"dataType":  "CATEGORICAL",
			"timestamp": "2026-10-30T07:15:00.000Z",
		}
		if len(r.body) != len(want) {
			t.Fatalf("body %d = %v, want %v", i, r.body, want)
		}
		for k, v := range want {
			if r.body[k] != v {
				t.Errorf("body %d %s = %v, want %v", i, k, r.body[k], v)
			}
		}
	}
}

func TestBooleanScoreValueIsNumericWithComment(t *testing.T) {
	fake := newFakeScoresAPI()
	server := httptest.NewServer(fake)
	defer server.Close()

	scores := NewScores(Config{BaseURL: server.URL, PublicKey: "pk", SecretKey: "sk"})
	event := telemetry.BooleanScore("article-index:index-1", "summary_valid", true, firstVote)
	event.Comment = "fallback not used"
	if err := scores.Emit(context.Background(), event); err != nil {
		t.Fatal(err)
	}
	if err := scores.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}

	requests := fake.received()
	if len(requests) != 1 {
		t.Fatalf("got %d requests, want 1", len(requests))
	}
	body := requests[0].body
	if body["value"] != 1.0 || body["dataType"] != "BOOLEAN" || body["comment"] != "fallback not used" {
		t.Fatalf("body = %v", body)
	}
	if body["id"] != "28621d448aa3f2ce849b3d674d3b382d" {
		t.Fatalf("id = %v", body["id"])
	}
}

func TestUntimedScoreOmitsTimestamp(t *testing.T) {
	fake := newFakeScoresAPI()
	server := httptest.NewServer(fake)
	defer server.Close()

	scores := NewScores(Config{BaseURL: server.URL, PublicKey: "pk", SecretKey: "sk"})
	if err := scores.Emit(context.Background(), telemetry.NumericScore("digest:20261030", "selected", 3, time.Time{})); err != nil {
		t.Fatal(err)
	}
	_ = scores.Shutdown(context.Background())
	body := fake.received()[0].body
	if _, ok := body["timestamp"]; ok {
		t.Fatalf("timestamp sent for an untimed score: %v", body)
	}
	if body["value"] != 3.0 || body["dataType"] != "NUMERIC" {
		t.Fatalf("body = %v", body)
	}
}

// With Langfuse hanging, Emit keeps returning at once: one event in flight,
// one queued, and the rest dropped with an error the caller only logs.
func TestFullQueueDropsWithoutBlocking(t *testing.T) {
	fake := newFakeScoresAPI()
	fake.block = make(chan struct{})
	server := httptest.NewServer(fake)
	defer server.Close()

	scores := NewScores(Config{BaseURL: server.URL, PublicKey: "pk", SecretKey: "sk"}, WithScoreQueueSize(1))
	emit := func() error {
		return scores.Emit(context.Background(), telemetry.CategoricalScore("article-index:index-1", "user_vote", "up", firstVote))
	}
	if err := emit(); err != nil {
		t.Fatal(err)
	}
	waitArrived(t, fake) // first event is now in flight
	if err := emit(); err != nil {
		t.Fatalf("second event should be queued: %v", err)
	}

	start := time.Now()
	for range 100 {
		if err := emit(); err == nil {
			t.Fatal("event accepted beyond the queue's capacity")
		}
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Emit blocked for %v with a full queue", elapsed)
	}

	close(fake.block)
	if err := scores.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(fake.received()); got != 2 {
		t.Fatalf("Langfuse received %d scores, want 2 (in flight + queued)", got)
	}
}

func TestShutdownGivesUpAtDeadlineAndRejectsLaterEvents(t *testing.T) {
	fake := newFakeScoresAPI()
	fake.block = make(chan struct{})
	server := httptest.NewServer(fake)
	defer server.Close()
	defer close(fake.block)

	scores := NewScores(Config{BaseURL: server.URL, PublicKey: "pk", SecretKey: "sk"})
	_ = scores.Emit(context.Background(), telemetry.CategoricalScore("article-index:index-1", "user_vote", "up", firstVote))
	waitArrived(t, fake)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = scores.Shutdown(ctx) // an error is fine; returning on time is the point
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Shutdown took %v past its deadline", elapsed)
	}
	if err := scores.Emit(context.Background(), telemetry.CategoricalScore("article-index:index-1", "user_vote", "down", firstVote)); err == nil {
		t.Fatal("Emit after Shutdown accepted an event")
	}
}

func TestLangfuseErrorIsNotFatal(t *testing.T) {
	fake := newFakeScoresAPI()
	fake.status = http.StatusInternalServerError
	server := httptest.NewServer(fake)
	defer server.Close()

	scores := NewScores(Config{BaseURL: server.URL, PublicKey: "pk", SecretKey: "sk"})
	for range 2 {
		if err := scores.Emit(context.Background(), telemetry.CategoricalScore("article-index:index-1", "user_vote", "up", firstVote)); err != nil {
			t.Fatal(err)
		}
	}
	if err := scores.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(fake.received()); got != 2 {
		t.Fatalf("worker stopped after a Langfuse error: %d requests, want 2", got)
	}
}
