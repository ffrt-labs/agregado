// Package resend is a thin client for Resend's receiving API, scoped to what
// the Newsletters app (cmd/bridge-admin) needs: list recent inbound mail and
// read one email's body so its links can be shown. It never sends mail.
//
// Auth is a Bearer API key. Resend has no read-only key type, so the app's
// dedicated key must be one allowed to read received mail (ADR-0012).
package resend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultBaseURL is Resend's API origin.
const DefaultBaseURL = "https://api.resend.com"

// ListLimit is the most emails one list call returns, and the most this
// client asks for.
const ListLimit = 100

const defaultTimeout = 20 * time.Second

type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New takes Resend's origin (DefaultBaseURL outside tests).
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

// Email is a received email. List items carry no body: HTML and Text are
// filled only by ReceivedEmail.
type Email struct {
	ID        string
	To        []string
	From      string
	Subject   string
	CreatedAt time.Time
	HTML      string
	Text      string
}

type wireEmail struct {
	ID        string    `json:"id"`
	To        []string  `json:"to"`
	From      string    `json:"from"`
	Subject   string    `json:"subject"`
	CreatedAt timestamp `json:"created_at"`
	HTML      *string   `json:"html"`
	Text      *string   `json:"text"`
}

func (w wireEmail) email() Email {
	e := Email{ID: w.ID, To: w.To, From: w.From, Subject: w.Subject, CreatedAt: time.Time(w.CreatedAt)}
	if w.HTML != nil {
		e.HTML = *w.HTML
	}
	if w.Text != nil {
		e.Text = *w.Text
	}
	return e
}

// ReceivedEmails returns the latest ListLimit received emails, newest first.
// With recipients, Resend returns only mail to those addresses (its `to`
// filter matches exactly and case-sensitively).
func (c *Client) ReceivedEmails(ctx context.Context, recipients ...string) ([]Email, error) {
	query := url.Values{"limit": {fmt.Sprint(ListLimit)}}
	if len(recipients) > 0 {
		query.Set("to", strings.Join(recipients, ","))
	}
	var list struct {
		Data []wireEmail `json:"data"`
	}
	if err := c.get(ctx, "/emails/receiving?"+query.Encode(), &list); err != nil {
		return nil, err
	}
	emails := make([]Email, len(list.Data))
	for i, w := range list.Data {
		emails[i] = w.email()
	}
	return emails, nil
}

// ReceivedEmail returns one email with its body. html_format=cid keeps
// inline images as cid: references instead of inlining them as base64.
func (c *Client) ReceivedEmail(ctx context.Context, id string) (Email, error) {
	var w wireEmail
	if err := c.get(ctx, "/emails/receiving/"+url.PathEscape(id)+"?html_format=cid", &w); err != nil {
		return Email{}, err
	}
	return w.email(), nil
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+c.apiKey)
	request.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("resend: GET %s: %w", stripQuery(path), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		var decoded struct {
			Message string `json:"message"`
		}
		detail := strings.TrimSpace(string(raw))
		if json.Unmarshal(raw, &decoded) == nil && decoded.Message != "" {
			detail = decoded.Message
		}
		return fmt.Errorf("resend: GET %s returned %s: %s", stripQuery(path), resp.Status, detail)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("resend: decode GET %s: %w", stripQuery(path), err)
	}
	return nil
}

func stripQuery(path string) string {
	p, _, _ := strings.Cut(path, "?")
	return p
}

// timestamp accepts RFC 3339 and the Postgres-style form ("2006-01-02
// 15:04:05.999999+00") Resend has used for created_at. An unparseable value
// is left zero rather than failing the whole list.
type timestamp time.Time

var timestampLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999Z07",
	"2006-01-02T15:04:05.999999999Z07",
}

func (t *timestamp) UnmarshalJSON(raw []byte) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil || s == "" {
		return nil
	}
	for _, layout := range timestampLayouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			*t = timestamp(parsed)
			return nil
		}
	}
	return nil
}
