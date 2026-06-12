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
)

//go:embed templates/digest.html
var digestTemplate string

type DigestEmail struct {
	Subject string
	HTML string
	Text string
}

type Generator struct {
	generator 	*template.Template
	provider	ai.Provider
}

type templateData struct {
	Date time.Time
	Groups []TaggedArticles
}

func NewGenerator(templateSrc string, provider ai.Provider) (*Generator, error) {
	t, err := template.New("digest").Parse(templateSrc)

	if (err != nil) {
		return nil, err
	}

	return &Generator{
		generator: t,
		provider: provider,
	}, nil
}

func NewDefaultGenerator(provider ai.Provider) (*Generator, error) {
	return NewGenerator(digestTemplate, provider)
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

	data := templateData{
		Date: time.Now(),
		Groups: articles,
	}

	var html strings.Builder

	err := g.generator.Execute(&html, data)

	if (err != nil) {
		return nil, err
	}

	var text strings.Builder
	for _, group := range data.Groups {
		tagName := "Uncategorized"
		if (group.Tag != nil) {
			tagName = group.Tag.Name
		}

		text.WriteString(tagName + "\n")
		for _, article := range group.Articles {
			title := article.Title +  "-" + article.ExternalURL
			text.WriteString(title + "\n")
		}
	}

	return &DigestEmail{
		Subject: fmt.Sprintf("Your Daily Digest - %s", data.Date.Format("January 2, 2006")),
		HTML: html.String(),
		Text: text.String(),
	}, nil
}
