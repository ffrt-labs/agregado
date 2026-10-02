package digestartifact

import (
	"context"
	"encoding/json"
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
	articles  []Article
	artifacts map[string]Artifact
}

func (s *memoryStore) Candidates(context.Context, time.Time) ([]Article, error) {
	return s.articles, nil
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

type fakeFrontier struct{ calls int }

func (f *fakeFrontier) Select(_ context.Context, candidates []Candidate) ([]Choice, error) {
	f.calls++
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

func TestServiceProducesSmallEmptyDigestWithoutCallingFrontier(t *testing.T) {
	store := &memoryStore{artifacts: map[string]Artifact{}}
	frontier := &fakeFrontier{}
	artifact, _, err := NewService(store, frontier, "https://agregado.example", 3, 10).ForDate(context.Background(), time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if artifact.SelectedCount != 0 || frontier.calls != 0 {
		t.Fatalf("empty artifact = %+v, calls = %d", artifact, frontier.calls)
	}
}
