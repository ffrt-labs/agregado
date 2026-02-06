package rss

import "github.com/mmcdole/gofeed"

type Parser struct {
	fp *gofeed.Parser
}

func NewParser() *Parser {
	return &Parser{
		fp: gofeed.NewParser(),
	}
}

func (p *Parser) Parse(feedUrl string) (*gofeed.Feed, error) {
	return p.fp.ParseURL(feedUrl)
}
