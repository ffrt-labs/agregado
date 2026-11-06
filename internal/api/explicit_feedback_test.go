package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/felipeafreitas/agregado/internal/telemetry"
)

type explicitVoteCall struct {
	articleID string
	vote      string
}

type fakeExplicitVoteRecorder struct {
	calls     []explicitVoteCall
	firstVote time.Time
	err       error
}

func (f *fakeExplicitVoteRecorder) SetCurrent(_ context.Context, articleID, vote string) (time.Time, error) {
	f.calls = append(f.calls, explicitVoteCall{articleID: articleID, vote: vote})
	return f.firstVote, f.err
}

// fakeScoreSink records Langfuse score events; err makes it fail like a full
// queue or a misconfigured sink.
type fakeScoreSink struct {
	events []telemetry.ScoreEvent
	err    error
}

func (f *fakeScoreSink) Emit(_ context.Context, event telemetry.ScoreEvent) error {
	f.events = append(f.events, event)
	return f.err
}

const confirmationBody = "<!doctype html><html><head><title>Feedback recorded</title></head><body><p>Feedback recorded.</p></body></html>"

func TestExplicitFeedbackHandler_RecordsDigestVoteAndConfirms(t *testing.T) {
	recorder := &fakeExplicitVoteRecorder{}
	handler := NewExplicitFeedbackHandler(recorder, &fakeScoreSink{})

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
	scores := &fakeScoreSink{}
	handler := NewExplicitFeedbackHandler(recorder, scores)

	response := httptest.NewRecorder()
	handler.Handle(response, feedbackRequestWithID("article-1", "sideways"))

	if response.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if len(recorder.calls) != 0 {
		t.Fatalf("SetCurrent calls = %v, want none", recorder.calls)
	}
	if len(scores.events) != 0 {
		t.Fatalf("score events = %v, want none", scores.events)
	}
}

// A vote becomes a categorical user_vote Langfuse score on the Article's
// enrichment trace, stamped with the first vote's time so that a changed vote
// overwrites the same score in Langfuse instead of adding a second one.
func TestExplicitFeedbackHandler_EmitsUserVoteScoreOnArticleTrace(t *testing.T) {
	firstVote := time.Date(2026, 10, 30, 7, 15, 0, 0, time.UTC)
	recorder := &fakeExplicitVoteRecorder{firstVote: firstVote}
	scores := &fakeScoreSink{}
	handler := NewExplicitFeedbackHandler(recorder, scores)

	for _, vote := range []string{"up", "down"} {
		handler.Handle(httptest.NewRecorder(), feedbackRequestWithID("index-1", vote))
	}

	want := []telemetry.ScoreEvent{
		{TraceKey: "article-index:index-1", Name: "user_vote", DataType: telemetry.Categorical, StringValue: "up", Timestamp: firstVote},
		{TraceKey: "article-index:index-1", Name: "user_vote", DataType: telemetry.Categorical, StringValue: "down", Timestamp: firstVote},
	}
	if len(scores.events) != len(want) {
		t.Fatalf("score events = %+v, want %+v", scores.events, want)
	}
	for i := range want {
		if scores.events[i] != want[i] {
			t.Errorf("score event %d = %+v, want %+v", i, scores.events[i], want[i])
		}
	}
}

func TestExplicitFeedbackHandler_SinkFailureDoesNotChangeConfirmation(t *testing.T) {
	handler := NewExplicitFeedbackHandler(&fakeExplicitVoteRecorder{}, &fakeScoreSink{err: errors.New("queue full")})

	response := httptest.NewRecorder()
	handler.Handle(response, feedbackRequestWithID("index-1", "up"))

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Body.String(); got != confirmationBody {
		t.Fatalf("body = %q, want %q", got, confirmationBody)
	}
}

func TestExplicitFeedbackHandler_NoScoreWhenVoteNotStored(t *testing.T) {
	scores := &fakeScoreSink{}
	handler := NewExplicitFeedbackHandler(&fakeExplicitVoteRecorder{err: errors.New("db down")}, scores)

	response := httptest.NewRecorder()
	handler.Handle(response, feedbackRequestWithID("index-1", "up"))

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
	if len(scores.events) != 0 {
		t.Fatalf("score events = %v, want none", scores.events)
	}
}

func feedbackRequestWithID(id, vote string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/f/"+id+"/"+vote, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	rctx.URLParams.Add("vote", vote)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}
