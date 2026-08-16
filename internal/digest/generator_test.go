package digest

import (
	"strings"
	"testing"
	"time"

	"github.com/felipeafreitas/agregado/internal/domain"
)

func strptr(s string) *string { return &s }

// renderFixture renders a two-item digest: one RSS article with a real web
// home, one newsletter with none (ExternalURL nil since Phase 21).
func renderFixture(t *testing.T, baseURL string) *DigestEmail {
	t.Helper()

	g, err := NewDefaultGenerator(nil, baseURL)
	if err != nil {
		t.Fatalf("NewDefaultGenerator: %v", err)
	}

	computed := ComputedDigest{
		Date:     time.Now(),
		Overview: "Two things happened.",
		Groups: []TaggedArticles{{
			Tag: &domain.Tag{Name: "Engineering"},
			Articles: []domain.Article{
				{
					ID:          "11111111-1111-1111-1111-111111111111",
					Title:       "An RSS article",
					ExternalURL: strptr("https://publisher.example.com/post"),
				},
				{
					ID:    "22222222-2222-2222-2222-222222222222",
					Title: "A newsletter issue",
				},
			},
		}},
	}

	email, err := g.Render(computed, nil)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return email
}

// TestRender_TextPartLinksThroughRedirect pins the whole point of issue #11:
// the text/plain alternative must link /r/{id} like the HTML part does, not
// the publisher URL. /r/{id} is what calls MarkRead, and it is what resolves
// an article with no web home to the in-app reader — a text part that links
// ExternalURL directly drops the read signal and emits a dangling link for
// newsletters, which have no ExternalURL at all.
func TestRender_TextPartLinksThroughRedirect(t *testing.T) {
	const baseURL = "https://agregado.example.com"
	email := renderFixture(t, baseURL)

	for _, id := range []string{
		"11111111-1111-1111-1111-111111111111",
		"22222222-2222-2222-2222-222222222222",
	} {
		want := baseURL + "/r/" + id
		if !strings.Contains(email.Text, want) {
			t.Errorf("text part missing redirect link %q:\n%s", want, email.Text)
		}
	}

	// Neither alternative may reach the publisher directly — a link added
	// alongside /r/{id} would bypass MarkRead just as a substituted one does,
	// and target equality alone would not catch it.
	for _, part := range []struct{ name, body string }{
		{"text", email.Text},
		{"HTML", email.HTML},
	} {
		if strings.Contains(part.body, "https://publisher.example.com/post") {
			t.Errorf("%s part links the publisher directly, bypassing /r/{id}:\n%s", part.name, part.body)
		}
	}
}

// TestRender_AlternativesLinkTheSameTargets is the compensating control for
// the divergence that caused issue #11: the two alternatives of a multipart
// email are only worth sending if something enforces that they agree. Extract
// every /r/{id} target from each part and require the same targets in the
// same order — the parts render the same items in the same sequence, so an
// order difference is itself a divergence.
func TestRender_AlternativesLinkTheSameTargets(t *testing.T) {
	const baseURL = "https://agregado.example.com"
	email := renderFixture(t, baseURL)

	html := redirectTargets(email.HTML, baseURL)
	text := redirectTargets(email.Text, baseURL)

	if len(html) == 0 {
		t.Fatalf("HTML part linked no articles; fixture is wrong:\n%s", email.HTML)
	}
	if len(html) != len(text) {
		t.Fatalf("HTML linked %d article(s), text linked %d\nHTML: %v\ntext: %v", len(html), len(text), html, text)
	}
	for i := range html {
		if html[i] != text[i] {
			t.Errorf("alternative %d links diverge: HTML %q, text %q", i, html[i], text[i])
		}
	}
}

// TestRedirectURL covers the one place a click-through link is built, including
// the trailing-slash origin that would otherwise yield a doubled "//r/".
func TestRedirectURL(t *testing.T) {
	tests := []struct {
		baseURL string
		want    string
	}{
		{"https://agregado.example.com", "https://agregado.example.com/r/abc"},
		{"https://agregado.example.com/", "https://agregado.example.com/r/abc"},
		{"http://localhost:8080", "http://localhost:8080/r/abc"},
	}

	for _, tt := range tests {
		if got := redirectURL(tt.baseURL, "abc"); got != tt.want {
			t.Errorf("redirectURL(%q, \"abc\") = %q, want %q", tt.baseURL, got, tt.want)
		}
	}
}

// redirectTargets returns every baseURL + "/r/<id>" occurrence in s, in order.
func redirectTargets(s, baseURL string) []string {
	prefix := baseURL + "/r/"
	var found []string
	for {
		i := strings.Index(s, prefix)
		if i < 0 {
			return found
		}
		rest := s[i+len(prefix):]
		end := strings.IndexAny(rest, "\"' \t\n<>")
		if end < 0 {
			end = len(rest)
		}
		found = append(found, prefix+rest[:end])
		s = rest[end:]
	}
}

// TestRender_LocalhostBanner asserts on the generator's public rendered
// output: a digest built with a loopback base URL carries a warning banner,
// one built with a real origin does not. This is the compensating control
// for PUBLIC_BASE_URL silently falling back to its dev default in
// production (see issue #1) — untestable at the config/deploy layer, so the
// banner is what actually gets exercised.
func TestRender_LocalhostBanner(t *testing.T) {
	tests := []struct {
		name       string
		baseURL    string
		wantBanner bool
	}{
		{"dev default", "http://localhost:8080", true},
		{"loopback IP", "http://127.0.0.1:8080", true},
		{"localhost non-default port", "http://localhost:3000", true},
		{"real https origin", "https://agregado.example.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := NewDefaultGenerator(nil, tt.baseURL)
			if err != nil {
				t.Fatalf("NewDefaultGenerator: %v", err)
			}

			computed := ComputedDigest{Date: time.Now()}
			email, err := g.Render(computed, nil)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}

			gotBanner := strings.Contains(email.HTML, "PUBLIC_BASE_URL")
			if gotBanner != tt.wantBanner {
				t.Errorf("banner present = %v, want %v (baseURL %q)", gotBanner, tt.wantBanner, tt.baseURL)
			}
		})
	}
}
