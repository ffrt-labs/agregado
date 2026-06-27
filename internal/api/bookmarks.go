package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/go-chi/chi/v5"
)

type BookmarkRepo interface {
	FindSaved(ctx context.Context) ([]domain.Article, error)
	ToggleBookmark(ctx context.Context, id string) error
	Unsave(ctx context.Context, id string) error
	SaveExternalURL(ctx context.Context, url string) error
	Count(ctx context.Context) (int, error)
}

type BookmarkView struct {
	ID          string
	Title       string
	ExternalURL string
	Summary     *string
	SourceName  string
	SavedAt     *time.Time
	IsManual    bool
}

type BookmarksPageData struct {
	Bookmarks []BookmarkView
	Nav       NavData
}

type BookmarkHandler struct {
	articles BookmarkRepo
	sources  SourceLister
	nav      *NavBuilder
}

func NewBookmarkHandler(articles BookmarkRepo, sources SourceLister, nav *NavBuilder) *BookmarkHandler {
	return &BookmarkHandler{articles: articles, sources: sources, nav: nav}
}

func (h *BookmarkHandler) ListPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	saved, err := h.articles.FindSaved(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sources, _ := h.sources.List(ctx, 100, 0)
	sourceMap := make(map[string]string, len(sources))
	for _, s := range sources {
		sourceMap[s.ID] = s.Name
	}

	bookmarks := make([]BookmarkView, len(saved))
	for i, a := range saved {
		sourceName := ""
		if a.SourceID != nil {
			sourceName = sourceMap[*a.SourceID]
		}
		bookmarks[i] = BookmarkView{
			ID:          a.ID,
			Title:       a.Title,
			ExternalURL: a.ExternalURL,
			Summary:     a.Summary,
			SourceName:  sourceName,
			SavedAt:     a.SavedAt,
			IsManual:    a.SourceID == nil,
		}
	}

	render(w, "bookmarks.html", BookmarksPageData{
		Bookmarks: bookmarks,
		Nav:       h.nav.Build(ctx),
	})
}

func (h *BookmarkHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.articles.ToggleBookmark(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *BookmarkHandler) Remove(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.articles.Unsave(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *BookmarkHandler) SaveLink(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}
	if err := h.articles.SaveExternalURL(r.Context(), body.URL); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
