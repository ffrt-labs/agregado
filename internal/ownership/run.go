package ownership

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Write says whether a run may modify its destinations. A dry run still
// *reads* both of them, so the duplicate counts it reports are measured rather
// than guessed — the whole point of running one before the real thing.
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

// Bookmarker is Karakeep, the final owner of saved URLs.
type Bookmarker interface {
	// Save reports created false when the URL is already bookmarked, which is
	// what makes a rerun safe.
	Save(ctx context.Context, url, title string, write Write) (created bool, err error)
	Lookup(ctx context.Context, url string) (BookmarkImport, bool, error)
}

// ImportResult distinguishes the three things one Article Index import can do,
// because a rerun may legitimately land a vote or an Open on a record that
// already exists.
type ImportResult struct {
	Created    bool
	OpenStored bool
	VoteStored bool
}

// IndexImporter is the Article Index, the final owner of every Enrichment and
// preference signal.
type IndexImporter interface {
	Import(ctx context.Context, record IndexImport, write Write) (ImportResult, error)
	Lookup(ctx context.Context, canonicalURL string) (IndexImport, bool, error)
}

// Failure records one item that did not move, with the reason verbatim.
// Classes lists every data class the item was carrying, because one failed
// import can strand a Score, a summary and a vote at once.
type Failure struct {
	Classes []DataClass
	Key     string
	Reason  string
}

func (f Failure) classList() string {
	names := make([]string, 0, len(f.Classes))
	for _, class := range f.Classes {
		names = append(names, string(class))
	}
	return strings.Join(names, ",")
}

// Sample is one representative record compared before (as classified from the
// old database) and after (as read back from the destination).
type Sample struct {
	Before  IndexImport
	After   IndexImport
	Present bool
}

// BookmarkSample is the same comparison for a migrated saved URL.
type BookmarkSample struct {
	Before  BookmarkImport
	After   BookmarkImport
	Present bool
}

func (s Sample) Differences() []string {
	if !s.Present {
		return []string{"absent from the Article Index"}
	}
	var differences []string
	compare := func(field, before, after string) {
		if before != after {
			differences = append(differences, fmt.Sprintf("%s: %q -> %q", field, before, after))
		}
	}
	compare("title", s.Before.Title, s.After.Title)
	compare("author", s.Before.Author, s.After.Author)
	compare("summary", s.Before.Summary, s.After.Summary)
	compare("tags", strings.Join(s.Before.Tags, ","), strings.Join(s.After.Tags, ","))
	compare("vote", s.Before.Vote, s.After.Vote)
	compare("score", fmt.Sprint(s.Before.Score), fmt.Sprint(s.After.Score))
	compare("opened_at", formatTime(s.Before.OpenedAt), formatTime(s.After.OpenedAt))
	compare("published_at", formatTime(s.Before.PublishedAt), formatTime(s.After.PublishedAt))
	return differences
}

func (s Sample) Matches() bool { return len(s.Differences()) == 0 }

func (s BookmarkSample) Differences() []string {
	if !s.Present {
		return []string{"absent from Karakeep"}
	}
	if s.Before.Title != s.After.Title {
		return []string{fmt.Sprintf("title: %q -> %q", s.Before.Title, s.After.Title)}
	}
	return nil
}

func (s BookmarkSample) Matches() bool { return len(s.Differences()) == 0 }

func formatTime(t *time.Time) string {
	if t == nil {
		return "-"
	}
	return t.UTC().Format(time.RFC3339)
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

	for _, bookmark := range plan.Bookmarks {
		created, err := r.bookmarker.Save(ctx, bookmark.URL, bookmark.Title, write)
		switch {
		case err != nil:
			report.Counts[SavedURLs].Failed++
			report.Failures = append(report.Failures, Failure{[]DataClass{SavedURLs}, bookmark.URL, err.Error()})
		case created:
			report.Counts[SavedURLs].Destination++
		default:
			report.Counts[SavedURLs].Duplicate++
		}
	}

	for _, record := range plan.Records {
		classes := recordClasses(record)
		result, importErr := r.importer.Import(ctx, record, write)
		if importErr != nil {
			for _, class := range classes {
				report.Counts[class].Failed++
			}
			report.Failures = append(report.Failures, Failure{classes, record.CanonicalURL, importErr.Error()})
			continue
		}
		for _, class := range classes {
			if stored(class, result) {
				report.Counts[class].Destination++
			} else {
				report.Counts[class].Duplicate++
			}
		}
	}

	if err := r.sample(ctx, plan, &report); err != nil {
		return report, err
	}
	return report, nil
}

// recordClasses reports which classes a merged record actually carries, so an
// import only ever moves the counters for data that was really there.
func recordClasses(record IndexImport) []DataClass {
	var classes []DataClass
	if record.Score > 0 {
		classes = append(classes, Scores)
	}
	if len(record.Tags) > 0 {
		classes = append(classes, Tags)
	}
	if strings.TrimSpace(record.Summary) != "" {
		classes = append(classes, Summaries)
	}
	if record.OpenedAt != nil {
		classes = append(classes, Opens)
	}
	if record.Vote != "" {
		classes = append(classes, Votes)
	}
	return classes
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

// sample reads a spread of migrated items back out of their destination, so
// the run can be checked against the old database rather than trusted.
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

// Render writes the report as the plain text the operator reads before
// deciding to apply.
func (r Report) Render() string {
	var out strings.Builder
	if r.Write == DryRun {
		out.WriteString("DRY RUN — nothing was written to Karakeep or the Article Index.\n")
		out.WriteString("The destination column is what an apply would create.\n\n")
	} else {
		out.WriteString("APPLIED — Karakeep and the Article Index were written to.\n\n")
	}

	fmt.Fprintf(&out, "%-15s %-14s %7s %7s %7s %7s %7s %7s\n",
		"CLASS", "DESTINATION", "SOURCE", "TRANSF", "DEST", "SKIP", "DUP", "FAIL")
	for _, class := range Classes {
		counts := r.Counts[class]
		if counts == nil {
			counts = &Counts{}
		}
		fmt.Fprintf(&out, "%-15s %-14s %7d %7d %7d %7d %7d %7d\n",
			class, class.Destination(), counts.Source, counts.Transformed,
			counts.Destination, counts.Skipped, counts.Duplicate, counts.Failed)
	}

	if len(r.Failures) > 0 {
		out.WriteString("\nFAILURES\n")
		failures := append([]Failure(nil), r.Failures...)
		sort.SliceStable(failures, func(i, j int) bool { return failures[i].Key < failures[j].Key })
		for _, failure := range failures {
			fmt.Fprintf(&out, "  [%s] %s: %s\n", failure.classList(), failure.Key, failure.Reason)
		}
	}

	if len(r.Samples) > 0 || len(r.BookmarkSamples) > 0 {
		out.WriteString("\nREPRESENTATIVE RECORDS (old Postgres -> destination)\n")
	}
	for _, sample := range r.Samples {
		renderSample(&out, "article_index", sample.Before.CanonicalURL, sample.Matches(), sample.Differences())
		fmt.Fprintf(&out, "      before: score=%d tags=%v summary=%q opened=%s vote=%q\n",
			sample.Before.Score, sample.Before.Tags, truncate(sample.Before.Summary),
			formatTime(sample.Before.OpenedAt), sample.Before.Vote)
		fmt.Fprintf(&out, "      after:  score=%d tags=%v summary=%q opened=%s vote=%q\n",
			sample.After.Score, sample.After.Tags, truncate(sample.After.Summary),
			formatTime(sample.After.OpenedAt), sample.After.Vote)
	}
	for _, sample := range r.BookmarkSamples {
		renderSample(&out, "karakeep", sample.Before.URL, sample.Matches(), sample.Differences())
		fmt.Fprintf(&out, "      before: title=%q\n      after:  title=%q\n", sample.Before.Title, sample.After.Title)
	}

	return out.String()
}

func renderSample(out *strings.Builder, destination, key string, matches bool, differences []string) {
	marker := "OK "
	if !matches {
		marker = "DIFF"
	}
	fmt.Fprintf(out, "  %s %s %s\n", marker, destination, key)
	for _, difference := range differences {
		fmt.Fprintf(out, "      ! %s\n", difference)
	}
}

func truncate(text string) string {
	const limit = 60
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}
