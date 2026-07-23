package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/felipeafreitas/agregado/internal/ingestion/fetch"
)

// recordingFetcher counts Fetch calls so a test can assert the enrichment
// stage never HTTP-fetches a newsletter. The "fetcher never called" assertion
// is the specific silent regression Phase 21 must rule out (issue #3): the
// content-source CHECK permits 'fetched', so a mistaken fetch fails silently
// rather than erroring.
type recordingFetcher struct {
	calls  int
	result fetch.Result
}

func (f *recordingFetcher) Fetch(ctx context.Context, url string) (fetch.Result, error) {
	f.calls++
	return f.result, nil
}

func strptr(s string) *string { return &s }

// fakeSourceGetter backs resolveIsNewsletter without a database. A nil source
// with a non-nil err models a lookup failure; otherwise it returns source.
type fakeSourceGetter struct {
	source *domain.Source
	err    error
}

func (f *fakeSourceGetter) FindByID(ctx context.Context, id string) (*domain.Source, error) {
	return f.source, f.err
}

func TestResolveIsNewsletter(t *testing.T) {
	ctx := context.Background()

	t.Run("article with no source is not a newsletter", func(t *testing.T) {
		got, err := resolveIsNewsletter(ctx, &fakeSourceGetter{}, domain.Article{SourceID: nil})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Errorf("got true, want false for a source-less article")
		}
	})

	t.Run("newsletter source resolves to true", func(t *testing.T) {
		src := &fakeSourceGetter{source: &domain.Source{Type: domain.Newsletter}}
		got, err := resolveIsNewsletter(ctx, src, domain.Article{SourceID: strptr("s1")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !got {
			t.Errorf("got false, want true for a newsletter source")
		}
	})

	t.Run("rss source resolves to false", func(t *testing.T) {
		src := &fakeSourceGetter{source: &domain.Source{Type: domain.Rss}}
		got, err := resolveIsNewsletter(ctx, src, domain.Article{SourceID: strptr("s1")})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got {
			t.Errorf("got true, want false for an rss source")
		}
	})

	t.Run("source lookup error surfaces rather than defaulting to not-newsletter", func(t *testing.T) {
		src := &fakeSourceGetter{err: errors.New("db down")}
		_, err := resolveIsNewsletter(ctx, src, domain.Article{SourceID: strptr("s1")})
		if err == nil {
			t.Errorf("expected the lookup error to surface, got nil — a swallowed error would silently fetch a newsletter")
		}
	})
}

func TestResolveContent(t *testing.T) {
	ctx := context.Background()
	feedBody := "the newsletter body that arrived by email"

	t.Run("newsletter uses email text and never fetches", func(t *testing.T) {
		fetcher := &recordingFetcher{
			// A far longer fetched body: if the guard leaks, the length
			// heuristic would prefer this and the source would be "fetched".
			result: fetch.Result{Markdown: "fetched web version", Length: 100000},
		}
		article := domain.Article{
			Content:     strptr(feedBody),
			ExternalURL: nil,
		}

		text, source := resolveContent(ctx, fetcher, article, true)

		if fetcher.calls != 0 {
			t.Errorf("fetcher called %d times for a newsletter, want 0", fetcher.calls)
		}
		if source != "newsletter" {
			t.Errorf("source = %q, want %q", source, "newsletter")
		}
		if text != feedBody {
			t.Errorf("text = %q, want the email body %q", text, feedBody)
		}
	})

	t.Run("rss article fetches and prefers the longer fetched body", func(t *testing.T) {
		fetcher := &recordingFetcher{
			result: fetch.Result{Markdown: "a much longer fetched article body", Length: 100000},
		}
		article := domain.Article{
			Content:     strptr(feedBody),
			ExternalURL: strptr("https://example.com/post"),
		}

		text, source := resolveContent(ctx, fetcher, article, false)

		if fetcher.calls != 1 {
			t.Errorf("fetcher called %d times for an rss article, want 1", fetcher.calls)
		}
		if source != "fetched" {
			t.Errorf("source = %q, want %q", source, "fetched")
		}
		if text != "a much longer fetched article body" {
			t.Errorf("text = %q, want the fetched body", text)
		}
	})
}
