package digestartifact

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// Choice is decoded (internal/ai/cloudflare.go's Select) from the model's
// response to OpDigestSelect's prompt (internal/ai/prompts.go), which
// promises exactly this snake_case shape:
// {"choices":[{"article_id":"...","why":"..."}]}. Without explicit json
// tags, encoding/json's case-insensitive fallback matches "Why" against
// "why" but never matches "ArticleID" against "article_id" — a different
// naming convention, not just a case difference — so ArticleID silently
// stayed empty on every real call, which made ForDate's byID lookup miss
// every candidate and ship an empty digest regardless of content quality.
func TestChoiceDecodesTheSelectPromptsSnakeCaseJSON(t *testing.T) {
	var response struct {
		Choices []Choice `json:"choices"`
	}
	raw := `{"choices":[{"article_id":"abc123","why":"Worth reading"}]}`
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(response.Choices) != 1 {
		t.Fatalf("got %d choices, want 1", len(response.Choices))
	}
	if got := response.Choices[0].ArticleID; got != "abc123" {
		t.Errorf("ArticleID = %q, want %q", got, "abc123")
	}
	if got := response.Choices[0].Why; got != "Worth reading" {
		t.Errorf("Why = %q, want %q", got, "Worth reading")
	}
}

type memoryStore struct {
	articles     []Article
	artifacts    map[string]Artifact
	since, until time.Time
	lookback     time.Duration
}

func (s *memoryStore) Candidates(_ context.Context, _ time.Time, lookback time.Duration) (Pool, error) {
	s.lookback = lookback
	return Pool{Since: s.since, Until: s.until, Articles: s.articles}, nil
}
func (s *memoryStore) Find(_ context.Context, day time.Time) (Artifact, bool, error) {
	a, ok := s.artifacts[day.Format("2006-01-02")]
	return a, ok, nil
}
func (s *memoryStore) Save(_ context.Context, a Artifact) (Artifact, bool, error) {
	key := a.Date.Format("2006-01-02")
	if existing, ok := s.artifacts[key]; ok {
		return existing, false, nil
	}
	s.artifacts[key] = a
	return a, true, nil
}

type fakeFrontier struct {
	calls      int
	chooseNone bool
}

func (f *fakeFrontier) Select(_ context.Context, candidates []Candidate) ([]Choice, error) {
	f.calls++
	if f.chooseNone {
		return nil, nil
	}
	choices := make([]Choice, len(candidates))
	for i, c := range candidates {
		choices[i] = Choice{ArticleID: c.ID, Why: "Worth reading"}
	}
	return choices, nil
}

func TestServicePersistsAndReusesADailyArtifact(t *testing.T) {
	store := &memoryStore{articles: []Article{
		{ID: "a", CanonicalURL: "https://a", Title: "A", Score: 5, Source: "one", Topics: []string{"tech"}},
		{ID: "b", CanonicalURL: "https://b", Title: "B", Score: 4, Source: "two", Topics: []string{"science"}},
		{ID: "c", CanonicalURL: "https://c", Title: "C", Score: 2, Source: "one", Topics: []string{"tech"}},
		{ID: "duplicate", CanonicalURL: "https://a", Title: "Duplicate", Score: 5},
	}, artifacts: map[string]Artifact{}}
	frontier := &fakeFrontier{}
	service := NewService(store, frontier, "https://agregado.example", 3, 10)
	day := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)

	first, created, err := service.ForDate(context.Background(), day)
	if err != nil || !created {
		t.Fatalf("first = (%+v, %v, %v), want created artifact", first, created, err)
	}
	if frontier.calls != 1 {
		t.Fatalf("frontier calls = %d, want 1", frontier.calls)
	}
	if first.CandidateCount != 3 || first.FloorPassCount != 2 || first.SelectedCount != 3 {
		t.Fatalf("counts = %d/%d/%d, want 3/2/3", first.CandidateCount, first.FloorPassCount, first.SelectedCount)
	}
	if !first.Items[2].Exploration {
		t.Fatal("third item must be a clearly marked exploration pick")
	}
	if first.Items[0].ReadURL != "https://agregado.example/r/a" || first.Items[0].UpvoteURL != "https://agregado.example/f/a/up" || first.Items[0].DownvoteURL != "https://agregado.example/f/a/down" {
		t.Fatalf("unexpected action URLs: %+v", first.Items[0])
	}

	second, created, err := service.ForDate(context.Background(), day)
	if err != nil || created {
		t.Fatalf("second created = %v, err = %v", created, err)
	}
	if frontier.calls != 1 {
		t.Fatalf("repeat called frontier %d times, want 1", frontier.calls)
	}
	if second.ID != first.ID || second.HTML != first.HTML || second.Text != first.Text {
		t.Fatal("repeat must return the persisted artifact unchanged")
	}
}

// #79's AC12: with zero candidates a small empty Digest is still produced
// (n8n sends it), but it is not persisted. Persisting it froze the date: the
// endpoint is idempotent, so once an empty artifact was saved, re-running the
// workflow after fixing the pipeline could only re-send the same empty email.
func TestServiceProducesButDoesNotPersistAnEmptyDigest(t *testing.T) {
	store := &memoryStore{artifacts: map[string]Artifact{}}
	frontier := &fakeFrontier{}
	service := NewService(store, frontier, "https://agregado.example", 3, 10)
	day := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)

	artifact, created, err := service.ForDate(context.Background(), day)
	if err != nil {
		t.Fatal(err)
	}
	if created || len(store.artifacts) != 0 {
		t.Fatalf("empty digest was persisted: created = %v, stored = %d", created, len(store.artifacts))
	}
	if artifact.SelectedCount != 0 || frontier.calls != 0 {
		t.Fatalf("empty artifact = %+v, calls = %d", artifact, frontier.calls)
	}
	if artifact.EmptyReason == "" || !strings.Contains(artifact.Text, artifact.EmptyReason) {
		t.Fatalf("empty digest must say why it is empty: reason %q, text %q", artifact.EmptyReason, artifact.Text)
	}

	store.articles = []Article{{ID: "a", CanonicalURL: "https://a", Title: "A", Score: 4}}
	later, created, err := service.ForDate(context.Background(), day)
	if err != nil || !created || later.SelectedCount != 1 {
		t.Fatalf("re-run after an empty digest = (%+v, %v, %v), want a fresh persisted artifact", later, created, err)
	}
}

// #79's AC12: "low-scoring candidates do not otherwise force an empty
// result". The top-up used to run only when at least one Article passed the
// floor, so a day of score-2 Articles shipped "No Articles passed".
func TestServiceTopsUpWithExplorationPicksWhenNothingPassesTheFloor(t *testing.T) {
	store := &memoryStore{articles: []Article{
		{ID: "a", CanonicalURL: "https://a", Title: "A", Score: 2, Source: "one"},
		{ID: "b", CanonicalURL: "https://b", Title: "B", Score: 2, Source: "two"},
		{ID: "c", CanonicalURL: "https://c", Title: "C", Score: 1, Source: "three"},
		{ID: "d", CanonicalURL: "https://d", Title: "D", Score: 1, Source: "four"},
	}, artifacts: map[string]Artifact{}}
	artifact, _, err := NewService(store, &fakeFrontier{}, "https://agregado.example", 3, 10).ForDate(context.Background(), time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if artifact.FloorPassCount != 0 || artifact.SelectedCount != 3 {
		t.Fatalf("counts = floor %d / selected %d, want 0/3", artifact.FloorPassCount, artifact.SelectedCount)
	}
	for _, item := range artifact.Items {
		if !item.Exploration {
			t.Fatalf("%s must be marked as an exploration pick", item.ID)
		}
	}
}

// A frontier answer that matches no candidate is how #158 hid for weeks: the
// artifact rendered as empty and was persisted as if it were a quiet day.
func TestServiceDoesNotPersistWhenTheFrontierChoosesNothing(t *testing.T) {
	store := &memoryStore{articles: []Article{{ID: "a", CanonicalURL: "https://a", Title: "A", Score: 4}}, artifacts: map[string]Artifact{}}
	artifact, created, err := NewService(store, &fakeFrontier{chooseNone: true}, "https://agregado.example", 3, 10).ForDate(context.Background(), time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if created || len(store.artifacts) != 0 {
		t.Fatal("a digest the frontier selected nothing for must not be persisted")
	}
	if artifact.CandidateCount != 1 || !strings.Contains(artifact.EmptyReason, "1 candidate") {
		t.Fatalf("reason must name the candidates the frontier rejected: %+v", artifact)
	}
}

// The candidate window is "successful Enrichment since the previous
// successful Digest, with a bounded fallback lookback"
// (docs/architecture/agregado.md), not a UTC published_at day. The store
// resolves it; the artifact records it so the next Digest starts exactly
// where this one ended.
func TestServiceRecordsTheCandidateWindowOnTheArtifact(t *testing.T) {
	since := time.Date(2026, 9, 13, 13, 0, 0, 0, time.UTC)
	until := time.Date(2026, 9, 14, 13, 0, 0, 0, time.UTC)
	store := &memoryStore{since: since, until: until, articles: []Article{{ID: "a", CanonicalURL: "https://a", Title: "A", Score: 4}}, artifacts: map[string]Artifact{}}
	artifact, _, err := NewService(store, &fakeFrontier{}, "https://agregado.example", 3, 10).ForDate(context.Background(), time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !artifact.WindowStart.Equal(since) || !artifact.WindowEnd.Equal(until) {
		t.Fatalf("window = %v..%v, want %v..%v", artifact.WindowStart, artifact.WindowEnd, since, until)
	}
	if store.lookback != FallbackLookback {
		t.Fatalf("lookback = %v, want %v", store.lookback, FallbackLookback)
	}
}
