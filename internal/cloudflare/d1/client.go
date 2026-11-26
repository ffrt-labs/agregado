// Package d1 is a thin client for Cloudflare D1's REST query endpoint. It
// knows how to send one SQL statement with positional params and decode the
// rows; what the tables mean is the caller's business.
//
// Endpoint: POST {base}/accounts/{account}/d1/database/{db}/query with
// {"sql", "params"}. The token needs D1 Edit for writes.
package d1

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

// DefaultBaseURL is the Cloudflare API v4 root.
const DefaultBaseURL = "https://api.cloudflare.com/client/v4"

const defaultTimeout = 30 * time.Second

type Options struct {
	BaseURL    string // defaults to DefaultBaseURL
	AccountID  string
	DatabaseID string
	APIToken   string
	Timeout    time.Duration
}

type Client struct {
	endpoint string
	token    string
	http     *http.Client
}

func New(o Options) *Client {
	base := o.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Client{
		endpoint: strings.TrimRight(base, "/") + "/accounts/" + o.AccountID + "/d1/database/" + o.DatabaseID + "/query",
		token:    o.APIToken,
		http:     &http.Client{Timeout: timeout},
	}
}

// Meta is the part of D1's per-statement meta callers act on.
type Meta struct {
	Changes  int64 `json:"changes"`
	RowsRead int64 `json:"rows_read"`
}

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	Success bool       `json:"success"`
	Errors  []apiError `json:"errors"`
	Result  []struct {
		Success bool            `json:"success"`
		Results json.RawMessage `json:"results"`
		Meta    Meta            `json:"meta"`
	} `json:"result"`
}

// Query runs one statement and decodes its rows into out (a pointer to a
// slice of structs with json tags matching the column names). out may be nil
// for statements whose rows don't matter.
func (c *Client) Query(ctx context.Context, out any, sql string, params ...any) (Meta, error) {
	if params == nil {
		params = []any{}
	}
	body, err := json.Marshal(map[string]any{"sql": sql, "params": params})
	if err != nil {
		return Meta{}, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return Meta{}, err
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	request.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(request)
	if err != nil {
		return Meta{}, fmt.Errorf("d1: query: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return Meta{}, fmt.Errorf("d1: read response: %w", err)
	}

	var decoded response
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return Meta{}, fmt.Errorf("d1: query returned %s: %s", resp.Status, snippet(raw))
	}
	if resp.StatusCode/100 != 2 || !decoded.Success || len(decoded.Result) == 0 || !decoded.Result[0].Success {
		return Meta{}, fmt.Errorf("d1: query returned %s: %s", resp.Status, describe(decoded.Errors, raw))
	}

	first := decoded.Result[0]
	if out != nil && len(first.Results) > 0 {
		if err := json.Unmarshal(first.Results, out); err != nil {
			return Meta{}, fmt.Errorf("d1: decode rows: %w", err)
		}
	}
	return first.Meta, nil
}

// Exec runs a statement whose rows don't matter (INSERT, UPDATE, DELETE).
func (c *Client) Exec(ctx context.Context, sql string, params ...any) (Meta, error) {
	return c.Query(ctx, nil, sql, params...)
}

func describe(errs []apiError, raw []byte) string {
	if len(errs) == 0 {
		return snippet(raw)
	}
	messages := make([]string, len(errs))
	for i, e := range errs {
		messages[i] = fmt.Sprintf("%s (code %d)", e.Message, e.Code)
	}
	return strings.Join(messages, "; ")
}

func snippet(raw []byte) string {
	if len(raw) > 512 {
		raw = raw[:512]
	}
	return strings.TrimSpace(string(raw))
}
