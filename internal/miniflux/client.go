// Package miniflux is a thin client for the Miniflux v2 REST API, scoped to
// what the Newsletters app (cmd/bridge-admin) needs: read feeds and their
// fetch health, read and create categories, and subscribe a feed. Miniflux
// stays the reader backend (ADR-0006); this package only drives its API.
//
// Auth is the X-Auth-Token header (Settings > API Keys). Errors carry
// Miniflux's {"error_message"} body.
package miniflux

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 60 * time.Second

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New takes the Miniflux origin (e.g. http://miniflux:8080). The timeout is
// generous because creating a feed makes Miniflux fetch it synchronously.
func New(baseURL, apiKey string, timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: timeout},
	}
}

type Category struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
}

// Feed is the subset of a Miniflux feed the app shows. ParsingErrorCount is
// zero when the last fetch succeeded; ParsingErrorMessage says why it didn't.
type Feed struct {
	ID                  int64     `json:"id"`
	Title               string    `json:"title"`
	FeedURL             string    `json:"feed_url"`
	CheckedAt           time.Time `json:"checked_at"`
	ParsingErrorCount   int       `json:"parsing_error_count"`
	ParsingErrorMessage string    `json:"parsing_error_message"`
	Category            Category  `json:"category"`
}

// NewFeed is a subscription request. Miniflux sends Basic auth only when both
// Username and Password are non-empty.
type NewFeed struct {
	FeedURL    string `json:"feed_url"`
	CategoryID int64  `json:"category_id"`
	Username   string `json:"username,omitempty"`
	Password   string `json:"password,omitempty"`
}

func (c *Client) Feeds(ctx context.Context) ([]Feed, error) {
	var feeds []Feed
	err := c.call(ctx, http.MethodGet, "/v1/feeds", nil, &feeds, http.StatusOK)
	return feeds, err
}

func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	var categories []Category
	err := c.call(ctx, http.MethodGet, "/v1/categories", nil, &categories, http.StatusOK)
	return categories, err
}

func (c *Client) CreateCategory(ctx context.Context, title string) (Category, error) {
	var created Category
	err := c.call(ctx, http.MethodPost, "/v1/categories", map[string]string{"title": title}, &created, http.StatusCreated)
	return created, err
}

// CreateFeed subscribes a feed and returns its id. Miniflux fetches and
// parses the feed before answering, so a feed that 401s or 404s fails here.
func (c *Client) CreateFeed(ctx context.Context, feed NewFeed) (int64, error) {
	var created struct {
		FeedID int64 `json:"feed_id"`
	}
	err := c.call(ctx, http.MethodPost, "/v1/feeds", feed, &created, http.StatusCreated)
	return created.FeedID, err
}

// FeedUpdate changes some of a feed's settings. Empty fields are left as they
// are: Rename sets Title, Reconnect sets Username and Password.
type FeedUpdate struct {
	Title    string `json:"title,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// UpdateFeed applies update to feed id. Miniflux answers 201 with the feed.
func (c *Client) UpdateFeed(ctx context.Context, id int64, update FeedUpdate) error {
	return c.call(ctx, http.MethodPut, fmt.Sprintf("/v1/feeds/%d", id), update, nil, http.StatusCreated)
}

// RefreshFeed makes Miniflux fetch feed id now; it answers once the fetch is
// done, so a feed that still fails to authenticate fails here.
func (c *Client) RefreshFeed(ctx context.Context, id int64) error {
	return c.call(ctx, http.MethodPut, fmt.Sprintf("/v1/feeds/%d/refresh", id), nil, nil, http.StatusNoContent)
}

func (c *Client) call(ctx context.Context, method, path string, in, out any, want int) error {
	var body io.Reader
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("X-Auth-Token", c.apiKey)
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("miniflux: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		return statusError(method, path, resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("miniflux: decode %s %s: %w", method, path, err)
	}
	return nil
}

func statusError(method, path string, resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	var decoded struct {
		ErrorMessage string `json:"error_message"`
	}
	detail := strings.TrimSpace(string(raw))
	if json.Unmarshal(raw, &decoded) == nil && decoded.ErrorMessage != "" {
		detail = decoded.ErrorMessage
	}
	return fmt.Errorf("miniflux: %s %s returned %s: %s", method, path, resp.Status, detail)
}
