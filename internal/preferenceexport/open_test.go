package preferenceexport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/felipeafreitas/agregado/internal/telemetry"
)

type fakeOpenStore struct {
	url      string
	openedAt time.Time
	id       string
	err      error
}

func (s *fakeOpenStore) Open(_ context.Context, id string) (string, time.Time, error) {
	s.id = id
	return s.url, s.openedAt, s.err
}

// fakeScoreSink records Langfuse score events; err makes it fail like a full
// or closed queue.
type fakeScoreSink struct {
	events []telemetry.ScoreEvent
	err    error
}

func (f *fakeScoreSink) Emit(_ context.Context, event telemetry.ScoreEvent) error {
	f.events = append(f.events, event)
	return f.err
}

func openThrough(handler http.HandlerFunc, path string) *httptest.ResponseRecorder {
	router := chi.NewRouter()
	router.Get("/r/{id}", handler)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response
}

func TestOpenHandlerScoresTheOpenOnTheArticleTraceAtTheFirstOpenTime(t *testing.T) {
	firstOpen := time.Date(2026, 10, 30, 7, 15, 0, 0, time.UTC)
	store := &fakeOpenStore{url: "https://example.com/article", openedAt: firstOpen}
	sink := &fakeScoreSink{}
	handler := NewOpenHandler(store, sink).Handle

	openThrough(handler, "/r/article-1")
	openThrough(handler, "/r/article-1")

	want := telemetry.ScoreEvent{TraceKey: "article-index:article-1", Name: "opened", DataType: telemetry.Boolean, Value: 1, Timestamp: firstOpen}
	if len(sink.events) != 2 || sink.events[0] != want || sink.events[1] != want {
		t.Fatalf("events = %+v, want the same %+v for each open (one score in Langfuse)", sink.events, want)
	}
}

func TestOpenHandlerScoresNothingForLegacyArticles(t *testing.T) {
	sink := &fakeScoreSink{}
	response := openThrough(NewOpenHandler(&fakeOpenStore{err: ErrNotFound}, sink).HandleOr(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://legacy.example/article", http.StatusFound)
	}), "/r/legacy-article")

	if response.Header().Get("Location") != "https://legacy.example/article" {
		t.Fatalf("Location = %q, want the legacy redirect", response.Header().Get("Location"))
	}
	if len(sink.events) != 0 {
		t.Fatalf("events = %+v, want none for a legacy Article", sink.events)
	}
}

func TestOpenHandlerRedirectsWhenTheScoreSinkFails(t *testing.T) {
	store := &fakeOpenStore{url: "https://example.com/article", openedAt: time.Now()}
	response := openThrough(NewOpenHandler(store, &fakeScoreSink{err: errors.New("queue full")}).Handle, "/r/article-1")

	if response.Code != http.StatusFound || response.Header().Get("Location") != store.url {
		t.Fatalf("response = %d %q, want redirect to %q", response.Code, response.Header().Get("Location"), store.url)
	}
}

func TestOpenHandlerRecordsTheArticleOpenAndRedirects(t *testing.T) {
	store := &fakeOpenStore{url: "https://example.com/article"}
	router := chi.NewRouter()
	router.Get("/r/{id}", NewOpenHandler(store, nil).Handle)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/r/article-1", nil))

	if response.Code != http.StatusFound || response.Header().Get("Location") != store.url {
		t.Fatalf("response = %d %q, want redirect to %q", response.Code, response.Header().Get("Location"), store.url)
	}
	if store.id != "article-1" {
		t.Fatalf("opened id = %q, want article-1", store.id)
	}
}

func TestOpenHandlerDoesNotRedirectUnknownArticles(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/r/{id}", NewOpenHandler(&fakeOpenStore{err: ErrNotFound}, nil).Handle)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/r/missing", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestOpenHandlerFallsBackForLegacyReaderArticles(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/r/{id}", NewOpenHandler(&fakeOpenStore{err: ErrNotFound}, nil).HandleOr(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://legacy.example/article", http.StatusFound)
	}))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/r/legacy-article", nil))

	if response.Code != http.StatusFound || response.Header().Get("Location") != "https://legacy.example/article" {
		t.Fatalf("response = %d %q, want legacy redirect", response.Code, response.Header().Get("Location"))
	}
}

func TestOpenHandlerDoesNotHideStorageFailures(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/r/{id}", NewOpenHandler(&fakeOpenStore{err: errors.New("database unavailable")}, nil).Handle)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/r/article-1", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}
