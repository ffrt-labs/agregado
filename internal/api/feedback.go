package api

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/felipeafreitas/agregado/internal/domain"
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
	secret   string
	feedback FeedbackCreator
	weights  WeightsUpserter
	articles ArticleGetter
}

func NewFeedbackHandler(secret string, feedback FeedbackCreator, weights WeightsUpserter, articles ArticleGetter) *FeedbackHandler {
	return &FeedbackHandler{
		secret:   secret,
		feedback: feedback,
		weights:  weights,
		articles: articles,
	}
}

func (h *FeedbackHandler) Handle(w http.ResponseWriter, r *http.Request) {
	articleID := r.URL.Query().Get("article_id")
	vote := r.URL.Query().Get("vote")
	token := r.URL.Query().Get("token")

	if vote != "up" && vote != "down" {
		writeError(w, http.StatusBadRequest, "vote must be 'up' or 'down'")
		return
	}

	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write([]byte(articleID + ":" + vote))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(token), []byte(expected)) {
		writeError(w, http.StatusForbidden, "invalid token")
		return
	}

	ctx := r.Context()

	if err := h.feedback.Create(ctx, articleID, vote); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	article, err := h.articles.GetById(ctx, articleID)
	if err == nil && article != nil {
		delta := 0.1
		if vote == "down" {
			delta = -0.1
		}
		for _, tag := range article.Tags {
			h.weights.Upsert(ctx, tag.Slug, delta)
		}
	}

	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "<html><body><h2>Thanks for your feedback!</h2></body></html>")
}
