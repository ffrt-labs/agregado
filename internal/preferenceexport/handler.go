// Package preferenceexport exposes the bounded Reader evidence n8n needs to
// regenerate PREFERENCES.md. It exports no Bookmark or Karakeep data.
package preferenceexport

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Signal is one Article's complete usable Reader evidence. An Article has at
// most one Open (its first) and one current explicit vote.
type Signal struct {
	ArticleID    string     `json:"article_id"`
	CanonicalURL string     `json:"canonical_url"`
	Title        string     `json:"title"`
	Author       string     `json:"author,omitempty"`
	SourceID     string     `json:"source_id,omitempty"`
	Summary      string     `json:"summary,omitempty"`
	Tags         []string   `json:"tags"`
	OpenedAt     *time.Time `json:"opened_at,omitempty"`
	Vote         string     `json:"vote,omitempty"`
}

type Store interface {
	Signals(context.Context) ([]Signal, error)
}

type Handler struct {
	secret string
	store  Store
}

func NewHandler(secret string, store Store) *Handler {
	return &Handler{secret: secret, store: store}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if h.secret == "" || r.Header.Get("X-Enrichment-Secret") != h.secret {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	signals, err := h.store.Signals(r.Context())
	if err != nil {
		http.Error(w, "preference signal export failed", http.StatusInternalServerError)
		return
	}
	if signals == nil {
		signals = []Signal{}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(struct {
		Signals []Signal `json:"signals"`
	}{Signals: signals})
}
