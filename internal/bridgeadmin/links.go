package bridgeadmin

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// maxLinks caps how many links one email shows; newsletters can carry
// hundreds of tracking links and the owner only needs the confirmation one.
const maxLinks = 60

// Link is one link found in a received email. Text is the anchor's visible
// text, collapsed to single spaces; empty for an image link or a bare URL.
type Link struct {
	URL  string
	Text string
}

var textURL = regexp.MustCompile(`https?://[^\s<>"'()\[\]]+`)

// ExtractLinks lists the http(s) links in an email body, in order, each URL
// once. It reads the hrefs from the HTML part, or the URLs in the text part
// when there is no HTML. The email itself is parsed, never rendered: the
// caller shows only these strings, escaped.
func ExtractLinks(html, text string) []Link {
	var links []Link
	seen := map[string]bool{}
	add := func(raw, label string) {
		raw = strings.TrimSpace(raw)
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || seen[raw] || len(links) >= maxLinks {
			return
		}
		seen[raw] = true
		links = append(links, Link{URL: raw, Text: strings.Join(strings.Fields(label), " ")})
	}

	if strings.TrimSpace(html) != "" {
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
		if err == nil {
			doc.Find("a[href]").Each(func(_ int, a *goquery.Selection) {
				href, _ := a.Attr("href")
				add(href, a.Text())
			})
			return links
		}
	}
	for _, raw := range textURL.FindAllString(text, -1) {
		add(strings.TrimRight(raw, ".,;:!?"), "")
	}
	return links
}
