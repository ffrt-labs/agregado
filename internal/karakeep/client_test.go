package karakeep

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stub struct {
	known   map[string]string // url -> title
	created map[string]string
	posts   int
}

func newStub() *stub {
	return &stub{known: map[string]string{}, created: map[string]string{}}
}

func (s *stub) server(t *testing.T) *Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/users/me":
			json.NewEncoder(w).Encode(map[string]any{"id": "u_1"})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/bookmarks/check-url"):
			title, ok := s.known[r.URL.Query().Get("url")]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{
				"bookmark": map[string]any{"id": "bk_1", "title": title},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/bookmarks":
			s.posts++
			var body map[string]string
			raw, _ := io.ReadAll(r.Body)
			json.Unmarshal(raw, &body)
			s.created[body["url"]] = body["title"]
			s.known[body["url"]] = body["title"]
			w.WriteHeader(http.StatusCreated)
			json.NewEncoder(w).Encode(map[string]any{"id": "bk_1"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	return New(server.URL, "test-key", 0)
}

func TestCreateSavesALinkBookmark(t *testing.T) {
	backend := newStub()
	client := backend.server(t)

	if err := client.Create(context.Background(), "https://example.com/a", "A"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if backend.created["https://example.com/a"] != "A" {
		t.Errorf("Karakeep received %v", backend.created)
	}
}

func TestLookupReportsAURLKarakeepAlreadyHas(t *testing.T) {
	backend := newStub()
	backend.known["https://example.com/a"] = "A"
	client := backend.server(t)

	_, present, err := client.Lookup(context.Background(), "https://example.com/a")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !present {
		t.Error("present = false for a URL already bookmarked")
	}
	if backend.posts != 0 {
		t.Errorf("a lookup posted %d bookmarks, want 0", backend.posts)
	}
}

// Reading a transport or server error as "not bookmarked" would duplicate
// every Bookmark on the next run, so it must surface as an error instead.
func TestAFailedCheckIsNotReadAsAbsent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	_, present, err := New(server.URL, "k", 0).Lookup(context.Background(), "https://example.com/a")
	if present {
		t.Error("present = true despite the check failing")
	}
	if err == nil {
		t.Fatal("want an error when check-url fails")
	}
	if !strings.Contains(err.Error(), "check-url") {
		t.Errorf("error = %v, want it to name the failing call", err)
	}
}

func TestLookupReadsTheBookmarkBack(t *testing.T) {
	backend := newStub()
	backend.known["https://example.com/a"] = "Saved title"
	client := backend.server(t)

	found, present, err := client.Lookup(context.Background(), "https://example.com/a")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if !present || found.Title != "Saved title" {
		t.Errorf("Lookup = %+v present=%v", found, present)
	}
}

func TestLookupReportsAnAbsentBookmark(t *testing.T) {
	client := newStub().server(t)

	_, present, err := client.Lookup(context.Background(), "https://example.com/missing")
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if present {
		t.Error("present = true for a URL Karakeep does not have")
	}
}

func TestPingRejectsABadKey(t *testing.T) {
	backend := newStub()
	good := backend.server(t)
	if err := good.Ping(context.Background()); err != nil {
		t.Fatalf("Ping with a good key: %v", err)
	}

	bad := New(strings.TrimSuffix(good.baseURL, "/"), "wrong-key", 0)
	if err := bad.Ping(context.Background()); err == nil {
		t.Error("Ping accepted a bad API key — a misconfigured client would duplicate the whole Pile")
	}
}
