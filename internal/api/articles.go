package api

import (
	"context"
	"encoding/json"
	"net/http"

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
	List(ctx context.Context, limit int, offset int) ([]domain.Article, error)
	ListBySource(ctx context.Context, source string, limit, offset int) ([]domain.Article, error)
	MarkRead(ctx context.Context, id string) error
	MarkUnread(ctx context.Context, id string) error
	Search(ctx context.Context, query string, limit int, offset int) ([]domain.Article, error)
	Count(ctx context.Context) (int, error)
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
	articles, err := a.articles.List(r.Context(), limit, offset)

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
	var articles []domain.Article

	if sourceID != "" {
		articles, err = a.articles.ListBySource(r.Context(), sourceID, limit + 1, offset)
	} else {
		articles, err = a.articles.List(r.Context(), limit + 1, offset)
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
			Sort:           r.URL.Query().Get("sort"),
			Nav:            a.nav.Build(r.Context()),
		},
	)
}

func (a *ArticleHandler) SearchPage(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	limit, offset := ParsePagination(r)

	if query == "" {
		articles, err := a.articles.List(r.Context(), limit, offset)

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
