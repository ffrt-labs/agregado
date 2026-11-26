package miniflux

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newStub(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Auth-Token"); got != "key" {
			t.Errorf("%s %s: X-Auth-Token = %q, want key", r.Method, r.URL.Path, got)
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	return New(server.URL+"/", "key", 0)
}

func TestFeeds_DecodesHealthFields(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/feeds", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":3,"title":"TLDR","feed_url":"https://b.example/feed/tldr.atom",
			"checked_at":"2026-11-25T10:00:00Z","parsing_error_count":2,
			"parsing_error_message":"401 Unauthorized","category":{"id":9,"title":"Newsletters"}}]`))
	})
	feeds, err := newStub(t, mux).Feeds(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Feed{
		ID: 3, Title: "TLDR", FeedURL: "https://b.example/feed/tldr.atom",
		CheckedAt:           time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC),
		ParsingErrorCount:   2,
		ParsingErrorMessage: "401 Unauthorized",
		Category:            Category{ID: 9, Title: "Newsletters"},
	}
	if len(feeds) != 1 || feeds[0] != want {
		t.Errorf("feeds = %+v, want [%+v]", feeds, want)
	}
}

func TestCategoriesAndCreateCategory(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/categories", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"id":1,"title":"All"}]`))
	})
	mux.HandleFunc("POST /v1/categories", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if body["title"] != "Newsletters" {
			t.Errorf("create category body = %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":9,"title":"Newsletters","user_id":1}`))
	})
	client := newStub(t, mux)
	categories, err := client.Categories(context.Background())
	if err != nil || len(categories) != 1 || categories[0] != (Category{ID: 1, Title: "All"}) {
		t.Fatalf("categories = %+v, %v", categories, err)
	}
	created, err := client.CreateCategory(context.Background(), "Newsletters")
	if err != nil || created != (Category{ID: 9, Title: "Newsletters"}) {
		t.Fatalf("created = %+v, %v", created, err)
	}
}

func TestCreateFeed_SendsBasicAuthAndReturnsID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/feeds", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["feed_url"] != "https://b.example/feed/syntax.atom" || body["category_id"] != float64(9) ||
			body["username"] != "syntax" || body["password"] != "s3cret" {
			t.Errorf("create feed body = %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"feed_id":262}`))
	})
	id, err := newStub(t, mux).CreateFeed(context.Background(), NewFeed{
		FeedURL: "https://b.example/feed/syntax.atom", CategoryID: 9, Username: "syntax", Password: "s3cret",
	})
	if err != nil || id != 262 {
		t.Fatalf("id = %d, err = %v", id, err)
	}
}

func TestCreateFeed_SurfacesMinifluxErrorMessage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/feeds", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error_message":"This feed already exists"}`))
	})
	_, err := newStub(t, mux).CreateFeed(context.Background(), NewFeed{FeedURL: "x", CategoryID: 1})
	if err == nil || !strings.Contains(err.Error(), "This feed already exists") {
		t.Fatalf("err = %v", err)
	}
}
