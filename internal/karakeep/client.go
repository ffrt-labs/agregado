// Package karakeep is a thin client for the Bookmarker's REST API, scoped to
// what the ownership migration needs: check whether a URL is already saved,
// and save it. Nothing here reads or writes Enrichment — a Save carries a URL
// and a title and nothing else (ADR-0005).
package karakeep

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/felipeafreitas/agregado/internal/ownership"
)

const defaultTimeout = 30 * time.Second

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New takes the Karakeep origin (e.g. https://karakeep.example.com); the
// /api/v1 prefix is this package's business, not the caller's.
func New(address, apiKey string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		baseURL: strings.TrimRight(address, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: timeout},
	}
}

type checkURLResponse struct {
	Bookmark *bookmark `json:"bookmark"`
}

type bookmark struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content struct {
		URL   string `json:"url"`
		Title string `json:"title"`
	} `json:"content"`
}

// title prefers the bookmark's own title over the crawler's, matching what the
// Pile shows.
func (b bookmark) title() string {
	if strings.TrimSpace(b.Title) != "" {
		return b.Title
	}
	return b.Content.Title
}

// Save creates a link Bookmark, or reports created false when the URL is
// already in Karakeep. The existence check runs in both modes: it is what
// makes a rerun safe, and what makes a dry run's duplicate count real.
func (c *Client) Save(ctx context.Context, link, title string, write ownership.Write) (bool, error) {
	existing, err := c.find(ctx, link)
	if err != nil {
		return false, err
	}
	if existing != nil {
		return false, nil
	}
	if write == ownership.DryRun {
		return true, nil
	}

	body, err := json.Marshal(map[string]string{"type": "link", "url": link, "title": title})
	if err != nil {
		return false, err
	}
	response, err := c.do(ctx, http.MethodPost, "/api/v1/bookmarks", bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated {
		return false, statusError("create bookmark", response)
	}
	return true, nil
}

// Lookup reads a Bookmark back out of Karakeep so a migrated saved URL can be
// compared against the row it came from.
func (c *Client) Lookup(ctx context.Context, link string) (ownership.BookmarkImport, bool, error) {
	found, err := c.find(ctx, link)
	if err != nil || found == nil {
		return ownership.BookmarkImport{URL: link}, false, err
	}
	return ownership.BookmarkImport{URL: link, Title: found.title()}, true, nil
}

func (c *Client) find(ctx context.Context, link string) (*bookmark, error) {
	path := "/api/v1/bookmarks/check-url?url=" + url.QueryEscape(link)
	response, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	// Karakeep answers "not bookmarked" with 404 on this endpoint; anything
	// else non-2xx is a real error and must not be read as "not there", which
	// would silently duplicate every Bookmark on a rerun.
	if response.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if response.StatusCode != http.StatusOK {
		return nil, statusError("check-url", response)
	}

	var decoded checkURLResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("karakeep: decode check-url: %w", err)
	}
	if decoded.Bookmark == nil || decoded.Bookmark.ID == "" {
		return nil, nil
	}
	return decoded.Bookmark, nil
}

func (c *Client) do(ctx context.Context, method, path string, body io.Reader) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(request)
	if err != nil {
		return nil, fmt.Errorf("karakeep: %s %s: %w", method, path, err)
	}
	return response, nil
}

func statusError(operation string, response *http.Response) error {
	detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
	return fmt.Errorf("karakeep: %s returned %s: %s", operation, response.Status, strings.TrimSpace(string(detail)))
}
