// Package articleindex owns the content-free record that survives Miniflux's
// Article retention window. Article bodies deliberately pass through this
// package but are never written by its Store.
package articleindex

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/felipeafreitas/agregado/internal/ingestion/fetch"
)

type Status string

const (
	Processing Status = "processing"
	Complete   Status = "complete"
	Failed     Status = "failed"
)

// Record is the durable Article Index shape. It intentionally has no content
// field: article bodies must not cross the persistence boundary.
type Record struct {
	ID              string
	MinifluxEntryID int64
	SourceID        string
	CanonicalURL    string
	Title           string
	Author          string
	PublishedAt     *time.Time
	Summary         string
	Tags            []string
	Score           int
	Status          Status
	FailureReason   string
}

type Store interface {
	// Create atomically claims a canonical URL. created false returns the
	// existing record and is the idempotency boundary before any model call.
	Create(ctx context.Context, record Record) (stored Record, created bool, err error)
	Complete(ctx context.Context, record Record) error
	Fail(ctx context.Context, id, reason string) error
}

type Fetcher interface {
	Fetch(ctx context.Context, url string) (fetch.Result, error)
}

// Model deliberately reflects the three independent cheap-tier tasks. The
// score receives PREFERENCES.md verbatim; preference semantics stay outside
// shared extraction/enrichment code (ADR-0005).
type Model interface {
	Summarize(ctx context.Context, title, content string) (string, error)
	Categorize(ctx context.Context, title, content string) (string, error)
	Score(ctx context.Context, title, content, preferences string) (int, error)
}

type Preferences interface{ Read() (string, error) }

type Request struct {
	MinifluxEntryID int64      `json:"entry_id"`
	SourceID        string     `json:"source_id,omitempty"`
	CanonicalURL    string     `json:"canonical_url"`
	Title           string     `json:"title"`
	Author          string     `json:"author,omitempty"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	BridgeContent   string     `json:"bridge_content,omitempty"`
}

func (r Request) validate() error {
	u, err := url.ParseRequestURI(r.CanonicalURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("canonical_url must be an absolute http(s) URL")
	}
	if r.MinifluxEntryID <= 0 {
		return fmt.Errorf("entry_id must be positive")
	}
	if strings.TrimSpace(r.Title) == "" {
		return fmt.Errorf("title is required")
	}
	return nil
}

type Service struct {
	store       Store
	fetcher     Fetcher
	model       Model
	preferences Preferences
}

func NewService(store Store, fetcher Fetcher, model Model, preferences Preferences) *Service {
	return &Service{store: store, fetcher: fetcher, model: model, preferences: preferences}
}

func (s *Service) Process(ctx context.Context, request Request) (Record, bool, error) {
	if err := request.validate(); err != nil {
		return Record{}, false, err
	}
	record, created, err := s.store.Create(ctx, Record{
		MinifluxEntryID: request.MinifluxEntryID, CanonicalURL: request.CanonicalURL,
		SourceID: request.SourceID, Title: request.Title, Author: request.Author, PublishedAt: request.PublishedAt,
	})
	if err != nil || !created {
		return record, created, err
	}

	content := strings.TrimSpace(request.BridgeContent)
	if content == "" {
		result, fetchErr := s.fetcher.Fetch(ctx, request.CanonicalURL)
		if fetchErr != nil {
			return s.fail(ctx, record, fetchErr)
		}
		content = strings.TrimSpace(result.Markdown)
	}
	if content == "" {
		return s.fail(ctx, record, fmt.Errorf("no readable article content"))
	}

	preferences, err := s.preferences.Read()
	if err != nil {
		return s.fail(ctx, record, err)
	}
	summary, err := s.model.Summarize(ctx, request.Title, content)
	if err != nil {
		return s.fail(ctx, record, err)
	}
	tag, err := s.model.Categorize(ctx, request.Title, content)
	if err != nil {
		return s.fail(ctx, record, err)
	}
	score, err := s.model.Score(ctx, request.Title, content, preferences)
	if err != nil {
		return s.fail(ctx, record, err)
	}
	if score < 1 || score > 5 {
		return s.fail(ctx, record, fmt.Errorf("score out of range: %d", score))
	}

	record.Summary, record.Tags, record.Score, record.Status = summary, []string{tag}, score, Complete
	if err := s.store.Complete(ctx, record); err != nil {
		return record, true, err
	}
	return record, true, nil
}

func (s *Service) fail(ctx context.Context, record Record, cause error) (Record, bool, error) {
	record.Status, record.FailureReason = Failed, cause.Error()
	if err := s.store.Fail(ctx, record.ID, record.FailureReason); err != nil {
		return record, true, err
	}
	return record, true, cause
}
