package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The browser UI that POSTed votes to /api/articles/{id}/feedback is gone
// (#238), and the topic_weights table that endpoint nudged is dropped (#241).
// Explicit feedback lives at /f/{id}/{vote} on the Article Index instead.
func TestServerNoLongerRoutesLegacyArticleFeedback(t *testing.T) {
	s := NewServer(nil, nil, nil, nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/articles/abc/feedback", strings.NewReader(`{"vote":"up"}`))
	rec := httptest.NewRecorder()
	s.httpServer.Handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST /api/articles/{id}/feedback = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
