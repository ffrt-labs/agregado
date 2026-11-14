package api

import (
	"context"
	"embed"
	"html/template"
	"log/slog"
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

// ArticleTitles names the voted Article on the confirmation page.
type ArticleTitles interface {
	Title(ctx context.Context, articleID string) (string, error)
}

//go:embed templates/vote_confirmation.html
var voteConfirmationFS embed.FS

var voteConfirmation = template.Must(template.ParseFS(voteConfirmationFS, "templates/vote_confirmation.html"))

// UserVoteScore is the Langfuse score a Digest vote becomes.
const UserVoteScore = "user_vote"

// ExplicitFeedbackHandler serves the public feedback links in a Digest. The
// confirmation intentionally offers no reading surface.
type ExplicitFeedbackHandler struct {
	votes  ExplicitVoteRecorder
	titles ArticleTitles
	scores telemetry.ScoreSink
}

func NewExplicitFeedbackHandler(votes ExplicitVoteRecorder, titles ArticleTitles, scores telemetry.ScoreSink) *ExplicitFeedbackHandler {
	return &ExplicitFeedbackHandler{votes: votes, titles: titles, scores: scores}
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

	// The vote is already stored; a missing title only shortens the page.
	title, err := h.titles.Title(r.Context(), articleID)
	if err != nil {
		slog.Warn("feedback: title lookup failed", "component", "api", "article_id", articleID, "err", err)
		title = ""
	}
	direction := map[string]string{"up": "more", "down": "less"}[vote]

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if err := voteConfirmation.Execute(w, struct{ Direction, Title string }{direction, title}); err != nil {
		slog.Error("feedback: confirmation page", "component", "api", "err", err)
	}
}
