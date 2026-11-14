package digestartifact

import (
	"bytes"
	"embed"
	htmltemplate "html/template"
	"net/url"
	"strconv"
	"strings"
	texttemplate "text/template"
	"unicode"
	"unicode/utf8"
)

// The Digest's look lives entirely in these templates, so a design round can
// change them without touching Go. Only newly computed Digests use them: a
// saved artifact keeps the HTML it was stored with.
//
//go:embed templates/digest.html templates/digest.txt
var templateFS embed.FS

var templateFuncs = map[string]any{"join": strings.Join}

var (
	htmlTemplate = htmltemplate.Must(htmltemplate.New("digest.html").Funcs(templateFuncs).ParseFS(templateFS, "templates/digest.html"))
	textTemplate = texttemplate.Must(texttemplate.New("digest.txt").Funcs(templateFuncs).ParseFS(templateFS, "templates/digest.txt"))
)

// digestView is everything the templates can show.
type digestView struct {
	Date                          string
	SelectedCount, CandidateCount int
	EmptyReason                   string
	Items                         []itemView
}

type itemView struct {
	Position                        string // "01"
	Exploration                     bool
	Host                            string // canonical URL host without www., else the author
	Kind                            string // the structured Summary's kind, capitalised; "" when absent
	Topics                          []string
	Score                           int
	ScoreDots                       string // "●●●●○"
	ScoreWord                       string // "high"
	TextMeta                        []string
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
	view := digestView{Date: a.Date.Format("Monday, January 2, 2006"), SelectedCount: a.SelectedCount, CandidateCount: a.CandidateCount}
	if len(a.Items) == 0 {
		view.EmptyReason = a.EmptyReason
	}
	for n, i := range a.Items {
		kind := summaryKind(i.Summary)
		item := itemView{
			Position: twoDigits(n + 1), Exploration: i.Exploration,
			Host: host(i.CanonicalURL, i.Author), Kind: capitalise(kind), Topics: i.Topics,
			Score: i.Score, ScoreDots: scoreDots(i.Score), ScoreWord: scoreWord(i.Score),
			Title: i.Title, Claims: claims(i.Summary),
			ReadURL: i.ReadURL, UpvoteURL: i.UpvoteURL, DownvoteURL: i.DownvoteURL,
		}
		item.TextMeta = append(item.TextMeta, i.Topics...)
		if kind != "" {
			item.TextMeta = append(item.TextMeta, kind)
		}
		if item.ScoreDots != "" {
			item.TextMeta = append(item.TextMeta, strconv.Itoa(i.Score)+"/5")
		}
		if item.Host != "" {
			item.TextMeta = append(item.TextMeta, item.Host)
		}
		view.Items = append(view.Items, item)
	}
	return view
}

func twoDigits(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// host names where an Article came from. article_index.source_id is never set
// (n8n doesn't send it), so the canonical URL's host stands in for a Source.
func host(canonicalURL, author string) string {
	if u, err := url.Parse(canonicalURL); err == nil && u.Hostname() != "" {
		return strings.TrimPrefix(u.Hostname(), "www.")
	}
	return author
}

// scoreDots is empty outside 1–5, so a missing score shows nothing.
func scoreDots(score int) string {
	if score < 1 || score > 5 {
		return ""
	}
	return strings.Repeat("●", score) + strings.Repeat("○", 5-score)
}

// scoreWord covers below-floor scores too: Exploration picks are topped up
// from Articles under the floor, so a 1 or 2 can reach the Digest.
func scoreWord(score int) string {
	switch {
	case score >= 5:
		return "top"
	case score == 4:
		return "high"
	case score == 3:
		return "mid"
	default:
		return "low"
	}
}

func capitalise(s string) string {
	r, size := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[size:]
}
