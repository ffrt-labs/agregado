package rss

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"
	"errors"

	"github.com/felipeafreitas/agregado/internal/broker"
	"github.com/felipeafreitas/agregado/internal/domain"
)

// log returns the poller's component-bound child of the process default
// logger. "poller" is a low-cardinality subsystem label the future collector
// can index on; source ids and feed URLs stay as per-line fields.
//
// This is a function, not a package-level var: slog.With resolves
// slog.Default() at the moment it's called, and a package var would capture
// the pre-Setup default at init time — before main installs the configured
// handler — silently pinning every poller line to the wrong destination.
func log() *slog.Logger { return slog.With("component", "poller") }

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
		// A failure here skips an entire poll cycle for every source. It used
		// to return in total silence — the feed simply stopped updating with
		// nothing anywhere to say why.
		log().Error("listing active sources failed; skipping poll cycle", "err", err)
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
	logger := log().With("source_id", source.ID, "url", *source.URL)

	feed, err := p.parser.Parse(*source.URL)
	if err != nil {
		errMsg := err.Error()
		source.LastError = &errMsg
		source.ErrorCount++
		logger.Error("parsing feed failed", "err", err, "error_count", source.ErrorCount)

		if uerr := p.sources.Update(ctx, source); uerr != nil {
			// sources.last_error is the only other record of this failure, so
			// losing the write means it survives nowhere but the line above.
			logger.Error("recording feed error on source failed", "err", uerr)
		}
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

		link := item.Link
		article := &domain.Article{
			SourceID: &id,
			ExternalURL: &link,
			Title: item.Title,
			Author: author,
			Summary: summary,
			Content: content,
			PublishedAt: item.PublishedParsed,
		}

		body, err := json.Marshal(article)
		if err != nil {
			// Warn, not error: a single malformed item is a data problem, not
			// a broken poller. The remaining items are abandoned regardless —
			// say so, because the reader will not guess it from the error.
			logger.Warn("encoding article failed; abandoning rest of feed",
				"err", err,
				"article_url", item.Link,
				"title", item.Title,
			)
			return
		}

		err = p.pub.Publish("articles.ingest", "rss", body)
		if err != nil {
			// Error: publishing fails when the broker is unreachable, which
			// stops ingestion for every source, not just this one.
			logger.Error("publishing article failed; abandoning rest of feed",
				"err", err,
				"article_url", item.Link,
				"title", item.Title,
			)
			return
		}
	}

	source.LastError = nil
	source.ErrorCount = 0
	now := time.Now()
	source.LastFetchedAt = &now
	if err := p.sources.Update(ctx, source); err != nil {
		// LastFetchedAt is the cursor that suppresses already-seen items, so a
		// dropped write here silently re-publishes the whole feed next cycle.
		logger.Error("updating source after poll failed", "err", err)
		return
	}

	// Routine chatter: below the default level so a healthy poll cycle never
	// buries the ERRORs that matter.
	logger.Debug("source polled", "items", len(feed.Items))
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
