// Package fetch retrieves an article's full body from its external link and
// converts it to Markdown, so ingest-time AI calls can work from the real
// article instead of a feed's <description> teaser.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	readability "codeberg.org/readeck/go-readability/v2"
)

const (
	defaultTimeout         = 15 * time.Second
	defaultMaxBytes        = 5 * 1024 * 1024 // 5MB
	defaultMinContentChars = 500
	defaultUserAgent       = "Agregado/1.0 (+https://github.com/felipeafreitas/agregado)"
	defaultHostDelay       = 500 * time.Millisecond
	maxRedirects           = 5
)

// ErrBlocked signals the origin refused the request (403/429). Callers must
// not retry — the origin has made a deliberate access decision.
var ErrBlocked = errors.New("fetch: blocked by origin")

// ErrThinContent signals extraction technically succeeded but produced too
// little text to trust. Consent walls, SPA shells and paywalls all return
// HTTP 200, so this is the only place they can be caught.
var ErrThinContent = errors.New("fetch: extracted content too short")

// Result is a successfully fetched and extracted article.
type Result struct {
	Markdown string
	Title    string
	Byline   string
	Length   int // rune count of the extracted plain text (pre-Markdown)
}

// Fetcher retrieves and extracts article bodies. It is safe for concurrent
// use; requests to the same host are serialized with a small delay between
// them so a batch of new articles doesn't hammer one origin.
type Fetcher struct {
	client          *http.Client
	userAgent       string
	maxBytes        int64
	minContentChars int
	hostDelay       time.Duration

	hostMu    sync.Mutex
	hostLocks map[string]*sync.Mutex
}

// New builds a Fetcher. Any zero-value argument falls back to a sane default
// (mirrors the <=0-guard convention in ai.NewCloudflareProvider).
func New(timeout time.Duration, maxBytes int64, minContentChars int, userAgent string) *Fetcher {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if minContentChars <= 0 {
		minContentChars = defaultMinContentChars
	}
	if userAgent == "" {
		userAgent = defaultUserAgent
	}

	return &Fetcher{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return fmt.Errorf("stopped after %d redirects", maxRedirects)
				}
				return nil
			},
		},
		userAgent:       userAgent,
		maxBytes:        maxBytes,
		minContentChars: minContentChars,
		hostDelay:       defaultHostDelay,
		hostLocks:       make(map[string]*sync.Mutex),
	}
}

// Fetch retrieves rawURL, extracts the main article content with Readability,
// and converts it to Markdown. Returns ErrBlocked on 403/429, ErrThinContent
// when extraction yields too little text, or a wrapped error for anything
// else (bad URL, network failure, unsupported content type, malformed HTML).
func (f *Fetcher) Fetch(ctx context.Context, rawURL string) (Result, error) {
	pageURL, err := url.Parse(rawURL)
	if err != nil {
		return Result{}, fmt.Errorf("fetch: parse url: %w", err)
	}

	unlock := f.lockHost(pageURL.Host)
	defer unlock()

	body, err := f.get(ctx, rawURL)
	if err != nil {
		return Result{}, err
	}
	defer body.Close()

	article, err := readability.FromReader(body, pageURL)
	if err != nil {
		return Result{}, fmt.Errorf("fetch: extract: %w", err)
	}
	if article.Node == nil {
		return Result{}, ErrThinContent
	}

	var textBuf strings.Builder
	if err := article.RenderText(&textBuf); err != nil {
		return Result{}, fmt.Errorf("fetch: render text: %w", err)
	}
	length := len([]rune(textBuf.String()))
	if length < f.minContentChars {
		return Result{}, ErrThinContent
	}

	markdown, err := htmltomarkdown.ConvertNode(article.Node)
	if err != nil {
		return Result{}, fmt.Errorf("fetch: convert markdown: %w", err)
	}

	return Result{
		Markdown: string(markdown),
		Title:    article.Title(),
		Byline:   article.Byline(),
		Length:   length,
	}, nil
}

// get issues the request and returns a size-capped, closable body. The
// User-Agent identifies the client truthfully rather than spoofing a browser.
func (f *Fetcher) get(ctx context.Context, rawURL string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch: build request: %w", err)
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch: request failed: %w", err)
	}

	if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
		resp.Body.Close()
		return nil, ErrBlocked
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("fetch: unexpected status %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml") {
		resp.Body.Close()
		return nil, fmt.Errorf("fetch: unsupported content-type %q", ct)
	}

	return &limitedBody{Reader: io.LimitReader(resp.Body, f.maxBytes), closer: resp.Body}, nil
}

// limitedBody caps how many bytes are read from resp.Body while still
// closing the underlying connection correctly.
type limitedBody struct {
	io.Reader
	closer io.Closer
}

func (b *limitedBody) Close() error { return b.closer.Close() }

// lockHost serializes requests to the same host and returns an unlock func
// that waits hostDelay before releasing, so the next request to that host
// (if any) is naturally spaced out. Distinct hosts proceed concurrently.
func (f *Fetcher) lockHost(host string) func() {
	f.hostMu.Lock()
	l, ok := f.hostLocks[host]
	if !ok {
		l = &sync.Mutex{}
		f.hostLocks[host] = l
	}
	f.hostMu.Unlock()

	l.Lock()
	return func() {
		time.Sleep(f.hostDelay)
		l.Unlock()
	}
}
