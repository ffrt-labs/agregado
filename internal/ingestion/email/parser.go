package email

import (
	"fmt"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/google/uuid"
	"github.com/jaytaylor/html2text"
)

type Parser struct {
}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) Parse(payload Payload) (*domain.Article, string, error) {
	title := payload.Subject

	var content string
	content, err := html2text.FromString(payload.Html)

	if content == "" || err != nil {
		content = payload.Text
	}

	return &domain.Article{
		Title: title,
		Content: &content,
		ExternalURL: fmt.Sprintf("newsletter:%s", uuid.New().String()),
		Author: &payload.From,
	}, payload.From, nil
}
