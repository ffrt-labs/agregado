// Package articleindex owns the content-free record that survives Miniflux's
// Article retention window. Article bodies deliberately pass through this
// package but are never written by its Store.
package articleindex

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"

	"github.com/felipeafreitas/agregado/internal/ingestion/fetch"
	"github.com/felipeafreitas/agregado/internal/telemetry"
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
	// Categorize returns the tag and whether it is one of the live allowed
	// slugs; a disallowed tag is still stored, only scored (slug_allowed).
	Categorize(ctx context.Context, title, content string) (tag string, allowed bool, err error)
	Score(ctx context.Context, title, content, preferences string) (int, error)
}

// Preferences supplies the accepted profile, placeholders already stripped
// (internal/preferences).
type Preferences interface {
	Read(ctx context.Context) (string, error)
}

type Request struct {
	MinifluxEntryID int64      `json:"entry_id"`
	SourceID        string     `json:"source_id,omitempty"`
	CanonicalURL    string     `json:"canonical_url"`
	Title           string     `json:"title"`
	Author          string     `json:"author,omitempty"`
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	BridgeContent   string     `json:"bridge_content,omitempty"`
	// FeedContent is Miniflux's own content for the entry, used only when the
	// Article cannot be fetched; the Summary is then marked [content: partial].
	FeedContent string `json:"feed_content,omitempty"`
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
	tracer      trace.Tracer
	scores      telemetry.ScoreSink
}

// Option configures an optional Service seam.
type Option func(*Service)

// WithTracerProvider records each new Article's enrichment as one Langfuse
// trace. Without it (or with nil) the Service's tracer is a no-op.
func WithTracerProvider(tp trace.TracerProvider) Option {
	return func(s *Service) {
		if tp != nil {
			s.tracer = tp.Tracer(telemetry.TracerName)
		}
	}
}

// WithScoreSink records each new Article's code checks (summary_valid,
// summary_fallback, slug_allowed) as Langfuse scores on its enrichment trace.
// Without it (or with nil) the scores are discarded.
func WithScoreSink(sink telemetry.ScoreSink) Option {
	return func(s *Service) {
		if sink != nil {
			s.scores = sink
		}
	}
}

func NewService(store Store, fetcher Fetcher, model Model, preferences Preferences, opts ...Option) *Service {
	s := &Service{store: store, fetcher: fetcher, model: model, preferences: preferences,
		tracer: noop.NewTracerProvider().Tracer(telemetry.TracerName), scores: telemetry.NopScoreSink{}}
	for _, opt := range opts {
		opt(s)
	}
	return s
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

	// The trace opens only past the idempotency boundary, so a duplicate
	// request, which makes no model calls, leaves no trace either.
	ctx, span := s.startTrace(ctx, record, request)
	defer span.End()
	checks := codeChecks{sink: s.scores, traceKey: telemetry.ArticleIndexKey(record.ID), at: time.Now()}
	record, err = s.enrich(ctx, record, request, checks)
	if err != nil {
		reason := record.FailureReason
		if reason == "" { // the record completed but could not be saved
			reason = err.Error()
		}
		telemetry.Fail(span, reason)
	} else {
		output, _ := json.Marshal(map[string]any{"summary": record.Summary, "tags": record.Tags, "score": record.Score})
		span.SetAttributes(attribute.String(telemetry.AttrObsOutput, string(output)))
	}
	return record, true, err
}

func (s *Service) startTrace(ctx context.Context, record Record, request Request) (context.Context, trace.Span) {
	input, _ := json.Marshal(map[string]any{"entry_id": request.MinifluxEntryID, "canonical_url": request.CanonicalURL, "title": request.Title})
	attrs := []attribute.KeyValue{
		telemetry.TraceMetadata("record_id").String(record.ID),
		attribute.String(telemetry.AttrObsInput, string(input)),
	}
	if request.SourceID != "" {
		attrs = append(attrs, attribute.StringSlice(telemetry.AttrTraceTags, []string{request.SourceID}))
	}
	return telemetry.StartTrace(ctx, s.tracer, "article-enrichment", telemetry.ArticleIndexKey(record.ID), attrs...)
}

// content resolves the Article body, preferring Bridge content, then a fetch,
// then the feed's own (partial) content, and records which in a fetch span.
func (s *Service) content(ctx context.Context, request Request) (content string, partial bool, err error) {
	ctx, span := s.tracer.Start(ctx, "fetch", trace.WithAttributes(attribute.String(telemetry.AttrObsType, "span")))
	defer span.End()

	source := "bridge"
	content = strings.TrimSpace(request.BridgeContent)
	var fetchErr error
	if content == "" {
		source = "fetch"
		var result fetch.Result
		if result, fetchErr = s.fetcher.Fetch(ctx, request.CanonicalURL); fetchErr == nil {
			content = strings.TrimSpace(result.Markdown)
		}
	}
	if content == "" {
		if content = strings.TrimSpace(request.FeedContent); content != "" {
			source, partial = "feed", true
			slog.Warn("fetch failed or empty, using feed content", "component", "articleindex", "url", request.CanonicalURL, "err", fetchErr)
		} else if fetchErr != nil {
			err = fetchErr
		} else {
			err = fmt.Errorf("no readable article content")
		}
	}
	span.SetAttributes(
		telemetry.ObservationMetadata("content_source").String(source),
		telemetry.ObservationMetadata("partial").Bool(partial),
	)
	if err != nil {
		telemetry.Fail(span, err.Error())
	}
	return content, partial, err
}

func (s *Service) enrich(ctx context.Context, record Record, request Request, checks codeChecks) (Record, error) {
	content, partial, err := s.content(ctx, request)
	if err != nil {
		return s.fail(ctx, record, err)
	}

	preferences, err := s.preferences.Read(ctx)
	if err != nil {
		return s.fail(ctx, record, err)
	}
	summary, valid := s.summarize(ctx, request.Title, content, partial)
	checks.emit(ctx, scoreSummaryValid, valid)
	checks.emit(ctx, scoreSummaryFallback, !valid)
	tag, allowed, err := s.model.Categorize(ctx, request.Title, content)
	if err != nil {
		return s.fail(ctx, record, err)
	}
	checks.emit(ctx, scoreSlugAllowed, allowed)
	score, err := s.model.Score(ctx, request.Title, content, preferences)
	if err != nil {
		return s.fail(ctx, record, err)
	}
	if score < 1 || score > 5 {
		return s.fail(ctx, record, fmt.Errorf("score out of range: %d", score))
	}

	record.Summary, record.Tags, record.Score, record.Status = summary, []string{tag}, score, Complete
	return record, s.store.Complete(ctx, record)
}

// summarize never fails the Article: a model error or unusable JSON degrades to
// the lead extract (ADR 0009), so selection always has input. valid reports
// whether the model's structured Summary was usable (false means fallback).
func (s *Service) summarize(ctx context.Context, title, content string, partial bool) (summary string, valid bool) {
	input := content
	if partial {
		input = partialMarker + "\n" + content
	}
	raw, err := s.model.Summarize(ctx, title, input)
	if err == nil {
		if summary, ok := normalizeSummary(raw); ok {
			if partial {
				summary = partialMarker + "\n" + summary
			}
			return summary, true
		}
		err = fmt.Errorf("invalid structured summary")
	}
	slog.Warn("summary: falling back to lead extract", "component", "articleindex", "title", title, "err", err)
	return leadExtract(title, content, partial), false
}

func (s *Service) fail(ctx context.Context, record Record, cause error) (Record, error) {
	record.Status, record.FailureReason = Failed, cause.Error()
	if err := s.store.Fail(ctx, record.ID, record.FailureReason); err != nil {
		return record, err
	}
	return record, cause
}
