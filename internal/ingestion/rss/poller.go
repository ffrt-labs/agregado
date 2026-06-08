package rss

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"errors"

	"github.com/felipeafreitas/agregado/internal/broker"
	"github.com/felipeafreitas/agregado/internal/domain"
)

type SourceLister interface {
	ListActive(ctx context.Context) ([]domain.Source, error)
	Update(ctx context.Context, source domain.Source) error
	FindByID(ctx context.Context, id string) (*domain.Source, error)
}

type Poller struct {
	sources SourceLister
	parser *Parser
	pub *broker.Publisher
	interval time.Duration
}

func NewPoller(sources SourceLister, parser *Parser, pub *broker.Publisher, interval time.Duration) *Poller {
	return &Poller{
		sources: sources,
		parser: parser,
		pub: pub,
		interval: interval,
	}
}

func (p *Poller) Start(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	p.poll(ctx)

	for {
		select {
			case <-ticker.C:
				p.poll(ctx)
			case <-ctx.Done():
				return
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	sources, err := p.sources.ListActive(ctx)

	if err != nil {
		return
	}

	for _, source := range sources {
		if source.Type != domain.Rss {
			continue
		}

		p.pollSource(ctx, source)
	}
}

func (p *Poller) pollSource(ctx context.Context, source domain.Source) {
	feed, err := p.parser.Parse(*source.URL)
	if err != nil {
		errMsg := err.Error()
		source.LastError = &errMsg
		source.ErrorCount++
		p.sources.Update(ctx, source)

		return
	}

	for _, item := range feed.Items {
		if item.PublishedParsed != nil && source.LastFetchedAt != nil {
			if item.PublishedParsed.Before(*source.LastFetchedAt) {
				continue
			}
		}

		id := source.ID

		var authorNamesArray []string
		if len(item.Authors) > 0 {
			for _, author := range item.Authors {
				authorName := author.Name
				authorNamesArray = append(authorNamesArray, authorName)
			}
		} else if item.Author != nil && item.Author.Name != "" {
			authorNamesArray = append(authorNamesArray, item.Author.Name)
		}

		var author *string
		if len(authorNamesArray) > 0 {
			authorsString := strings.Join(authorNamesArray, ", ")
			author = &authorsString
		}

		var summary *string
	  	if item.Description != "" {
	    	summary = &item.Description
		}

	  	var content *string
		if item.Content != "" {
	    	content = &item.Content
		}

		article := &domain.Article{
			SourceID: &id,
			ExternalURL: item.Link,
			Title: item.Title,
			Author: author,
			Summary: summary,
			Content: content,
			PublishedAt: item.PublishedParsed,
		}

		body, err := json.Marshal(article)
		if err != nil {
			return
		}

		err = p.pub.Publish("articles.ingest", "rss", body)
		if err != nil {
			return
		}
	}

	source.LastError = nil
	source.ErrorCount = 0
	now := time.Now()
	source.LastFetchedAt = &now
	p.sources.Update(ctx, source)
}

func (p *Poller) RefreshSource(ctx context.Context, id string) error {
	source, err := p.sources.FindByID(ctx, id)
	if err != nil {
		return err
	}

	if source.Type != domain.Rss {
		return errors.New("only rss sources can be refreshed")
	}

	p.pollSource(ctx, *source)
	return nil
}
