package articleindex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/felipeafreitas/agregado/internal/ingestion/fetch"
)

type fakeStore struct {
	records map[string]Record
}

func (s *fakeStore) Create(ctx context.Context, record Record) (Record, bool, error) {
	if existing, ok := s.records[record.CanonicalURL]; ok {
		return existing, false, nil
	}
	record.ID = "index-1"
	record.Status = Processing
	s.records[record.CanonicalURL] = record
	return record, true, nil
}

func (s *fakeStore) Complete(ctx context.Context, record Record) error {
	s.records[record.CanonicalURL] = record
	return nil
}

func (s *fakeStore) Fail(ctx context.Context, id string, reason string) error { return nil }

type fakeFetcher struct {
	calls int
	text  string
}

func (f *fakeFetcher) Fetch(ctx context.Context, url string) (fetch.Result, error) {
	f.calls++
	return fetch.Result{Markdown: f.text, Length: len(f.text)}, nil
}

type fakeModel struct {
	summary, tag string
	score        int
	calls        int
	preferences  string
}

func (m *fakeModel) Summarize(ctx context.Context, title, content string) (string, error) {
	m.calls++
	return m.summary, nil
}
func (m *fakeModel) Categorize(ctx context.Context, title, content string) (string, error) {
	m.calls++
	return m.tag, nil
}
func (m *fakeModel) Score(ctx context.Context, title, content, preferences string) (int, error) {
	m.calls++
	m.preferences = preferences
	return m.score, nil
}

type fakePreferences struct{ text string }

func (p fakePreferences) Read() (string, error) { return p.text, nil }

func TestHandlerProcessesCanonicalArticleOnlyOnce(t *testing.T) {
	store := &fakeStore{records: map[string]Record{}}
	fetcher := &fakeFetcher{text: "fetched canonical article"}
	model := &fakeModel{summary: "A short summary", tag: "technology", score: 4}
	h := NewHandler("test-secret", NewService(store, fetcher, model, fakePreferences{"# Preferences\n- technology"}))

	body := `{"entry_id":42,"canonical_url":"https://example.com/article","title":"An article"}`
	request := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/private/articles/enrich", strings.NewReader(body))
		r.Header.Set("X-Enrichment-Secret", "test-secret")
		w := httptest.NewRecorder()
		h.Handle(w, r)
		return w
	}

	if got := request().Code; got != http.StatusCreated {
		t.Fatalf("first status = %d, want %d", got, http.StatusCreated)
	}
	if got := request().Code; got != http.StatusOK {
		t.Fatalf("second status = %d, want %d", got, http.StatusOK)
	}
	if fetcher.calls != 1 || model.calls != 3 {
		t.Fatalf("duplicate called fetch/model %d/%d times, want 1/3", fetcher.calls, model.calls)
	}
	record := store.records["https://example.com/article"]
	if record.Status != Complete || record.Summary != "A short summary" || record.Score != 4 {
		t.Fatalf("stored record = %+v, want completed content-free enrichment", record)
	}
	if record.MinifluxEntryID != 42 || len(record.Tags) != 1 || record.Tags[0] != "technology" {
		t.Fatalf("stored metadata = %+v", record)
	}
	if model.preferences != "# Preferences\n- technology" {
		t.Fatalf("score preferences = %q", model.preferences)
	}
}

func TestHandlerUsesBridgeContentWithoutFetching(t *testing.T) {
	store := &fakeStore{records: map[string]Record{}}
	fetcher := &fakeFetcher{text: "should not be used"}
	model := &fakeModel{summary: "summary", tag: "newsletter", score: 3}
	h := NewHandler("test-secret", NewService(store, fetcher, model, fakePreferences{"seed"}))
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"entry_id":8,"canonical_url":"https://bridge.example/uuid","title":"Email-only","bridge_content":"email body"}`))
	r.Header.Set("X-Enrichment-Secret", "test-secret")
	w := httptest.NewRecorder()
	h.Handle(w, r)

	if w.Code != http.StatusCreated || fetcher.calls != 0 {
		t.Fatalf("status/fetch calls = %d/%d, want 201/0", w.Code, fetcher.calls)
	}
}

func TestHandlerRejectsUnauthorizedAndMalformedRequests(t *testing.T) {
	h := NewHandler("test-secret", NewService(&fakeStore{records: map[string]Record{}}, &fakeFetcher{}, &fakeModel{}, fakePreferences{}))
	for _, tc := range []struct {
		name, body string
		secret     string
		want       int
	}{
		{"missing secret", `{}`, "", http.StatusUnauthorized},
		{"missing canonical url", `{"entry_id":1,"title":"x"}`, "test-secret", http.StatusBadRequest},
		{"bad canonical url", `{"entry_id":1,"canonical_url":"ftp://example.com","title":"x"}`, "test-secret", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			r.Header.Set("X-Enrichment-Secret", tc.secret)
			w := httptest.NewRecorder()
			h.Handle(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d", w.Code, tc.want)
			}
		})
	}
}
