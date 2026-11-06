package preferenceexport

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/felipeafreitas/agregado/internal/telemetry"
)

var ErrNotFound = errors.New("article index record not found")

// OpenedScore is the Langfuse score name for an Article's first Open.
const OpenedScore = "opened"

// OpenStore records only an Article's first Open, so repeated opens cannot
// amplify this weak preference signal. It returns the Article's canonical URL
// and the time of that first Open.
type OpenStore interface {
	Open(context.Context, string) (string, time.Time, error)
}

type OpenHandler struct {
	store  OpenStore
	scores telemetry.ScoreSink
}

// NewOpenHandler records Opens in store and sends each as an `opened` score
// to scores (nil discards them).
func NewOpenHandler(store OpenStore, scores telemetry.ScoreSink) *OpenHandler {
	return &OpenHandler{store: store, scores: scores}
}

func (h *OpenHandler) Handle(w http.ResponseWriter, r *http.Request) {
	h.HandleOr(nil)(w, r)
}

// HandleOr preserves another Reader's established click-through behavior when
// the URL belongs to a legacy Article rather than the Article Index.
func (h *OpenHandler) HandleOr(fallback http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		canonicalURL, firstOpenedAt, err := h.store.Open(r.Context(), id)
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
		// The first Open's time keeps the score's timestamp stable, so a
		// repeated Open overwrites the same `opened` score instead of adding one.
		score := telemetry.BooleanScore(telemetry.ArticleIndexKey(id), OpenedScore, true, firstOpenedAt)
		telemetry.EmitScore(r.Context(), h.scores, score)
		http.Redirect(w, r, canonicalURL, http.StatusFound)
	}
}
