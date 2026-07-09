package digest

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"log"
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
	secret    string
	baseURL   string
}

// emailData is the email template's root. It mirrors the shared DigestView but
// carries email-only extras: absolute per-item feedback URLs and a footer
// browse link (the mailbox can't resolve the relative links the web UI uses).
type emailData struct {
	Greeting     string
	DeliveryTime string
	Date         string
	Intro        string
	Groups       []emailGroup
	BrowseURL    string
}

type emailGroup struct {
	Topic   string
	Summary string
	Items   []emailItem
}

type emailItem struct {
	DigestItemView
	UpURL   string
	DownURL string
}

type ComputedDigest struct {
	Date           time.Time
	Overview       string
	Groups         []TaggedArticles
	CandidateCount int
}

func NewGenerator(templateSrc string, provider ai.Provider, secret, baseURL string) (*Generator, error) {
	t, err := template.New("digest").Funcs(tmplfunc.Map).Parse(templateSrc)
	if err != nil {
		return nil, err
	}

	return &Generator{
		generator: t,
		provider:  provider,
		secret:    secret,
		baseURL:   baseURL,
	}, nil
}

func NewDefaultGenerator(provider ai.Provider, secret, baseURL string) (*Generator, error) {
	return NewGenerator(digestTemplate, provider, secret, baseURL)
}

func (g *Generator) tokenFor(articleID, vote string) string {
	mac := hmac.New(sha256.New, []byte(g.secret))
	mac.Write([]byte(articleID + ":" + vote))
	return hex.EncodeToString(mac.Sum(nil))
}

// feedbackURL builds the absolute, HMAC-signed feedback link that the email's
// ▲/▼ buttons point at.
func (g *Generator) feedbackURL(articleID, vote string) string {
	return fmt.Sprintf("%s/api/feedback?article_id=%s&vote=%s&token=%s",
		g.baseURL, url.QueryEscape(articleID), vote, g.tokenFor(articleID, vote))
}

func (g *Generator) Compute(ctx context.Context, articles []TaggedArticles, candidateCount int) ComputedDigest {
	for i, group := range articles {
		summary, err := g.provider.Summarize(ctx, group.Articles)
		if err != nil {
			log.Printf("summarize failed for group: %v", err)
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
			log.Printf("digest overview failed: %v", err)
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
// DigestView (so the email shows the same Position/SourceName/excerpt/dots as
// the web page), then decorates each item with absolute feedback URLs.
// sourceNames maps source ID → display name (see BuildView).
func (g *Generator) Render(c ComputedDigest, sourceNames map[string]string) (*DigestEmail, error) {
	view := BuildView(c, sourceNames)

	groups := make([]emailGroup, len(view.Groups))
	for i, group := range view.Groups {
		items := make([]emailItem, len(group.Items))
		for j, item := range group.Items {
			items[j] = emailItem{
				DigestItemView: item,
				UpURL:          g.feedbackURL(item.ID, "up"),
				DownURL:        g.feedbackURL(item.ID, "down"),
			}
		}
		groups[i] = emailGroup{Topic: group.Topic, Summary: group.Summary, Items: items}
	}

	data := emailData{
		Greeting:     view.Greeting,
		DeliveryTime: view.DeliveryTime,
		Date:         view.Date,
		Intro:        view.Intro,
		Groups:       groups,
		BrowseURL:    g.baseURL + "/articles",
	}

	var html strings.Builder
	if err := g.generator.Execute(&html, data); err != nil {
		return nil, err
	}

	var text strings.Builder
	if data.Intro != "" {
		text.WriteString(data.Intro + "\n\n")
	}
	for _, group := range data.Groups {
		text.WriteString(group.Topic + "\n")
		for _, item := range group.Items {
			text.WriteString(item.Title + " - " + item.ExternalURL + "\n")
		}
		text.WriteString("\n")
	}

	return &DigestEmail{
		Subject: fmt.Sprintf("Your Daily Digest - %s", c.Date.Format("January 2, 2006")),
		HTML:    html.String(),
		Text:    text.String(),
	}, nil
}
