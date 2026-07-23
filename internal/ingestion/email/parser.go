package email

import (
	"net/mail"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/jaytaylor/html2text"
)

type Parser struct {
}

func NewParser() *Parser {
	return &Parser{}
}

// SenderInfo is the stable newsletter identity resolved from the email's
// headers, as opposed to the SMTP envelope sender (payload.From), which is an
// ESP bounce/VERP address that rotates on every send — see PRD F3.3.
type SenderInfo struct {
	// Identity is the lookup/dedup key: List-Id (RFC 2919), falling back to
	// the From-header address, falling back to the envelope sender.
	Identity string
	// Address is the human From-header email address (falls back to the
	// envelope sender if the From header is missing/unparseable).
	Address string
	// Name is the From-header display name (falls back to Address).
	Name string
}

func (p *Parser) Parse(payload Payload) (*domain.Article, SenderInfo, error) {
	title := payload.Subject

	var content string
	content, err := html2text.FromString(payload.Html)

	if content == "" || err != nil {
		content = payload.Text
	}

	sender := resolveSender(payload.Headers, payload.From)

	author := sender.Name

	return &domain.Article{
		Title: title,
		Content: &content,
		// ExternalURL is left nil: a newsletter has no web page of its own. Its
		// canonical URL (a "view in browser" link, if any) is resolved below and
		// lives in CanonicalURL. Before Phase 21 this held a 'newsletter:<uuid>'
		// sentinel only to satisfy a NOT NULL column (issue #3).
		Author: &author,
		CanonicalURL: resolveCanonicalURL(payload.Headers, payload.Html),
		// Persist the raw HTML as ingestion provenance so the extraction
		// heuristic above can be re-run against real mail later (issue #2).
		RawHTML: payload.Html,
	}, sender, nil
}

// resolveCanonicalURL derives the newsletter's real web home from the parsed
// email, mirroring resolveSender's priority chain. Priority: the Archived-At
// header (RFC 5064 — the per-message archive URL, an exact answer when the
// sender provides one and free to try since the Worker already forwards every
// header) > a "view in browser" anchor scraped from the HTML body
// (Substack/Ghost/beehiiv) > nothing (nil → /r/{id} falls back to the reader
// page). List-Archive (RFC 2369) is deliberately not tried: it points at the
// archive index, not the specific issue. See issue #2.
func resolveCanonicalURL(headers Headers, html string) *string {
	// RFC 5064: the value is a URL, conventionally wrapped in angle brackets.
	if raw := strings.TrimSpace(headers["archived-at"]); raw != "" {
		candidate := strings.TrimSpace(strings.Trim(raw, "<>"))
		if isCanonicalCandidate(candidate) {
			return &candidate
		}
	}

	if link := scrapeViewInBrowser(html); link != "" {
		return &link
	}

	return nil
}

// scrapeViewInBrowser looks for the "view this email in your browser" anchor
// that Substack/Ghost/beehiiv emit, returning the first href whose link text
// reads like a browser-view link and whose URL passes isCanonicalCandidate.
func scrapeViewInBrowser(html string) string {
	if strings.TrimSpace(html) == "" {
		return ""
	}

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		return ""
	}

	var found string
	doc.Find("a[href]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		if !isViewInBrowserText(strings.ToLower(strings.TrimSpace(s.Text()))) {
			return true // not this anchor — keep scanning
		}
		href := strings.TrimSpace(s.AttrOr("href", ""))
		if isCanonicalCandidate(href) {
			found = href
			return false // stop
		}
		return true
	})
	return found
}

// isViewInBrowserText reports whether an anchor's visible text reads like a
// "view in browser" link. Matched on the human label rather than the href so a
// tracking-wrapped URL still resolves — the label is what senders keep readable.
func isViewInBrowserText(text string) bool {
	if text == "" || strings.Contains(text, "unsubscribe") {
		return false
	}
	phrases := []string{
		"view in browser",
		"view this email",
		"view in your browser",
		"view email in browser",
		"view online",
		"view it online",
		"see it in your browser",
		"read online",
		"read in browser",
		"web version",
	}
	for _, p := range phrases {
		if strings.Contains(text, p) {
			return true
		}
	}
	return false
}

// isCanonicalCandidate reports whether a header- or HTML-sourced URL is a
// legitimate canonical link rather than the junk that shares the same email:
// tracking pixels, unsubscribe links, mailto: addresses, and social share
// buttons. Used by both branches of resolveCanonicalURL, so a rejected URL in
// either place falls through to the next step of the chain.
func isCanonicalCandidate(rawURL string) bool {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}

	// http(s) only — rejects mailto:, ftp:, tel:, and relative/empty hrefs.
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	// Tracking pixels and unsubscribe links share the newsletter's HTML. Match
	// on host/path *segments*, not a raw substring, so junk like pixel.gif and
	// /unsubscribe is rejected while a legitimate post whose slug merely contains
	// the word (e.g. /pixel-art-in-css) still resolves.
	if hasBlockedSegment(u) {
		return false
	}

	lower := strings.ToLower(rawURL)

	// Social share buttons — a tapped headline must never open a share dialog.
	shareMarkers := []string{
		"twitter.com/intent", "x.com/intent",
		"facebook.com/sharer", "linkedin.com/share",
		"t.me/share", "/share?", "/sharer",
	}
	for _, m := range shareMarkers {
		if strings.Contains(lower, m) {
			return false
		}
	}

	return true
}

// hasBlockedSegment reports whether the URL's host or path contains a discrete
// "unsubscribe" or "pixel" segment, splitting on both "/" and "." so a filename
// like pixel.gif is caught while a hyphenated slug like pixel-art is not.
func hasBlockedSegment(u *url.URL) bool {
	splitter := func(r rune) bool { return r == '/' || r == '.' }
	segments := append(
		strings.FieldsFunc(u.Host, splitter),
		strings.FieldsFunc(u.Path, splitter)...,
	)
	for _, seg := range segments {
		switch strings.ToLower(seg) {
		case "unsubscribe", "pixel":
			return true
		}
	}
	return false
}

// resolveSender derives the stable newsletter identity from the parsed email
// headers (forwarded verbatim, lowercased, by the Cloudflare Worker). Priority:
// List-Id (RFC 2919, the gold standard for mailing-list identity) > the
// From-header address (stable per newsletter) > the envelope sender (last
// resort — this is the rotating address the original bug keyed on).
func resolveSender(headers Headers, envelopeFrom string) SenderInfo {
	address := envelopeFrom
	name := envelopeFrom

	if from, err := mail.ParseAddress(headers["from"]); err == nil {
		address = from.Address
		if from.Name != "" {
			name = from.Name
		} else {
			name = from.Address
		}
	}

	identity := extractListID(headers["list-id"])
	if identity == "" {
		identity = address
	}

	return SenderInfo{Identity: identity, Address: address, Name: name}
}

// extractListID pulls the stable identifier out of a raw List-Id header value.
// Per RFC 2919 the format is `Display Phrase <list.id.value>`; the angle-bracket
// portion is the actual stable identity, the phrase is just a human label.
func extractListID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if start := strings.LastIndex(raw, "<"); start != -1 {
		if end := strings.Index(raw[start:], ">"); end != -1 {
			return raw[start+1 : start+end]
		}
	}

	return raw
}
