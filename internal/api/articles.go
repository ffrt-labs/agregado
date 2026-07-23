package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/go-chi/chi/v5"
)

type SourceLister interface {
	List(ctx context.Context, limit, offset int) ([]domain.Source, error)
}

type ArticleHandler struct {
	articles ArticleRepository
	sources  SourceLister
	nav      *NavBuilder
}


type ArticlesPageData struct {
	Articles		[]domain.Article
	HasPrev			bool
	HasMore			bool
	PrevOffset		int
	NextOffset		int
	Sources			[]domain.Source
	SelectedSource	string
	Sort			string
	Nav			NavData
}

type ArticleRepository interface {
	List(ctx context.Context, limit int, offset int, sort string) ([]domain.Article, error)
	ListBySource(ctx context.Context, source string, limit, offset int, sort string) ([]domain.Article, error)
	GetById(ctx context.Context, id string) (*domain.Article, error)
	MarkRead(ctx context.Context, id string) error
	MarkUnread(ctx context.Context, id string) error
	Search(ctx context.Context, query string, limit int, offset int) ([]domain.Article, error)
	Count(ctx context.Context) (int, error)
}

// newsletterURLPrefix marks an article whose ExternalURL has no real page to
// link to (see internal/ingestion/email/parser.go) — the reader page at
// /articles/{id} is its only destination, since Article.Content holds the body.
const newsletterURLPrefix = "newsletter:"

// webURL returns the article's real web home and whether one exists. Precedence
// (issue #2): the canonical URL extracted at parse time, else external_url when
// it is a real page. A newsletter with no web version — external_url is still
// the newsletter:<uuid> placeholder and no canonical URL was found — returns
// ("", false), meaning the in-app reader page is its only destination.
func webURL(a *domain.Article) (string, bool) {
	if a.CanonicalURL != nil && *a.CanonicalURL != "" {
		return *a.CanonicalURL, true
	}
	if !strings.HasPrefix(a.ExternalURL, newsletterURLPrefix) {
		return a.ExternalURL, true
	}
	return "", false
}

type ArticleReaderData struct {
	Article     domain.Article
	SourceName  string
	OriginalURL string // empty for newsletter: articles, which have no real page to link to
	// No Nav: the reader page renders through the sidebar-free layout_reader.html
	// so it leaks no nav counts or admin links off-network (issue #2).
}

func NewArticleHandler(articleRepo ArticleRepository, sourceLister SourceLister, nav *NavBuilder) *ArticleHandler {
	return &ArticleHandler{
		articles: articleRepo,
		sources:  sourceLister,
		nav:      nav,
	}
}

func (a *ArticleHandler) List(w http.ResponseWriter, r *http.Request) {
	limit, offset := ParsePagination(r)
	articles, err := a.articles.List(r.Context(), limit, offset, r.URL.Query().Get("sort"))

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(articles)
}

func (a *ArticleHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := a.articles.MarkRead(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *ArticleHandler) MarkUnread(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	err := a.articles.MarkUnread(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *ArticleHandler) Search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		writeError(w, http.StatusBadRequest, "q parameter is required")
		return
	}

	limit, offset := ParsePagination(r)

	articles, err := a.articles.Search(r.Context(), query, limit, offset)

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(articles)
}

func (a *ArticleHandler) ListPage(w http.ResponseWriter, r *http.Request) {
	limit, offset := ParsePagination(r)
	sources, err := a.sources.List(r.Context(), 100, 0)

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sourceID := r.URL.Query().Get("source_id")
	sort := r.URL.Query().Get("sort")
	var articles []domain.Article

	if sourceID != "" {
		articles, err = a.articles.ListBySource(r.Context(), sourceID, limit + 1, offset, sort)
	} else {
		articles, err = a.articles.List(r.Context(), limit + 1, offset, sort)
	}

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	hasMore := false
	if len(articles) > limit {
		hasMore = true
	}

	if hasMore {
		articles = articles[:limit]
	}

	render(
		w,
		"articles.html",
		ArticlesPageData{
			Articles:       articles,
			HasPrev:        offset > 0,
			HasMore:        hasMore,
			PrevOffset:     offset - limit,
			NextOffset:     offset + limit,
			Sources:        sources,
			SelectedSource: sourceID,
			Sort:           sort,
			Nav:            a.nav.Build(r.Context()),
		},
	)
}

func (a *ArticleHandler) SearchPage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	limit, offset := ParsePagination(r)

	if query == "" {
		articles, err := a.articles.List(r.Context(), limit, offset, r.URL.Query().Get("sort"))

		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		renderPartial(w, "article_list.html", "article-list",ArticlesPageData{ Articles: articles})
		return
	}

	articles, err := a.articles.Search(r.Context(), query, limit, offset)

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	renderPartial(w, "article_list.html", "article-list",ArticlesPageData{ Articles: articles })
}

// Open is the click-through target for every article title link (GET /r/{id}):
// it marks the article read, then redirects to wherever the content actually
// lives — the original page for RSS/manual articles, or our own reader page
// for newsletters, which have no real page of their own (see newsletterURLPrefix).
func (a *ArticleHandler) Open(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	article, err := a.articles.GetById(r.Context(), id)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	if err := a.articles.MarkRead(r.Context(), id); err != nil {
		log.Printf("articles: mark read failed id=%s: %v", id, err)
	}

	target, ok := webURL(article)
	if !ok {
		target = "/articles/" + article.ID
	}

	http.Redirect(w, r, target, http.StatusFound)
}

// GetPage renders the in-app reader (GET /articles/{id}) for newsletter
// articles, and for anyone who lands on this URL directly (e.g. a bookmark).
func (a *ArticleHandler) GetPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	article, err := a.articles.GetById(r.Context(), id)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	if err := a.articles.MarkRead(r.Context(), id); err != nil {
		log.Printf("articles: mark read failed id=%s: %v", id, err)
	}

	sources, _ := a.sources.List(r.Context(), 100, 0)
	sourceMap := make(map[string]string, len(sources))
	for _, s := range sources {
		sourceMap[s.ID] = s.Name
	}

	sourceName := ""
	if article.SourceID != nil {
		sourceName = sourceMap[*article.SourceID]
	}

	originalURL, _ := webURL(article)

	renderReader(w, "reader.html", ArticleReaderData{
		Article:     *article,
		SourceName:  sourceName,
		OriginalURL: originalURL,
	})
}
