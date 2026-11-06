package api

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/felipeafreitas/agregado/internal/telemetry"
)

// ExplicitVoteRecorder keeps the single current strong preference signal for
// an Article. It is separate from an Open, which is weak evidence. SetCurrent
// returns when the Article was first voted on, which a changed vote keeps.
type ExplicitVoteRecorder interface {
	SetCurrent(ctx context.Context, articleID, vote string) (firstVotedAt time.Time, err error)
}

// UserVoteScore is the Langfuse score a Digest vote becomes.
const UserVoteScore = "user_vote"

// ExplicitFeedbackHandler serves the public feedback links in a Digest. The
// confirmation intentionally offers no reading surface.
type ExplicitFeedbackHandler struct {
	votes  ExplicitVoteRecorder
	scores telemetry.ScoreSink
}

func NewExplicitFeedbackHandler(votes ExplicitVoteRecorder, scores telemetry.ScoreSink) *ExplicitFeedbackHandler {
	return &ExplicitFeedbackHandler{votes: votes, scores: scores}
}

func (h *ExplicitFeedbackHandler) Handle(w http.ResponseWriter, r *http.Request) {
	vote := chi.URLParam(r, "vote")
	if vote != "up" && vote != "down" {
		http.NotFound(w, r)
		return
	}

	articleID := chi.URLParam(r, "id")
	firstVotedAt, err := h.votes.SetCurrent(r.Context(), articleID, vote)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// The first vote's time keeps the Langfuse score's timestamp stable, so a
	// changed vote overwrites the earlier user_vote instead of adding one.
	score := telemetry.CategoricalScore(telemetry.ArticleIndexKey(articleID), UserVoteScore, vote, firstVotedAt)
	telemetry.EmitScore(r.Context(), h.scores, score)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("<!doctype html><html><head><title>Feedback recorded</title></head><body><p>Feedback recorded.</p></body></html>"))
}
