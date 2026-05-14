package digest

import (
	_ "embed"
	"fmt"
	"html/template"
	"strings"
	"time"
)

//go:embed templates/digest.html
var digestTemplate string

type DigestEmail struct {
	Subject string
	HTML string
	Text string
}

type Generator struct {
	generator *template.Template
}

type templateData struct {
	Date time.Time
	Groups []TaggedArticles
}

func NewGenerator(templateSrc string) (*Generator, error) {
	t, err := template.New("digest").Parse(templateSrc)

	if (err != nil) {
		return nil, err
	}

	return &Generator{ generator: t }, nil
}

func NewDefaultGenerator() (*Generator, error) {
	return NewGenerator(digestTemplate)
}

func (g *Generator) Generate(articles []TaggedArticles) (*DigestEmail, error) {
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
