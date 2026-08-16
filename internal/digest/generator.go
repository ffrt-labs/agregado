package digest

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/felipeafreitas/agregado/internal/ai"
	"github.com/felipeafreitas/agregado/internal/tmplfunc"
)

//go:embed templates/digest.html
var digestTemplate string

type DigestEmail struct {
	Subject string
	HTML    string
	Text    string
}

type Generator struct {
	generator *template.Template
	provider  ai.Provider
	baseURL   string
}

// emailData is the email template's root. It mirrors the shared DigestView but
// carries email-only extras: absolute links (the mailbox can't resolve the
// relative links the web UI uses) and its own date formatting — the email is
// a point-in-time artifact (unlike the always-live web page), so it spells out
// the full date including the year.
type emailData struct {
	Greeting       string
	DeliveryTime   string
	Date           string
	Intro          string
	Groups         []emailGroup
	ClearedCount   int
	CandidateCount int
	// LocalhostWarning is non-empty when BaseURL is a loopback origin, which
	// means the digest's own links won't resolve for a reader off the home
	// network. See NewGenerator's isLocalBaseURL for what counts as loopback.
	LocalhostWarning string
}

type emailGroup struct {
	Topic   string
	Summary string
	Items   []emailItem
}

// emailItem is a digest item with its click-through link already resolved.
// Both alternatives of the email render Link rather than each rebuilding the
// URL from the base origin and the article ID — that per-surface re-derivation
// is what let the text part drift onto the publisher URL and silently skip
// read-tracking (issue #11).
type emailItem struct {
	DigestItemView
	Link string
}

// redirectURL is the one place an article's click-through link is built, from
// the public origin (PUBLIC_BASE_URL). It always points at /r/{id}, never the
// publisher: the redirect is what marks the article read, and it is the only
// link that resolves for a newsletter, which has no web home of its own. The
// email links nothing but this and never a bare origin, since no other path is
// published through the tunnel — see docs/adr/0001.
func redirectURL(baseURL, id string) string {
	return strings.TrimSuffix(baseURL, "/") + "/r/" + id
}

type ComputedDigest struct {
	Date           time.Time
	Overview       string
	Groups         []TaggedArticles
	CandidateCount int
}

func NewGenerator(templateSrc string, provider ai.Provider, baseURL string) (*Generator, error) {
	t, err := template.New("digest").Funcs(tmplfunc.Map).Parse(templateSrc)
	if err != nil {
		return nil, err
	}

	return &Generator{
		generator: t,
		provider:  provider,
		baseURL:   baseURL,
	}, nil
}

func NewDefaultGenerator(provider ai.Provider, baseURL string) (*Generator, error) {
	return NewGenerator(digestTemplate, provider, baseURL)
}

// isLocalBaseURL reports whether baseURL points at a loopback origin
// (localhost or 127.0.0.1, regardless of port), the config's envDefault and
// the shape a forgotten PUBLIC_BASE_URL falls back to in production. An
// unparsable baseURL is not treated as local — Render still has a link to
// render, and this check only decides whether to also show a warning.
func isLocalBaseURL(baseURL string) bool {
	u, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1"
}

func (g *Generator) Compute(ctx context.Context, articles []TaggedArticles, candidateCount int) ComputedDigest {
	for i, group := range articles {
		summary, err := g.provider.Summarize(ctx, group.Articles)
		if err != nil {
			slog.Warn("summarize failed for group", "component", "digest", "err", err)
		} else {
			articles[i].Summary = summary
		}
	}

	var summaries []string
	for _, group := range articles {
		if group.Summary != "" {
			summaries = append(summaries, group.Summary)
		}
	}

	var overview string
	if len(summaries) > 0 {
		if s, err := g.provider.Digest(ctx, summaries); err == nil {
			overview = s
		} else {
			slog.Warn("digest overview failed", "component", "digest", "err", err)
		}
	}

	return ComputedDigest{
		Date:           time.Now(),
		Overview:       overview,
		Groups:         articles,
		CandidateCount: candidateCount,
	}
}

// Render turns a computed digest into a sendable email. It builds the shared
// DigestView (so the email shows the same Position/SourceName/excerpt/reason
// as the web page) and decorates it with email-only absolute links.
// sourceNames maps source ID → display name (see BuildView).
func (g *Generator) Render(c ComputedDigest, sourceNames map[string]string) (*DigestEmail, error) {
	view := BuildView(c, sourceNames)

	groups := make([]emailGroup, len(view.Groups))
	for i, group := range view.Groups {
		items := make([]emailItem, len(group.Items))
		for j, item := range group.Items {
			items[j] = emailItem{DigestItemView: item, Link: redirectURL(g.baseURL, item.ID)}
		}
		groups[i] = emailGroup{Topic: group.Topic, Summary: group.Summary, Items: items}
	}

	var localhostWarning string
	if isLocalBaseURL(g.baseURL) {
		localhostWarning = "PUBLIC_BASE_URL is unset (or still localhost) — links in this email will not work off your home network."
		slog.Warn("PUBLIC_BASE_URL is a loopback origin; rendering localhost warning banner", "component", "digest", "base_url", g.baseURL)
	}

	data := emailData{
		Greeting:         view.Greeting,
		DeliveryTime:     view.DeliveryTime,
		Date:             c.Date.Format("Monday, January 2, 2006"),
		Intro:            view.Intro,
		Groups:           groups,
		ClearedCount:     view.ClearedCount,
		CandidateCount:   view.CandidateCount,
		LocalhostWarning: localhostWarning,
	}

	var html strings.Builder
	if err := g.generator.Execute(&html, data); err != nil {
		return nil, err
	}

	// The text part renders item.Link — the same value the HTML part renders —
	// so the two alternatives cannot disagree about where a headline points.
	var text strings.Builder
	if data.Intro != "" {
		text.WriteString(data.Intro + "\n\n")
	}
	for _, group := range data.Groups {
		text.WriteString(group.Topic + "\n")
		for _, item := range group.Items {
			text.WriteString(item.Title + " - " + item.Link + "\n")
		}
		text.WriteString("\n")
	}

	return &DigestEmail{
		Subject: fmt.Sprintf("Your Daily Digest - %s", c.Date.Format("January 2, 2006")),
		HTML:    html.String(),
		Text:    text.String(),
	}, nil
}
