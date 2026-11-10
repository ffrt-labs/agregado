package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/felipeafreitas/agregado/internal/broker"
	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/felipeafreitas/agregado/internal/storage"
	"github.com/go-chi/chi/v5"
)

// UnenrichedFinder is satisfied by *storage.ArticleRepo; declared as an
// interface to keep AdminHandler's dependency narrow.
type UnenrichedFinder interface {
	FindUnenriched(ctx context.Context) ([]string, error)
}

// EnrichPublisher mirrors storage.EnrichPublisher's shape — the admin
// backfill trigger publishes the same {article_id} message the storage
// worker publishes on ingest, so the same enrich consumer picks it up.
type EnrichPublisher interface {
	Publish(exchange, routingKey string, body []byte) error
}

type AdminHandler struct {
	tags      *storage.TagRepo
	articles  UnenrichedFinder
	publisher EnrichPublisher
	nav       *NavBuilder
}

func NewAdminHandler(
	tags *storage.TagRepo,
	articles UnenrichedFinder,
	publisher *broker.Publisher,
	nav *NavBuilder,
) *AdminHandler {
	return &AdminHandler{tags: tags, articles: articles, publisher: publisher, nav: nav}
}

type AdminTagsPageData struct {
	Tags []domain.Tag
	Nav  NavData
}

type TagPayload struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Color string `json:"color"`
}

// normalize lowercases + trims the slug so it matches the normalized classifier
// output (see ranker's slug lookup), and defaults an empty color.
func (p TagPayload) normalize() (name, slug, color string) {
	name = strings.TrimSpace(p.Name)
	slug = strings.ToLower(strings.TrimSpace(p.Slug))
	color = strings.TrimSpace(p.Color)
	if color == "" {
		color = "#64748b"
	}
	return
}

func (h *AdminHandler) TagsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	tags, err := h.tags.FindAll(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	render(w, "admin_tags.html", AdminTagsPageData{Tags: tags, Nav: h.nav.Build(ctx)})
}

func (h *AdminHandler) CreateTag(w http.ResponseWriter, r *http.Request) {
	var body TagPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, slug, color := body.normalize()
	if name == "" || slug == "" {
		writeError(w, http.StatusBadRequest, "name and slug are required")
		return
	}
	if err := h.tags.Create(r.Context(), name, slug, color); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) UpdateTag(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var body TagPayload
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	name, slug, color := body.normalize()
	if name == "" || slug == "" {
		writeError(w, http.StatusBadRequest, "name and slug are required")
		return
	}
	if err := h.tags.Update(r.Context(), id, name, slug, color); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *AdminHandler) DeleteTag(w http.ResponseWriter, r *http.Request) {
	if err := h.tags.Delete(r.Context(), chi.URLParam(r, "id")); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

type EnrichBackfillResult struct {
	Republished int `json:"republished"`
}

// EnrichBackfill republishes every article with content_source still NULL
// (never enriched — pre-Phase 17 rows, or ones whose original enrich
// trigger was lost) onto articles.enrich. Mirrors /api/digest/refresh and
// /api/backup/send: a manual admin trigger for work that otherwise only
// happens as a side effect of ingest.
func (h *AdminHandler) EnrichBackfill(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	ids, err := h.articles.FindUnenriched(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	republished := 0
	for _, id := range ids {
		body, err := json.Marshal(struct {
			ArticleID string `json:"article_id"`
		}{ArticleID: id})
		if err != nil {
			continue
		}
		if err := h.publisher.Publish("articles.enrich", "backfill", body); err != nil {
			writeError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		republished++
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(EnrichBackfillResult{Republished: republished})
}
