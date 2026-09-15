package preferenceexport

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
)

var ErrNotFound = errors.New("article index record not found")

// OpenStore records only an Article's first Open, so repeated opens cannot
// amplify this weak preference signal.
type OpenStore interface {
	Open(context.Context, string) (string, error)
}

type OpenHandler struct{ store OpenStore }

func NewOpenHandler(store OpenStore) *OpenHandler { return &OpenHandler{store: store} }

func (h *OpenHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.HandleOr(nil)(w, r)
}

// HandleOr preserves another Reader's established click-through behavior when
// the URL belongs to a legacy Article rather than the Article Index.
func (h *OpenHandler) HandleOr(fallback http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		canonicalURL, err := h.store.Open(r.Context(), chi.URLParam(r, "id"))
		if err != nil {
			if fallback != nil && errors.Is(err, ErrNotFound) {
				fallback(w, r)
				return
			}
			if errors.Is(err, ErrNotFound) {
				http.Error(w, "Not Found", http.StatusNotFound)
				return
			}
			http.Error(w, "record open failed", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, canonicalURL, http.StatusFound)
	}
}
