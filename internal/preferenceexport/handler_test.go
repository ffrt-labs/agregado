package preferenceexport

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeStore struct {
	signals []Signal
	err     error
}

func (s fakeStore) Signals(context.Context) ([]Signal, error) { return s.signals, s.err }

func TestHandlerExportsOneCurrentSignalSetPerArticle(t *testing.T) {
	openedAt := time.Date(2026, 9, 15, 7, 30, 0, 0, time.UTC)
	handler := NewHandler("n8n-secret", fakeStore{signals: []Signal{{
		ArticleID:    "article-1",
		CanonicalURL: "https://example.com/article",
		Title:        "An Article",
		Tags:         []string{"technology"},
		OpenedAt:     &openedAt,
		Vote:         "up",
	}}})

	request := httptest.NewRequest(http.MethodGet, "/api/private/preferences/signals", nil)
	request.Header.Set("X-Enrichment-Secret", "n8n-secret")
	response := httptest.NewRecorder()
	handler.Handle(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	var body struct {
		Signals []Signal `json:"signals"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body.Signals) != 1 {
		t.Fatalf("signals = %+v, want one Article signal", body.Signals)
	}
	got := body.Signals[0]
	if got.ArticleID != "article-1" || got.OpenedAt == nil || !got.OpenedAt.Equal(openedAt) || got.Vote != "up" {
		t.Fatalf("signal = %+v, want one Open and the current up vote", got)
	}
}

func TestHandlerRejectsRequestsWithoutTheN8NSecret(t *testing.T) {
	handler := NewHandler("n8n-secret", fakeStore{})
	response := httptest.NewRecorder()
	handler.Handle(response, httptest.NewRequest(http.MethodGet, "/api/private/preferences/signals", nil))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}

func TestHandlerReturnsAnEmptyArrayForAColdStart(t *testing.T) {
	handler := NewHandler("n8n-secret", fakeStore{})
	request := httptest.NewRequest(http.MethodGet, "/api/private/preferences/signals", nil)
	request.Header.Set("X-Enrichment-Secret", "n8n-secret")
	response := httptest.NewRecorder()
	handler.Handle(response, request)

	if response.Code != http.StatusOK || response.Body.String() != "{\"signals\":[]}\n" {
		t.Fatalf("cold-start response = %d %s, want an empty signal array", response.Code, response.Body.String())
	}
}
