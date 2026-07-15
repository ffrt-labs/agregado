package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/go-chi/chi/v5"
)

type feedbackCall struct {
	articleID, vote string
}

type fakeFeedbackCreator struct {
	createErr error
	calls     []feedbackCall
}

func (f *fakeFeedbackCreator) Create(ctx context.Context, articleID, vote string) error {
	f.calls = append(f.calls, feedbackCall{articleID, vote})
	return f.createErr
}

type weightUpsertCall struct {
	topic string
	delta float64
}

type fakeWeightsUpserter struct {
	calls []weightUpsertCall
}

func (f *fakeWeightsUpserter) Upsert(ctx context.Context, topic string, delta float64) error {
	f.calls = append(f.calls, weightUpsertCall{topic, delta})
	return nil
}

func feedbackRequest(id, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/articles/"+id+"/feedback", strings.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestFeedbackHandler_Handle(t *testing.T) {
	taggedArticle := &domain.Article{
		ID:   "art-1",
		Tags: []domain.Tag{{Slug: "tech"}, {Slug: "science"}},
	}

	t.Run("up vote records feedback and bumps every tag's weight positively", func(t *testing.T) {
		feedback := &fakeFeedbackCreator{}
		weights := &fakeWeightsUpserter{}
		articles := &fakeArticleRepo{articles: map[string]*domain.Article{"art-1": taggedArticle}}
		handler := NewFeedbackHandler(feedback, weights, articles)

		w := httptest.NewRecorder()
		handler.Handle(w, feedbackRequest("art-1", `{"vote":"up"}`))

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
		}
		if len(feedback.calls) != 1 || feedback.calls[0] != (feedbackCall{"art-1", "up"}) {
			t.Errorf("feedback.Create calls = %v, want one call for (art-1, up)", feedback.calls)
		}
		want := []weightUpsertCall{{"tech", 0.1}, {"science", 0.1}}
		if len(weights.calls) != len(want) {
			t.Fatalf("weights.Upsert calls = %v, want %v", weights.calls, want)
		}
		for i, c := range want {
			if weights.calls[i] != c {
				t.Errorf("weights.Upsert call %d = %v, want %v", i, weights.calls[i], c)
			}
		}
	})

	t.Run("down vote bumps weight negatively", func(t *testing.T) {
		feedback := &fakeFeedbackCreator{}
		weights := &fakeWeightsUpserter{}
		articles := &fakeArticleRepo{articles: map[string]*domain.Article{"art-1": taggedArticle}}
		handler := NewFeedbackHandler(feedback, weights, articles)

		w := httptest.NewRecorder()
		handler.Handle(w, feedbackRequest("art-1", `{"vote":"down"}`))

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
		}
		for _, c := range weights.calls {
			if c.delta != -0.1 {
				t.Errorf("weights.Upsert delta = %v, want -0.1", c.delta)
			}
		}
	})

	t.Run("invalid vote value is rejected before touching any repo", func(t *testing.T) {
		feedback := &fakeFeedbackCreator{}
		weights := &fakeWeightsUpserter{}
		articles := &fakeArticleRepo{articles: map[string]*domain.Article{"art-1": taggedArticle}}
		handler := NewFeedbackHandler(feedback, weights, articles)

		w := httptest.NewRecorder()
		handler.Handle(w, feedbackRequest("art-1", `{"vote":"sideways"}`))

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
		if len(feedback.calls) != 0 {
			t.Errorf("feedback.Create should not be called, got %v", feedback.calls)
		}
	})

	t.Run("malformed JSON body is rejected", func(t *testing.T) {
		feedback := &fakeFeedbackCreator{}
		weights := &fakeWeightsUpserter{}
		articles := &fakeArticleRepo{}
		handler := NewFeedbackHandler(feedback, weights, articles)

		w := httptest.NewRecorder()
		handler.Handle(w, feedbackRequest("art-1", `not json`))

		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
		}
	})

	t.Run("feedback repo error surfaces as 500 and skips weight updates", func(t *testing.T) {
		feedback := &fakeFeedbackCreator{createErr: errors.New("db down")}
		weights := &fakeWeightsUpserter{}
		articles := &fakeArticleRepo{articles: map[string]*domain.Article{"art-1": taggedArticle}}
		handler := NewFeedbackHandler(feedback, weights, articles)

		w := httptest.NewRecorder()
		handler.Handle(w, feedbackRequest("art-1", `{"vote":"up"}`))

		if w.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusInternalServerError)
		}
		if len(weights.calls) != 0 {
			t.Errorf("weights.Upsert should not be called when Create fails, got %v", weights.calls)
		}
	})

	t.Run("untagged article still records the vote, just no weight bump", func(t *testing.T) {
		untagged := &domain.Article{ID: "art-2"}
		feedback := &fakeFeedbackCreator{}
		weights := &fakeWeightsUpserter{}
		articles := &fakeArticleRepo{articles: map[string]*domain.Article{"art-2": untagged}}
		handler := NewFeedbackHandler(feedback, weights, articles)

		w := httptest.NewRecorder()
		handler.Handle(w, feedbackRequest("art-2", `{"vote":"up"}`))

		if w.Code != http.StatusNoContent {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusNoContent)
		}
		if len(weights.calls) != 0 {
			t.Errorf("expected no weight updates for an untagged article, got %v", weights.calls)
		}
	})
}
