package articleindex

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/felipeafreitas/agregado/internal/ingestion/fetch"
	"github.com/felipeafreitas/agregado/internal/telemetry"
)

type summaryModel struct {
	fakeModel
	raw        string
	err        error
	gotContent string
}

func (m *summaryModel) Summarize(ctx context.Context, title, content string) (string, error) {
	m.gotContent = content
	return m.raw, m.err
}

type errFetcher struct{}

func (errFetcher) Fetch(context.Context, string) (fetch.Result, error) {
	return fetch.Result{}, errors.New("blocked")
}

func process(t *testing.T, f Fetcher, m Model, req Request) (Record, error) {
	t.Helper()
	store := &fakeStore{records: map[string]Record{}}
	record, _, err := NewService(store, f, m, fakePreferences{}).Process(context.Background(), req)
	return record, err
}

func request() Request {
	return Request{MinifluxEntryID: 1, CanonicalURL: "https://example.com/a", Title: "A title"}
}

func TestSummaryStoresStructuredClaimsAsJSON(t *testing.T) {
	m := &summaryModel{fakeModel: fakeModel{tag: "tech", score: 3},
		raw: "```json\n{\"kind\":\"news\",\"claims\":[\"c1\",\"c2\",\"c3\",\"c4\"],\"entities\":[\"a\",\"b\",\"c\",\"d\",\"e\",\"f\",\"g\"]}\n```"}
	record, err := process(t, &fakeFetcher{text: "body"}, m, request())
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Kind     string   `json:"kind"`
		Claims   []string `json:"claims"`
		Entities []string `json:"entities"`
	}
	if err := json.Unmarshal([]byte(record.Summary), &got); err != nil {
		t.Fatalf("summary is not JSON: %q", record.Summary)
	}
	if got.Kind != "news" || len(got.Claims) != 3 || len(got.Entities) != 6 {
		t.Fatalf("contract not enforced: %+v", got)
	}
}

func TestSummaryUnknownKindBecomesOther(t *testing.T) {
	m := &summaryModel{fakeModel: fakeModel{tag: "t", score: 3}, raw: `{"kind":"memoir","claims":["c"],"entities":[]}`}
	record, _ := process(t, &fakeFetcher{text: "body"}, m, request())
	if !strings.Contains(record.Summary, `"kind":"other"`) {
		t.Fatalf("got %q", record.Summary)
	}
}

func TestSummaryFallsBackToLeadExtractOnModelError(t *testing.T) {
	words := make([]string, 200)
	for i := range words {
		words[i] = fmt.Sprintf("w%d", i)
	}
	m := &summaryModel{fakeModel: fakeModel{tag: "t", score: 3}, err: errors.New("cloudflare down")}
	record, err := process(t, &fakeFetcher{text: strings.Join(words, " ")}, m, request())
	if err != nil || record.Status != Complete {
		t.Fatalf("model error must not fail the Article: %v %v", record.Status, err)
	}
	if !strings.HasPrefix(record.Summary, "A title\n") || !strings.Contains(record.Summary, "w149") || strings.Contains(record.Summary, "w150") {
		t.Fatalf("want title + first 150 words, got %q", record.Summary)
	}
}

func TestSummaryFallsBackToLeadExtractOnInvalidJSON(t *testing.T) {
	for _, raw := range []string{"not json", `{"kind":"news","claims":[],"entities":[]}`, ""} {
		m := &summaryModel{fakeModel: fakeModel{tag: "t", score: 3}, raw: raw}
		record, _ := process(t, &fakeFetcher{text: "the body"}, m, request())
		if record.Summary != "A title\nthe body" {
			t.Fatalf("raw %q: got %q", raw, record.Summary)
		}
	}
}

func TestFeedContentIsPartialFallbackWhenFetchFails(t *testing.T) {
	m := &summaryModel{fakeModel: fakeModel{tag: "t", score: 3}, err: errors.New("down")}
	req := request()
	req.FeedContent = "<p>feed teaser</p>"
	record, err := process(t, errFetcher{}, m, req)
	if err != nil || record.Status != Complete {
		t.Fatalf("feed content should rescue the Article: %v %v", record.Status, err)
	}
	if !strings.HasPrefix(m.gotContent, "[content: partial]") || !strings.Contains(m.gotContent, "feed teaser") {
		t.Fatalf("model must see the partial marker, got %q", m.gotContent)
	}
	if !strings.Contains(record.Summary, "[content: partial]") {
		t.Fatalf("lead extract must keep the marker, got %q", record.Summary)
	}
}

func TestPartialMarkerSurvivesOnStructuredSummary(t *testing.T) {
	m := &summaryModel{fakeModel: fakeModel{tag: "t", score: 3}, raw: `{"kind":"news","claims":["c"],"entities":[]}`}
	req := request()
	req.FeedContent = "teaser"
	record, _ := process(t, errFetcher{}, m, req)
	if !strings.HasPrefix(record.Summary, "[content: partial]\n{") {
		t.Fatalf("selection must see the marker, got %q", record.Summary)
	}
}

func TestFullContentIsNotMarkedPartial(t *testing.T) {
	m := &summaryModel{fakeModel: fakeModel{tag: "t", score: 3}, raw: `{"kind":"news","claims":["c"],"entities":[]}`}
	req := request()
	req.FeedContent = "teaser"
	process(t, &fakeFetcher{text: "full body"}, m, req)
	if strings.Contains(m.gotContent, "partial") {
		t.Fatalf("got %q", m.gotContent)
	}
}

// fakeScoreSink records Langfuse score events; err makes every Emit fail like
// a full queue.
type fakeScoreSink struct {
	events []telemetry.ScoreEvent
	err    error
}

func (f *fakeScoreSink) Emit(_ context.Context, event telemetry.ScoreEvent) error {
	f.events = append(f.events, event)
	return f.err
}

func processScored(t *testing.T, sink telemetry.ScoreSink, m Model) (Record, error) {
	t.Helper()
	store := &fakeStore{records: map[string]Record{}}
	record, _, err := NewService(store, &fakeFetcher{text: "the body"}, m, fakePreferences{}, WithScoreSink(sink)).
		Process(context.Background(), request())
	return record, err
}

// wantScores checks the named scores were emitted as Booleans with these
// values on the enrichment's trace.
func wantScores(t *testing.T, sink *fakeScoreSink, want map[string]float64) {
	t.Helper()
	got := map[string]float64{}
	for _, e := range sink.events {
		if e.TraceKey != telemetry.ArticleIndexKey("index-1") || e.DataType != telemetry.Boolean {
			t.Fatalf("score must be a Boolean on the enrichment trace: %+v", e)
		}
		got[e.Name] = e.Value
	}
	for name, v := range want {
		if g, ok := got[name]; !ok || g != v {
			t.Errorf("score %s = %v (emitted %v), want %v; all: %v", name, g, ok, v, got)
		}
	}
}

func TestValidSummaryScoresValidAndNoFallback(t *testing.T) {
	sink := &fakeScoreSink{}
	m := &summaryModel{fakeModel: fakeModel{tag: "tech", score: 3}, raw: `{"kind":"news","claims":["c"],"entities":[]}`}
	if _, err := processScored(t, sink, m); err != nil {
		t.Fatal(err)
	}
	wantScores(t, sink, map[string]float64{"summary_valid": 1, "summary_fallback": 0})
	for _, e := range sink.events {
		if e.Timestamp.IsZero() || !e.Timestamp.Equal(sink.events[0].Timestamp) {
			t.Fatalf("one enrichment's scores share one stable timestamp: %+v", sink.events)
		}
	}
}

func TestInvalidJSONSummaryScoresInvalidAndFallback(t *testing.T) {
	sink := &fakeScoreSink{}
	m := &summaryModel{fakeModel: fakeModel{tag: "tech", score: 3}, raw: "not json"}
	processScored(t, sink, m)
	wantScores(t, sink, map[string]float64{"summary_valid": 0, "summary_fallback": 1})
}

func TestSummaryModelErrorScoresInvalidAndFallback(t *testing.T) {
	sink := &fakeScoreSink{}
	m := &summaryModel{fakeModel: fakeModel{tag: "tech", score: 3}, err: errors.New("cloudflare down")}
	processScored(t, sink, m)
	wantScores(t, sink, map[string]float64{"summary_valid": 0, "summary_fallback": 1})
}

func TestFailingScoreSinkDoesNotChangeEnrichment(t *testing.T) {
	m := &summaryModel{fakeModel: fakeModel{tag: "tech", score: 3}, raw: `{"kind":"news","claims":["c"],"entities":[]}`}
	want, err := processScored(t, telemetry.NopScoreSink{}, m)
	if err != nil {
		t.Fatal(err)
	}
	sink := &fakeScoreSink{err: errors.New("queue full")}
	got, err := processScored(t, sink, m)
	if err != nil || got.Status != Complete || got.Summary != want.Summary || got.Tags[0] != want.Tags[0] || got.Score != want.Score {
		t.Fatalf("failing sink changed the result: %+v, %v; want %+v", got, err, want)
	}
	if len(sink.events) != 3 {
		t.Fatalf("want all three scores attempted, got %d", len(sink.events))
	}
}

func TestNoContentAtAllStillFails(t *testing.T) {
	m := &summaryModel{fakeModel: fakeModel{tag: "t", score: 3}}
	if _, err := process(t, errFetcher{}, m, request()); err == nil {
		t.Fatal("want failure when neither fetch nor feed content exists")
	}
}
