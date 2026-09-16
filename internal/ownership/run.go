package ownership

import (
	"context"
	"fmt"
	"strings"
)

// Write says whether a run may modify its destinations. It is a Runner
// concern, deliberately: the destination clients know how to look something up
// and how to write it, and nothing about migration policy. A dry run is simply
// a run where the Runner does every read and skips every write, which is what
// makes its duplicate counts measured rather than guessed.
type Write bool

const (
	DryRun Write = false
	Apply  Write = true
)

// LegacySource reads the old Agregado Postgres database. It never returns
// Article bodies: HasBody is enough to count what is being left behind.
type LegacySource interface {
	Articles(ctx context.Context) ([]LegacyArticle, error)
	SourceCount(ctx context.Context) (int, error)
}

// Bookmarker is Karakeep, the final owner of saved URLs. Lookup does double
// duty — it answers "is this already saved?" before a write and "what landed?"
// after one — so there is no second code path that could disagree with it.
type Bookmarker interface {
	Lookup(ctx context.Context, url string) (BookmarkImport, bool, error)
	Create(ctx context.Context, url, title string) error
}

// IndexImporter is the Article Index, the final owner of every Enrichment and
// preference signal.
type IndexImporter interface {
	Lookup(ctx context.Context, canonicalURL string) (IndexImport, bool, error)
	Import(ctx context.Context, record IndexImport) error
}

// ImportResult distinguishes the three things one import can do, because a
// rerun may legitimately land a vote or an Open on a record that already
// exists — a previous run that died halfway must be able to finish.
type ImportResult struct {
	Created    bool
	OpenStored bool
	VoteStored bool
}

// outcome decides what importing record over existing would achieve. Pure, and
// the only place the rule lives: the same function predicts a dry run and
// scores an apply, so the two can never disagree.
func outcome(record, existing IndexImport, exists bool) ImportResult {
	return ImportResult{
		Created:    !exists,
		OpenStored: record.OpenedAt != nil && (!exists || existing.OpenedAt == nil),
		VoteStored: record.Vote != "" && (!exists || existing.Vote == ""),
	}
}

// stored maps one import's outcome onto a class. Opens and votes have their
// own outcome because they can land on a record that already exists — the
// Enrichment classes ride on whether the row itself was created.
func stored(class DataClass, result ImportResult) bool {
	switch class {
	case Opens:
		return result.OpenStored
	case Votes:
		return result.VoteStored
	default:
		return result.Created
	}
}

// Failure records one item that did not move, with the reason verbatim.
// Classes lists every data class the item was carrying, because one failed
// import can strand a Score, a summary and a vote at once.
type Failure struct {
	Classes []DataClass
	Key     string
	Reason  string
}

// Report is what one run produced. Counts is keyed by every class in Classes,
// including the two that are deliberately excluded.
type Report struct {
	Write           Write
	Counts          map[DataClass]*Counts
	Failures        []Failure
	Samples         []Sample
	BookmarkSamples []BookmarkSample
}

type Runner struct {
	legacy     LegacySource
	bookmarker Bookmarker
	importer   IndexImporter
	sampleSize int
}

func NewRunner(legacy LegacySource, bookmarker Bookmarker, importer IndexImporter, sampleSize int) *Runner {
	return &Runner{legacy: legacy, bookmarker: bookmarker, importer: importer, sampleSize: sampleSize}
}

// Run classifies the old database and carries the result to both destinations.
// A per-item failure is counted and recorded, never fatal: one destination
// being down must not abandon the other, and a partial run is safe to repeat.
func (r *Runner) Run(ctx context.Context, write Write) (Report, error) {
	articles, err := r.legacy.Articles(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("read legacy articles: %w", err)
	}
	sources, err := r.legacy.SourceCount(ctx)
	if err != nil {
		return Report{}, fmt.Errorf("count legacy sources: %w", err)
	}

	plan := Classify(articles)
	plan.ExcludeSources(sources)
	report := Report{Write: write, Counts: plan.Counts}

	r.saveBookmarks(ctx, plan.Bookmarks, write, &report)
	r.importRecords(ctx, plan.Records, write, &report)

	if err := r.sample(ctx, plan, &report); err != nil {
		return report, err
	}
	return report, nil
}

func (r *Runner) saveBookmarks(ctx context.Context, bookmarks []BookmarkImport, write Write, report *Report) {
	for _, bookmark := range bookmarks {
		fail := func(err error) {
			report.Counts[SavedURLs].Failed++
			report.Failures = append(report.Failures, Failure{[]DataClass{SavedURLs}, bookmark.URL, err.Error()})
		}

		// Looking first is what makes a rerun safe. A lookup that errors must
		// never be read as "not there": that would duplicate the Bookmark.
		_, exists, err := r.bookmarker.Lookup(ctx, bookmark.URL)
		if err != nil {
			fail(err)
			continue
		}
		if exists {
			report.Counts[SavedURLs].Duplicate++
			continue
		}
		if write == Apply {
			if err := r.bookmarker.Create(ctx, bookmark.URL, bookmark.Title); err != nil {
				fail(err)
				continue
			}
		}
		report.Counts[SavedURLs].Destination++
	}
}

func (r *Runner) importRecords(ctx context.Context, records []IndexImport, write Write, report *Report) {
	for _, record := range records {
		classes := record.Classes()
		fail := func(err error) {
			for _, class := range classes {
				report.Counts[class].Failed++
			}
			report.Failures = append(report.Failures, Failure{classes, record.CanonicalURL, err.Error()})
		}

		existing, exists, err := r.importer.Lookup(ctx, record.CanonicalURL)
		if err != nil {
			fail(err)
			continue
		}
		result := outcome(record, existing, exists)

		if write == Apply {
			if err := r.importer.Import(ctx, record); err != nil {
				fail(err)
				continue
			}
		}
		for _, class := range classes {
			if stored(class, result) {
				report.Counts[class].Destination++
			} else {
				report.Counts[class].Duplicate++
			}
		}
	}
}

// sample reads a spread of migrated items back out of their destination, so
// the run can be checked against the old database rather than trusted. It runs
// after every write, so an applied run compares against what actually landed.
func (r *Runner) sample(ctx context.Context, plan Plan, report *Report) error {
	for _, record := range pick(plan.Records, r.sampleSize) {
		after, present, err := r.importer.Lookup(ctx, record.CanonicalURL)
		if err != nil {
			return fmt.Errorf("sample article index %s: %w", record.CanonicalURL, err)
		}
		report.Samples = append(report.Samples, Sample{Before: record, After: after, Present: present})
	}
	for _, bookmark := range pick(plan.Bookmarks, r.sampleSize) {
		after, present, err := r.bookmarker.Lookup(ctx, bookmark.URL)
		if err != nil {
			return fmt.Errorf("sample bookmark %s: %w", bookmark.URL, err)
		}
		report.BookmarkSamples = append(report.BookmarkSamples, BookmarkSample{Before: bookmark, After: after, Present: present})
	}
	return nil
}

// pick spreads the sample evenly across the batch rather than taking the first
// n — the oldest rows are the least representative of the whole history.
func pick[T any](items []T, n int) []T {
	if n <= 0 || len(items) == 0 {
		return nil
	}
	if n >= len(items) {
		return items
	}
	picked := make([]T, 0, n)
	for i := 0; i < n; i++ {
		picked = append(picked, items[i*len(items)/n])
	}
	return picked
}

func (f Failure) classList() string {
	names := make([]string, 0, len(f.Classes))
	for _, class := range f.Classes {
		names = append(names, string(class))
	}
	return strings.Join(names, ",")
}
