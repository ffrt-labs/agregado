package email

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestParseNewsletterFixtures runs the crafted-email corpus in
// testdata/newsletters through Parse, keeping the fixtures honest against the
// same table documented in that directory's README. These are the exact
// Payloads the Worker POSTs, so live verification (issue #2) sends the same
// bytes through the real path.
func TestParseNewsletterFixtures(t *testing.T) {
	cases := []struct {
		file string
		want string // "" = expect no canonical URL (reader-page fallback)
	}{
		{"01-archived-at-header.json", "https://dispatch.example.com/p/weekly-dispatch-42"},
		{"02-view-in-browser.json", "https://overflow.example.com/p/issue-108"},
		{"03-no-web-home.json", ""},
	}

	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("testdata", "newsletters", tc.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			var payload Payload
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("unmarshal fixture: %v", err)
			}

			article, _, err := NewParser().Parse(payload)
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}

			if payload.Html != "" && article.RawHTML != payload.Html {
				t.Errorf("RawHTML not carried through for %s", tc.file)
			}

			switch {
			case tc.want == "" && article.CanonicalURL != nil:
				t.Errorf("CanonicalURL = %q, want nil", *article.CanonicalURL)
			case tc.want != "" && article.CanonicalURL == nil:
				t.Errorf("CanonicalURL = nil, want %q", tc.want)
			case tc.want != "" && *article.CanonicalURL != tc.want:
				t.Errorf("CanonicalURL = %q, want %q", *article.CanonicalURL, tc.want)
			}
		})
	}
}

// TestParseCanonicalURL exercises the extraction chain through Parse's public
// surface (the ingestion boundary), not the header lookup or scraper in
// isolation — the same way TestResolveSender tests identity resolution. Each
// case proves one branch of resolveCanonicalURL's priority chain.
func TestParseCanonicalURL(t *testing.T) {
	const viewInBrowserHTML = `<html><body>
		<a href="https://track.example.com/pixel.gif?open=1">pixel</a>
		<a href="https://newsletter.example.com/p/issue-42">View this email in your browser</a>
		<a href="https://example.com/unsubscribe?u=123">Unsubscribe</a>
	</body></html>`

	cases := []struct {
		name    string
		payload Payload
		want    string // "" means expect no canonical URL (nil)
	}{
		{
			name: "archived-at header wins even when an html link also exists",
			payload: Payload{
				Headers: Headers{"archived-at": "<https://archive.example.com/issue-42>"},
				Html:    viewInBrowserHTML,
			},
			want: "https://archive.example.com/issue-42",
		},
		{
			name: "archived-at absent falls to the view-in-browser anchor",
			payload: Payload{
				Headers: Headers{},
				Html:    viewInBrowserHTML,
			},
			want: "https://newsletter.example.com/p/issue-42",
		},
		{
			name: "neither present yields no canonical url",
			payload: Payload{
				Headers: Headers{},
				Html:    `<html><body><p>No links here.</p></body></html>`,
			},
			want: "",
		},
		{
			name: "malformed archived-at falls through to the html scrape",
			payload: Payload{
				Headers: Headers{"archived-at": "not-a-url"},
				Html:    viewInBrowserHTML,
			},
			want: "https://newsletter.example.com/p/issue-42",
		},
		{
			name: "junk links only are rejected",
			payload: Payload{
				Headers: Headers{},
				Html: `<html><body>
					<a href="https://track.example.com/pixel.gif">View in browser</a>
					<a href="https://example.com/unsubscribe">View online</a>
					<a href="mailto:hi@example.com">View this email</a>
					<a href="https://twitter.com/intent/tweet?url=x">View in your browser</a>
				</body></html>`,
			},
			want: "",
		},
		{
			name: "a legit slug that merely contains the word pixel is accepted",
			payload: Payload{
				Headers: Headers{"archived-at": "<https://blog.example.com/pixel-art-in-css>"},
				Html:    viewInBrowserHTML,
			},
			want: "https://blog.example.com/pixel-art-in-css",
		},
		{
			name: "non-http scheme is rejected",
			payload: Payload{
				Headers: Headers{"archived-at": "ftp://archive.example.com/issue-42"},
				Html:    `<html><body><a href="ftp://files.example.com/x">View in browser</a></body></html>`,
			},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewParser()
			article, _, err := p.Parse(tc.payload)
			if err != nil {
				t.Fatalf("Parse returned error: %v", err)
			}
			if tc.want == "" {
				if article.CanonicalURL != nil {
					t.Errorf("CanonicalURL = %q, want nil", *article.CanonicalURL)
				}
				return
			}
			if article.CanonicalURL == nil {
				t.Fatalf("CanonicalURL = nil, want %q", tc.want)
			}
			if *article.CanonicalURL != tc.want {
				t.Errorf("CanonicalURL = %q, want %q", *article.CanonicalURL, tc.want)
			}
		})
	}
}

// TestParsePersistsRawHTML proves Parse carries the original HTML forward on
// the Article so the worker can persist it (issue #2) rather than html2text
// discarding it at the boundary as before.
func TestParsePersistsRawHTML(t *testing.T) {
	html := `<html><body><p>Hello</p></body></html>`
	article, _, err := NewParser().Parse(Payload{Html: html})
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}
	if article.RawHTML != html {
		t.Errorf("RawHTML = %q, want %q", article.RawHTML, html)
	}
}

func TestResolveSender(t *testing.T) {
	cases := []struct {
		name         string
		headers      Headers
		envelopeFrom string
		wantIdentity string
		wantAddress  string
		wantName     string
	}{
		{
			name: "list-id preferred over from-header address",
			headers: Headers{
				"list-id": "TLDR <tldr.lists.tldrnewsletter.com>",
				"from":    "TLDR <dan@tldrnewsletter.com>",
			},
			envelopeFrom: "0100019f5b865e15-f4ae5968@dailyupdate.tldrnewsletter.com",
			wantIdentity: "tldr.lists.tldrnewsletter.com",
			wantAddress:  "dan@tldrnewsletter.com",
			wantName:     "TLDR",
		},
		{
			name: "falls back to from-header address when list-id absent",
			headers: Headers{
				"from": "Newsletters <newsletters@felipefreitas.dev>",
			},
			envelopeFrom: "bounces+27731166-8a63@em6054.thenewscc.com.br",
			wantIdentity: "newsletters@felipefreitas.dev",
			wantAddress:  "newsletters@felipefreitas.dev",
			wantName:     "Newsletters",
		},
		{
			name:         "falls back to envelope sender when headers missing",
			headers:      Headers{},
			envelopeFrom: "bounces+27731166-8a63@em6054.thenewscc.com.br",
			wantIdentity: "bounces+27731166-8a63@em6054.thenewscc.com.br",
			wantAddress:  "bounces+27731166-8a63@em6054.thenewscc.com.br",
			wantName:     "bounces+27731166-8a63@em6054.thenewscc.com.br",
		},
		{
			name: "list-id without angle brackets used as-is",
			headers: Headers{
				"list-id": "list.example.com",
			},
			envelopeFrom: "envelope@example.com",
			wantIdentity: "list.example.com",
			wantAddress:  "envelope@example.com",
			wantName:     "envelope@example.com",
		},
		{
			name: "malformed from header falls back to envelope",
			headers: Headers{
				"from": "not a valid address",
			},
			envelopeFrom: "envelope@example.com",
			wantIdentity: "envelope@example.com",
			wantAddress:  "envelope@example.com",
			wantName:     "envelope@example.com",
		},
		{
			name: "from header with no display name uses address as name",
			headers: Headers{
				"from": "dan@tldrnewsletter.com",
			},
			envelopeFrom: "0100019f5b865e15@dailyupdate.tldrnewsletter.com",
			wantIdentity: "dan@tldrnewsletter.com",
			wantAddress:  "dan@tldrnewsletter.com",
			wantName:     "dan@tldrnewsletter.com",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveSender(tc.headers, tc.envelopeFrom)
			if got.Identity != tc.wantIdentity {
				t.Errorf("Identity = %q, want %q", got.Identity, tc.wantIdentity)
			}
			if got.Address != tc.wantAddress {
				t.Errorf("Address = %q, want %q", got.Address, tc.wantAddress)
			}
			if got.Name != tc.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tc.wantName)
			}
		})
	}
}

func TestResolveSenderStableAcrossRotatingEnvelope(t *testing.T) {
	headers := Headers{
		"list-id": "TLDR <tldr.lists.tldrnewsletter.com>",
		"from":    "TLDR <dan@tldrnewsletter.com>",
	}

	envelopes := []string{
		"0100019f5b2407cf-98d50c0e-1305-4815-98ae-43a5c29eba5b-000000@dailyupdate.tldrnewsletter.com",
		"0100019f5b4de805-4d0927be-2b04-4dc2-9704-5bec308ed9a0-000000@dailyupdate.tldrnewsletter.com",
		"0100019f5b865e15-f4ae5968-8f69-4a56-98d4-8e6289fcca6f-000000@dailyupdate.tldrnewsletter.com",
	}

	var firstIdentity string
	for i, envelope := range envelopes {
		sender := resolveSender(headers, envelope)
		if i == 0 {
			firstIdentity = sender.Identity
			continue
		}
		if sender.Identity != firstIdentity {
			t.Errorf("identity changed across rotating envelope senders: %q != %q", sender.Identity, firstIdentity)
		}
	}
}
