package miniflux

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestDeleteFeed_RemovesTheFeed(t *testing.T) {
	var deleted bool
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /v1/feeds/42", func(w http.ResponseWriter, r *http.Request) {
		deleted = true
		w.WriteHeader(http.StatusNoContent)
	})

	if err := newStub(t, mux).DeleteFeed(context.Background(), 42); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Error("DELETE /v1/feeds/42 was not called")
	}
}

func TestDeleteFeed_AFeedAlreadyGoneCountsAsDeleted(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /v1/feeds/42", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error_message":"Feed not found"}`))
	})

	if err := newStub(t, mux).DeleteFeed(context.Background(), 42); err != nil {
		t.Fatalf("err = %v, want nil (delete is idempotent)", err)
	}
}

func TestDeleteFeed_ReportsOtherFailures(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /v1/feeds/42", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error_message":"database is locked"}`))
	})

	err := newStub(t, mux).DeleteFeed(context.Background(), 42)
	if err == nil || !strings.Contains(err.Error(), "database is locked") {
		t.Fatalf("err = %v", err)
	}
}
