package preferenceexport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

type fakeOpenStore struct {
	url string
	id  string
	err error
}

func (s *fakeOpenStore) Open(_ context.Context, id string) (string, error) {
	s.id = id
	return s.url, s.err
}

func TestOpenHandlerRecordsTheArticleOpenAndRedirects(t *testing.T) {
	store := &fakeOpenStore{url: "https://example.com/article"}
	router := chi.NewRouter()
	router.Get("/r/{id}", NewOpenHandler(store).Handle)
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
	router.Get("/r/{id}", NewOpenHandler(&fakeOpenStore{err: ErrNotFound}).Handle)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/r/missing", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func TestOpenHandlerFallsBackForLegacyReaderArticles(t *testing.T) {
	router := chi.NewRouter()
	router.Get("/r/{id}", NewOpenHandler(&fakeOpenStore{err: ErrNotFound}).HandleOr(func(w http.ResponseWriter, r *http.Request) {
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
	router.Get("/r/{id}", NewOpenHandler(&fakeOpenStore{err: errors.New("database unavailable")}).Handle)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/r/article-1", nil))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}
