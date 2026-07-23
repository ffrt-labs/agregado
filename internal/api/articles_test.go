package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/go-chi/chi/v5"
)

// fakeArticleRepo satisfies both ArticleRepository and NavQuerier, so the
// same fake can back the handler under test and the *NavBuilder it holds.
type fakeArticleRepo struct {
	articles      map[string]*domain.Article
	getByIDErr    error
	markReadErr   error
	markReadCalls []string
}

func (f *fakeArticleRepo) List(ctx context.Context, limit, offset int, sort string) ([]domain.Article, error) {
	return nil, nil
}

func (f *fakeArticleRepo) ListBySource(ctx context.Context, source string, limit, offset int, sort string) ([]domain.Article, error) {
	return nil, nil
}

func (f *fakeArticleRepo) GetById(ctx context.Context, id string) (*domain.Article, error) {
	if f.getByIDErr != nil {
		return nil, f.getByIDErr
	}
	a, ok := f.articles[id]
	if !ok {
		return nil, errors.New("no rows")
	}
	return a, nil
}

func (f *fakeArticleRepo) MarkRead(ctx context.Context, id string) error {
	f.markReadCalls = append(f.markReadCalls, id)
	return f.markReadErr
}

func (f *fakeArticleRepo) MarkUnread(ctx context.Context, id string) error { return nil }

func (f *fakeArticleRepo) Search(ctx context.Context, query string, limit, offset int) ([]domain.Article, error) {
	return nil, nil
}

func (f *fakeArticleRepo) Count(ctx context.Context) (int, error) { return 0, nil }

func (f *fakeArticleRepo) CountAboveScore(ctx context.Context, minScore int) (int, error) {
	return 0, nil
}

func (f *fakeArticleRepo) CountSaved(ctx context.Context) (int, error) { return 0, nil }

type fakeSourceLister struct {
	sources []domain.Source
}

func (f *fakeSourceLister) List(ctx context.Context, limit, offset int) ([]domain.Source, error) {
	return f.sources, nil
}

func newTestHandler(repo *fakeArticleRepo, sources *fakeSourceLister) *ArticleHandler {
	nav := NewNavBuilder(repo, sources, 70)
	return NewArticleHandler(repo, sources, nav)
}

func requestWithID(id string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/r/"+id, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func strptr(s string) *string { return &s }

func TestArticleHandler_Open(t *testing.T) {
	rssArticle := &domain.Article{ID: "rss-1", ExternalURL: "https://example.com/post"}
	newsletterArticle := &domain.Article{ID: "news-1", ExternalURL: "newsletter:abc-123"}
	newsletterWithCanonical := &domain.Article{
		ID: "news-2", ExternalURL: "newsletter:def-456",
		CanonicalURL: strptr("https://newsletter.example.com/p/issue-42"),
	}

	cases := []struct {
		name         string
		repo         *fakeArticleRepo
		id           string
		wantStatus   int
		wantLocation string
		wantMarkRead bool
	}{
		{
			name:         "rss article redirects to external url",
			repo:         &fakeArticleRepo{articles: map[string]*domain.Article{"rss-1": rssArticle}},
			id:           "rss-1",
			wantStatus:   http.StatusFound,
			wantLocation: "https://example.com/post",
			wantMarkRead: true,
		},
		{
			name:         "newsletter without canonical url redirects to reader page",
			repo:         &fakeArticleRepo{articles: map[string]*domain.Article{"news-1": newsletterArticle}},
			id:           "news-1",
			wantStatus:   http.StatusFound,
			wantLocation: "/articles/news-1",
			wantMarkRead: true,
		},
		{
			name:         "newsletter with canonical url redirects to the canonical url",
			repo:         &fakeArticleRepo{articles: map[string]*domain.Article{"news-2": newsletterWithCanonical}},
			id:           "news-2",
			wantStatus:   http.StatusFound,
			wantLocation: "https://newsletter.example.com/p/issue-42",
			wantMarkRead: true,
		},
		{
			name:         "unknown id returns 404, no redirect",
			repo:         &fakeArticleRepo{articles: map[string]*domain.Article{}},
			id:           "missing",
			wantStatus:   http.StatusNotFound,
			wantMarkRead: false,
		},
		{
			name: "mark-read failure does not block the redirect",
			repo: &fakeArticleRepo{
				articles:    map[string]*domain.Article{"rss-1": rssArticle},
				markReadErr: errors.New("db down"),
			},
			id:           "rss-1",
			wantStatus:   http.StatusFound,
			wantLocation: "https://example.com/post",
			wantMarkRead: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := newTestHandler(tc.repo, &fakeSourceLister{})
			w := httptest.NewRecorder()
			handler.Open(w, requestWithID(tc.id))

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if tc.wantLocation != "" {
				if got := w.Header().Get("Location"); got != tc.wantLocation {
					t.Errorf("Location = %q, want %q", got, tc.wantLocation)
				}
			}
			gotMarkRead := len(tc.repo.markReadCalls) == 1 && tc.repo.markReadCalls[0] == tc.id
			if tc.wantMarkRead && !gotMarkRead {
				t.Errorf("expected MarkRead(%q), got calls %v", tc.id, tc.repo.markReadCalls)
			}
			if !tc.wantMarkRead && len(tc.repo.markReadCalls) != 0 {
				t.Errorf("expected no MarkRead call, got %v", tc.repo.markReadCalls)
			}
		})
	}
}

// chdirToRepoRoot points the working directory at the repo root for the
// duration of the test, since render() resolves templates relative to cwd
// ("templates/"+filename) — matching how the binary is actually run.
func chdirToRepoRoot(t *testing.T) {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(wd, "..", "..")
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
}

func TestArticleHandler_GetPage(t *testing.T) {
	chdirToRepoRoot(t)

	sourceID := "src-1"
	rssArticle := &domain.Article{
		ID: "rss-1", SourceID: &sourceID, Title: "An RSS Post", ExternalURL: "https://example.com/post",
	}
	newsletterArticle := &domain.Article{
		ID: "news-1", Title: "A Newsletter Issue", ExternalURL: "newsletter:abc-123",
	}
	sources := &fakeSourceLister{sources: []domain.Source{{ID: "src-1", Name: "Example Feed"}}}

	t.Run("renders rss article with a link to the original", func(t *testing.T) {
		repo := &fakeArticleRepo{articles: map[string]*domain.Article{"rss-1": rssArticle}}
		handler := newTestHandler(repo, sources)
		w := httptest.NewRecorder()
		handler.GetPage(w, requestWithID("rss-1"))

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "An RSS Post") {
			t.Errorf("body missing article title: %s", body)
		}
		if !strings.Contains(body, "Example Feed") {
			t.Errorf("body missing source name: %s", body)
		}
		if !strings.Contains(body, "https://example.com/post") {
			t.Errorf("body missing original-article link for an RSS article: %s", body)
		}
		if len(repo.markReadCalls) != 1 || repo.markReadCalls[0] != "rss-1" {
			t.Errorf("expected MarkRead(rss-1), got %v", repo.markReadCalls)
		}
		// The reader page is served publicly through the tunnel; it must leak
		// no admin links or nav read-counts off-network (issue #2).
		for _, leak := range []string{"/admin/logs", "/admin/prompts", "/admin/tags", "Daily Digest", "scored today"} {
			if strings.Contains(body, leak) {
				t.Errorf("reader page leaked nav element %q: %s", leak, body)
			}
		}
	})

	t.Run("renders newsletter article without a broken original-url link", func(t *testing.T) {
		repo := &fakeArticleRepo{articles: map[string]*domain.Article{"news-1": newsletterArticle}}
		handler := newTestHandler(repo, sources)
		w := httptest.NewRecorder()
		handler.GetPage(w, requestWithID("news-1"))

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200, body=%s", w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, "A Newsletter Issue") {
			t.Errorf("body missing article title: %s", body)
		}
		if strings.Contains(body, "newsletter:abc-123") {
			t.Errorf("body leaked the internal newsletter: scheme URL: %s", body)
		}
		if strings.Contains(body, "ZgotmplZ") {
			t.Errorf("body contains html/template's unsafe-URL sentinel: %s", body)
		}
	})

	t.Run("unknown id returns 404", func(t *testing.T) {
		repo := &fakeArticleRepo{articles: map[string]*domain.Article{}}
		handler := newTestHandler(repo, sources)
		w := httptest.NewRecorder()
		handler.GetPage(w, requestWithID("missing"))

		if w.Code != http.StatusNotFound {
			t.Errorf("status = %d, want 404", w.Code)
		}
	})
}
