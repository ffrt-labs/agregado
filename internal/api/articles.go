package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/go-chi/chi/v5"
)

type ArticleHandler struct {
	articles	ArticleRepository
}

type ArticlesPageData struct {
	Articles	[]domain.Article
}

type ArticleRepository interface {
	List(ctx context.Context, limit int, offset int) ([]domain.Article, error)
	MarkRead(ctx context.Context, id string) error
	MarkUnread(ctx context.Context, id string) error
	Search(ctx context.Context, query string, limit int, offset int) ([]domain.Article, error)
}

func NewArticleHandler (articleRepo ArticleRepository) *ArticleHandler {
	return &ArticleHandler{
		articles: articleRepo,
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
	articles, err := a.articles.List(r.Context(), limit, offset)

	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	render(w, "articles.html", ArticlesPageData{ Articles: articles })
}
