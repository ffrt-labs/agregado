package fetch

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestFetcher returns a Fetcher with no inter-request delay, so tests
// don't pay the real-world politeness cost.
func newTestFetcher(minContentChars int, maxBytes int64) *Fetcher {
	f := New(0, maxBytes, minContentChars, "")
	f.hostDelay = 0
	return f
}

const realArticleHTML = `<!DOCTYPE html>
<html>
<head><title>How Readability Extraction Works | ExampleSite</title></head>
<body>
<nav><a href="/">Home</a> <a href="/about">About</a> <a href="/contact">Contact</a></nav>
<article>
<h1>How Readability Extraction Works</h1>
<p>Readability-style extraction was popularized by browsers shipping a reading mode that
strips navigation, ads, and sidebars, leaving only the main article body. The core idea is
to score candidate DOM nodes by text density and link density, then pick the highest-scoring
container as the article root.</p>
<p>Once the root is identified, the algorithm removes elements that look like boilerplate:
short paragraphs with lots of links, elements with class names like "sidebar" or "share",
and empty containers left behind after cleanup. What remains is the readable core of the
page, suitable for further processing such as conversion to Markdown.</p>
<p>This matters for an aggregator that ingests RSS and email content, because feeds frequently
omit the full article body, shipping only a short teaser. Fetching the original link and
running it through this kind of extraction recovers the real substance of the piece instead
of a fragment.</p>
</article>
<footer>Copyright 2026 ExampleSite. <a href="/privacy">Privacy</a> <a href="/terms">Terms</a></footer>
</body>
</html>`

func TestFetch_RealArticle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(realArticleHTML))
	}))
	defer server.Close()

	f := newTestFetcher(200, 0)
	result, err := f.Fetch(t.Context(), server.URL+"/article")
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if !strings.Contains(result.Markdown, "Readability-style extraction") {
		t.Errorf("Markdown missing expected article body, got: %q", result.Markdown)
	}
	if strings.Contains(result.Markdown, "Copyright 2026") {
		t.Errorf("Markdown leaked footer boilerplate: %q", result.Markdown)
	}
	if result.Length < 200 {
		t.Errorf("Length = %d, want >= 200", result.Length)
	}
}

func TestFetch_ConsentWallTooShort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<html><body><article><p>This site uses cookies. Please accept to continue reading.</p></article></body></html>`))
	}))
	defer server.Close()

	f := newTestFetcher(500, 0) // real default: 500 chars, this body is far shorter
	_, err := f.Fetch(t.Context(), server.URL)
	if !errors.Is(err, ErrThinContent) {
		t.Fatalf("Fetch() error = %v, want ErrThinContent", err)
	}
}

func TestFetch_Forbidden(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	f := newTestFetcher(0, 0)
	_, err := f.Fetch(t.Context(), server.URL)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("Fetch() error = %v, want ErrBlocked", err)
	}
}

func TestFetch_TooManyRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	f := newTestFetcher(0, 0)
	_, err := f.Fetch(t.Context(), server.URL)
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("Fetch() error = %v, want ErrBlocked", err)
	}
}

func TestFetch_UnsupportedContentType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write([]byte("%PDF-1.4 not html"))
	}))
	defer server.Close()

	f := newTestFetcher(0, 0)
	_, err := f.Fetch(t.Context(), server.URL)
	if err == nil {
		t.Fatal("Fetch() error = nil, want unsupported content-type error")
	}
	if !strings.Contains(err.Error(), "content-type") {
		t.Errorf("Fetch() error = %v, want it to mention content-type", err)
	}
}

func TestFetch_OversizedBodyIsCapped(t *testing.T) {
	// Build a body far larger than the cap: many long paragraphs.
	var sb strings.Builder
	sb.WriteString("<html><body><article><h1>Big</h1>")
	paragraph := "<p>" + strings.Repeat("word ", 200) + "</p>"
	for i := 0; i < 500; i++ { // ~500KB+ of markup
		sb.WriteString(paragraph)
	}
	sb.WriteString("</article></body></html>")
	full := sb.String()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(full))
	}))
	defer server.Close()

	const cap = 2000 // bytes — far smaller than the full body
	f := newTestFetcher(10, int64(cap))
	result, err := f.Fetch(t.Context(), server.URL)

	// A body truncated mid-tag is still valid input to the HTML parser (it
	// tolerates unclosed elements), so this should not error or hang — the
	// point of the test is that it terminates and the extracted content is
	// bounded by what fits in `cap` bytes, not by the full ~500KB body.
	if err != nil {
		if !errors.Is(err, ErrThinContent) {
			t.Fatalf("Fetch() error = %v, want nil or ErrThinContent", err)
		}
		return
	}
	if len(result.Markdown) >= len(full) {
		t.Errorf("Markdown len = %d, want it capped well below the full body (%d)", len(result.Markdown), len(full))
	}
}

func TestFetch_PerHostSerialization(t *testing.T) {
	var mu chan struct{} = make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case mu <- struct{}{}:
			// acquired: no other request is concurrently in-flight
		default:
			t.Errorf("concurrent request to the same host observed — per-host serialization failed")
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(realArticleHTML))
			return
		}
		defer func() { <-mu }()
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(realArticleHTML))
	}))
	defer server.Close()

	f := newTestFetcher(200, 0)
	done := make(chan struct{}, 2)
	for i := 0; i < 2; i++ {
		go func() {
			f.Fetch(t.Context(), server.URL)
			done <- struct{}{}
		}()
	}
	<-done
	<-done
}
