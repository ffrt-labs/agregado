package digestartifact

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	"net/url"
	"strings"
	texttemplate "text/template"
	"time"
	"unicode"
	"unicode/utf8"
)

// The Digest's look lives entirely in these templates, so a design round can
// change them without touching Go. Only newly computed Digests use them: a
// saved artifact keeps the HTML it was stored with.
//
//go:embed templates/digest.html templates/digest.txt
var templateFS embed.FS

// templateFuncs are formatting helpers; whether and where to use them is up
// to the templates.
var templateFuncs = map[string]any{
	"join":       strings.Join,
	"capitalise": capitalise,
	"dots":       scoreDots,
	"pad2":       func(n int) string { return fmt.Sprintf("%02d", n) },
}

var (
	htmlTemplate = htmltemplate.Must(htmltemplate.New("digest.html").Funcs(templateFuncs).ParseFS(templateFS, "templates/digest.html"))
	textTemplate = texttemplate.Must(texttemplate.New("digest.txt").Funcs(templateFuncs).ParseFS(templateFS, "templates/digest.txt"))
)

// digestView is everything the templates can show. It carries data, not
// presentation: wording, order, casing and glyphs are the templates' call.
type digestView struct {
	Date                          time.Time
	SelectedCount, CandidateCount int
	EmptyReason                   string
	Items                         []itemView
}

type itemView struct {
	Position                        int // 1-based
	Exploration                     bool
	Host                            string // canonical URL host without www., else the author
	Kind                            string // the structured Summary's kind, e.g. "analysis"; "" when absent
	Topics                          []string
	Score                           int
	Title                           string
	Claims                          []string
	ReadURL, UpvoteURL, DownvoteURL string
}

// Render is the email n8n sends: its HTML and plain-text parts.
func Render(a Artifact) (string, string) {
	view := newDigestView(a)
	var html, text bytes.Buffer
	// The templates are parsed at init and the view holds only strings and
	// ints, so execution cannot fail on data; a failure here is a template bug.
	if err := htmlTemplate.Execute(&html, view); err != nil {
		panic("digestartifact: html template: " + err.Error())
	}
	if err := textTemplate.Execute(&text, view); err != nil {
		panic("digestartifact: text template: " + err.Error())
	}
	return html.String(), text.String()
}

func newDigestView(a Artifact) digestView {
	view := digestView{Date: a.Date, SelectedCount: a.SelectedCount, CandidateCount: a.CandidateCount}
	if len(a.Items) == 0 {
		view.EmptyReason = a.EmptyReason
	}
	for index, item := range a.Items {
		view.Items = append(view.Items, itemView{
			Position: index + 1, Exploration: item.Exploration,
			Host: host(item.CanonicalURL, item.Author), Kind: summaryKind(item.Summary), Topics: item.Topics,
			Score: item.Score, Title: item.Title, Claims: claims(item.Summary),
			ReadURL: item.ReadURL, UpvoteURL: item.UpvoteURL, DownvoteURL: item.DownvoteURL,
		})
	}
	return view
}

// host names where an Article came from. article_index.source_id is never set
// (n8n doesn't send it), so the canonical URL's host stands in for a Source.
func host(canonicalURL, author string) string {
	if u, err := url.Parse(canonicalURL); err == nil && u.Hostname() != "" {
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	return author
}

// scoreDots is empty outside 1–5, so a missing score shows nothing. Scores
// below the floor do occur: Exploration picks are topped up from Articles
// under it.
func scoreDots(score int) string {
	if score < 1 || score > 5 {
		return ""
	}
	return strings.Repeat("●", score) + strings.Repeat("○", 5-score)
}

func capitalise(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
