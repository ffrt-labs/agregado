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
	"strings"
	"time"

	"github.com/felipeafreitas/agregado/internal/ai"
	"github.com/felipeafreitas/agregado/internal/domain"
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
}

type DigestArticle struct {
	domain.Article
	UpToken   string
	DownToken string
}

type digestGroup struct {
	Tag      *domain.Tag
	Articles []DigestArticle
	Summary  string
}

type templateData struct {
	Date     time.Time
	Overview string
	Groups   []digestGroup
}

func NewGenerator(templateSrc string, provider ai.Provider, secret string) (*Generator, error) {
	t, err := template.New("digest").Parse(templateSrc)
	if err != nil {
		return nil, err
	}

	return &Generator{
		generator: t,
		provider:  provider,
		secret:    secret,
	}, nil
}

func NewDefaultGenerator(provider ai.Provider, secret string) (*Generator, error) {
	return NewGenerator(digestTemplate, provider, secret)
}

func (g *Generator) tokenFor(articleID, vote string) string {
	mac := hmac.New(sha256.New, []byte(g.secret))
	mac.Write([]byte(articleID + ":" + vote))
	return hex.EncodeToString(mac.Sum(nil))
}

func (g *Generator) Generate(ctx context.Context, articles []TaggedArticles) (*DigestEmail, error) {
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

	groups := make([]digestGroup, len(articles))
	for i, group := range articles {
		digestArticles := make([]DigestArticle, len(group.Articles))
		for j, a := range group.Articles {
			digestArticles[j] = DigestArticle{
				Article:   a,
				UpToken:   g.tokenFor(a.ID, "up"),
				DownToken: g.tokenFor(a.ID, "down"),
			}
		}
		groups[i] = digestGroup{
			Tag:      group.Tag,
			Articles: digestArticles,
			Summary:  group.Summary,
		}
	}

	data := templateData{
		Date:     time.Now(),
		Overview: overview,
		Groups:   groups,
	}

	var html strings.Builder
	if err := g.generator.Execute(&html, data); err != nil {
		return nil, err
	}

	var text strings.Builder
	if data.Overview != "" {
		text.WriteString(data.Overview + "\n\n")
	}
	for _, group := range data.Groups {
		tagName := "Uncategorized"
		if group.Tag != nil {
			tagName = group.Tag.Name
		}
		text.WriteString(tagName + "\n")
		for _, article := range group.Articles {
			text.WriteString(article.Title + " - " + article.ExternalURL + "\n")
		}
	}

	return &DigestEmail{
		Subject: fmt.Sprintf("Your Daily Digest - %s", data.Date.Format("January 2, 2006")),
		HTML:    html.String(),
		Text:    text.String(),
	}, nil
}
