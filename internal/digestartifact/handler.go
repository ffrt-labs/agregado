package digestartifact

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type Handler struct {
	secret  string
	service *Service
}

func NewHandler(secret string, service *Service) *Handler {
	return &Handler{secret: secret, service: service}
}

// Handle exposes retrieval only to n8n. Delivery and scheduling remain outside
// this slice; a POST simply gets-or-creates the immutable artifact for a day.
func (h *Handler) Handle(w http.ResponseWriter, r *http.Request) {
	if h.secret == "" || r.Header.Get("X-Enrichment-Secret") != h.secret {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	day := time.Now().UTC()
	if raw := strings.TrimPrefix(r.URL.Path, "/api/private/digests/"); raw != "" && raw != "/api/private/digests" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			http.Error(w, "invalid digest date", http.StatusBadRequest)
			return
		}
		day = parsed
	}
	a, created, err := h.service.ForDate(r.Context(), day)
	if err != nil {
		http.Error(w, "digest generation failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if created {
		w.WriteHeader(http.StatusCreated)
	}
	json.NewEncoder(w).Encode(a)
}
