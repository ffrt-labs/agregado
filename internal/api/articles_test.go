package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/go-chi/chi/v5"
)

type fakeArticleRepo struct {
	articles      map[string]*domain.Article
	getByIDErr    error
	markReadErr   error
	markReadCalls []string
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

func requestWithID(id string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/r/"+id, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func strptr(s string) *string { return &s }

func TestArticleHandler_Open(t *testing.T) {
	rssArticle := &domain.Article{ID: "rss-1", ExternalURL: strptr("https://example.com/post")}
	// A newsletter with no web home: nil external_url, nil canonical (Phase 21
	// replaced the newsletter:<uuid> sentinel with NULL). With the in-app reader
	// page removed (#238) it has nowhere to go.
	newsletterArticle := &domain.Article{ID: "news-1", ExternalURL: nil}
	newsletterWithCanonical := &domain.Article{
		ID: "news-2", ExternalURL: nil,
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
			name:         "newsletter without a web home returns 404, not marked read",
			repo:         &fakeArticleRepo{articles: map[string]*domain.Article{"news-1": newsletterArticle}},
			id:           "news-1",
			wantStatus:   http.StatusNotFound,
			wantMarkRead: false,
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
			handler := NewArticleHandler(tc.repo)
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
