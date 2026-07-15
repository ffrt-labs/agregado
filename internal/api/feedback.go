package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/go-chi/chi/v5"
)

type FeedbackCreator interface {
	Create(ctx context.Context, articleID, vote string) error
}

type WeightsUpserter interface {
	Upsert(ctx context.Context, topic string, delta float64) error
}

type ArticleGetter interface {
	GetById(ctx context.Context, id string) (*domain.Article, error)
}

type FeedbackHandler struct {
	feedback FeedbackCreator
	weights  WeightsUpserter
	articles ArticleGetter
}

func NewFeedbackHandler(feedback FeedbackCreator, weights WeightsUpserter, articles ArticleGetter) *FeedbackHandler {
	return &FeedbackHandler{
		feedback: feedback,
		weights:  weights,
		articles: articles,
	}
}

type feedbackPayload struct {
	Vote string `json:"vote"`
}

// Handle records a vote and nudges the voted article's tag weights. This
// replaces a GET+HMAC design (GET /api/feedback?article_id=&vote=&token=)
// that had gone unreachable: its token generator was removed in an earlier
// refactor, so no caller could ever produce a valid token, and the web
// template already POSTs to a same-origin path instead. A same-origin POST
// needs no signature — the thing HMAC was defending against (a mail client
// prefetching and auto-triggering a GET that mutates state) doesn't apply
// here; that hazard returns if this is ever wired into email links, which is
// exactly why the email digest defers feedback to the web app.
func (h *FeedbackHandler) Handle(w http.ResponseWriter, r *http.Request) {
	articleID := chi.URLParam(r, "id")

	var payload feedbackPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if payload.Vote != "up" && payload.Vote != "down" {
		writeError(w, http.StatusBadRequest, "vote must be 'up' or 'down'")
		return
	}

	ctx := r.Context()

	if err := h.feedback.Create(ctx, articleID, payload.Vote); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	article, err := h.articles.GetById(ctx, articleID)
	if err == nil && article != nil {
		delta := 0.1
		if payload.Vote == "down" {
			delta = -0.1
		}
		for _, tag := range article.Tags {
			h.weights.Upsert(ctx, tag.Slug, delta)
		}
	}

	w.WriteHeader(http.StatusNoContent)
}
