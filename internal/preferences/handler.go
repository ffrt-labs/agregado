package preferences

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// maxRequestBytes leaves room above MaxBytes so an oversized profile gets a
// validation message rather than a truncated read.
const maxRequestBytes = 4 * MaxBytes

// Handler is the private n8n-facing surface, behind the same shared secret as
// the other /api/private endpoints.
type Handler struct {
	secret  string
	service *Service
}

func NewHandler(secret string, service *Service) *Handler {
	return &Handler{secret: secret, service: service}
}

func (h *Handler) authorized(w http.ResponseWriter, r *http.Request) bool {
	if h.secret == "" || r.Header.Get("X-Enrichment-Secret") != h.secret {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// Get returns the accepted profile as Markdown.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(w, r) {
		return
	}
	text, err := h.service.Current(r.Context())
	if errors.Is(err, ErrNoProfile) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		slog.Error("read preference profile failed", "component", "preferences", "err", err)
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	io.WriteString(w, text)
}

// Put stores the request body, the Doc exported as Markdown, as the accepted
// profile.
func (h *Handler) Put(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(w, r) {
		return
	}
	defer r.Body.Close()
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBytes))
	if err != nil {
		http.Error(w, "profile too large", http.StatusRequestEntityTooLarge)
		return
	}
	_, changed, err := h.service.Replace(r.Context(), string(body))
	var invalid *InvalidError
	if errors.As(err, &invalid) {
		http.Error(w, invalid.Error(), http.StatusBadRequest)
		return
	}
	if err != nil {
		slog.Error("store preference profile failed", "component", "preferences", "err", err)
		http.Error(w, "store failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"changed": changed})
}

// Regenerate rewrites the accepted profile from Reader evidence and returns
// it, so n8n can write it back to the Doc.
func (h *Handler) Regenerate(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(w, r) {
		return
	}
	result, err := h.service.Regenerate(r.Context())
	if errors.Is(err, ErrNoProfile) {
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	if err != nil {
		slog.Error("preference regeneration failed", "component", "preferences", "err", err)
		http.Error(w, "regeneration failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, result)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
