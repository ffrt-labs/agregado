package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type explicitVoteCall struct {
	articleID string
	vote      string
}

type fakeExplicitVoteRecorder struct {
	calls []explicitVoteCall
	err   error
}

func (f *fakeExplicitVoteRecorder) SetCurrent(_ context.Context, articleID, vote string) error {
	f.calls = append(f.calls, explicitVoteCall{articleID: articleID, vote: vote})
	return f.err
}

func TestExplicitFeedbackHandler_RecordsDigestVoteAndConfirms(t *testing.T) {
	recorder := &fakeExplicitVoteRecorder{}
	handler := NewExplicitFeedbackHandler(recorder)

	for _, vote := range []string{"up", "down"} {
		t.Run(vote, func(t *testing.T) {
			router := chi.NewRouter()
			router.Get("/f/{id}/{vote}", handler.Handle)

			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/f/article-1/"+vote, nil))

			if response.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
			}
			if got := response.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
				t.Fatalf("Content-Type = %q, want text/html", got)
			}
			if !strings.Contains(response.Body.String(), "Feedback recorded") {
				t.Fatalf("confirmation = %q, want recorded confirmation", response.Body.String())
			}
		})
	}

	want := []explicitVoteCall{{articleID: "article-1", vote: "up"}, {articleID: "article-1", vote: "down"}}
	if len(recorder.calls) != len(want) {
		t.Fatalf("SetCurrent calls = %v, want %v", recorder.calls, want)
	}
	for i, call := range want {
		if recorder.calls[i] != call {
			t.Errorf("SetCurrent call %d = %v, want %v", i, recorder.calls[i], call)
		}
	}
}

func TestExplicitFeedbackHandler_RejectsUnknownDirection(t *testing.T) {
	recorder := &fakeExplicitVoteRecorder{}
	handler := NewExplicitFeedbackHandler(recorder)

	response := httptest.NewRecorder()
	handler.Handle(response, feedbackRequestWithID("article-1", "sideways"))

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if len(recorder.calls) != 0 {
		t.Fatalf("SetCurrent calls = %v, want none", recorder.calls)
	}
}

func feedbackRequestWithID(id, vote string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/f/"+id+"/"+vote, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	rctx.URLParams.Add("vote", vote)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}
