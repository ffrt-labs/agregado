package api

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ExplicitVoteRecorder keeps the single current strong preference signal for
// an Article. It is separate from an Open, which is weak evidence.
type ExplicitVoteRecorder interface {
	SetCurrent(ctx context.Context, articleID, vote string) error
}

// ExplicitFeedbackHandler serves the public feedback links in a Digest. The
// confirmation intentionally offers no reading surface.
type ExplicitFeedbackHandler struct{ votes ExplicitVoteRecorder }

func NewExplicitFeedbackHandler(votes ExplicitVoteRecorder) *ExplicitFeedbackHandler {
	return &ExplicitFeedbackHandler{votes: votes}
}

func (h *ExplicitFeedbackHandler) Handle(w http.ResponseWriter, r *http.Request) {
	vote := chi.URLParam(r, "vote")
	if vote != "up" && vote != "down" {
		http.NotFound(w, r)
		return
	}

	if err := h.votes.SetCurrent(r.Context(), chi.URLParam(r, "id"), vote); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("<!doctype html><html><head><title>Feedback recorded</title></head><body><p>Feedback recorded.</p></body></html>"))
}
