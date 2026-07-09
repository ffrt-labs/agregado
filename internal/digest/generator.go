package digest

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"log"
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
	DigestURL      string
	SourcesURL     string
	ClearedCount   int
	CandidateCount int
}

type emailGroup struct {
	Topic   string
	Summary string
	Items   []DigestItemView
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
// DigestView (so the email shows the same Position/SourceName/excerpt/reason
// as the web page) and decorates it with email-only absolute links.
// sourceNames maps source ID → display name (see BuildView).
func (g *Generator) Render(c ComputedDigest, sourceNames map[string]string) (*DigestEmail, error) {
	view := BuildView(c, sourceNames)

	groups := make([]emailGroup, len(view.Groups))
	for i, group := range view.Groups {
		groups[i] = emailGroup{Topic: group.Topic, Summary: group.Summary, Items: group.Items}
	}

	data := emailData{
		Greeting:       view.Greeting,
		DeliveryTime:   view.DeliveryTime,
		Date:           c.Date.Format("Monday, January 2, 2006"),
		Intro:          view.Intro,
		Groups:         groups,
		DigestURL:      g.baseURL,
		SourcesURL:     g.baseURL + "/sources",
		ClearedCount:   view.ClearedCount,
		CandidateCount: view.CandidateCount,
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
