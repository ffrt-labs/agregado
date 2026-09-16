package ownership

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeLegacy struct {
	articles []LegacyArticle
	sources  int
}

func (f *fakeLegacy) Articles(context.Context) ([]LegacyArticle, error) { return f.articles, nil }
func (f *fakeLegacy) SourceCount(context.Context) (int, error)          { return f.sources, nil }

type fakeBookmarker struct {
	saved map[string]string
	fail  map[string]bool
	calls []string
}

func newFakeBookmarker() *fakeBookmarker {
	return &fakeBookmarker{saved: map[string]string{}, fail: map[string]bool{}}
}

func (f *fakeBookmarker) Lookup(_ context.Context, url string) (BookmarkImport, bool, error) {
	f.calls = append(f.calls, url)
	if f.fail[url] {
		return BookmarkImport{}, false, errors.New("karakeep unreachable")
	}
	title, ok := f.saved[url]
	return BookmarkImport{URL: url, Title: title}, ok, nil
}

func (f *fakeBookmarker) Create(_ context.Context, url, title string) error {
	if f.fail[url] {
		return errors.New("karakeep unreachable")
	}
	f.saved[url] = title
	return nil
}

type fakeImporter struct {
	rows map[string]*IndexImport
	fail map[string]bool
}

func newFakeImporter() *fakeImporter {
	return &fakeImporter{rows: map[string]*IndexImport{}, fail: map[string]bool{}}
}

// Import mirrors the real repo's ON CONFLICT: an existing row keeps its own
// Enrichment, and only a missing Open or vote is filled in.
func (f *fakeImporter) Import(_ context.Context, record IndexImport) error {
	if f.fail[record.CanonicalURL] {
		return errors.New("insert failed")
	}
	existing, found := f.rows[record.CanonicalURL]
	if found {
		if existing.OpenedAt == nil {
			existing.OpenedAt = record.OpenedAt
		}
		if existing.Vote == "" {
			existing.Vote, existing.VotedAt = record.Vote, record.VotedAt
		}
		return nil
	}
	stored := record
	f.rows[record.CanonicalURL] = &stored
	return nil
}

func (f *fakeImporter) Lookup(_ context.Context, canonicalURL string) (IndexImport, bool, error) {
	if f.fail[canonicalURL] {
		return IndexImport{}, false, errors.New("lookup failed")
	}
	row, ok := f.rows[canonicalURL]
	if !ok {
		return IndexImport{}, false, nil
	}
	return *row, true, nil
}

func fixture() *fakeLegacy {
	return &fakeLegacy{
		sources: 12,
		articles: []LegacyArticle{
			{ID: "a", ExternalURL: "https://example.com/1", Title: "One", Score: 5,
				Tags: []string{"tech"}, Summary: "sum", ReadAt: at(1), Vote: "up", VotedAt: at(1), HasBody: true},
			{ID: "b", ExternalURL: "https://example.com/2", Title: "Two", IsSaved: true, SavedAt: at(2), HasBody: true},
			{ID: "c", ExternalURL: "newsletter:9f3c", Title: "Issue", Score: 4},
		},
	}
}

func run(t *testing.T, legacy *fakeLegacy, bookmarker *fakeBookmarker, importer *fakeImporter, write Write) Report {
	t.Helper()
	report, err := NewRunner(legacy, bookmarker, importer, 0).Run(context.Background(), write)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return report
}

func TestDryRunWritesNothing(t *testing.T) {
	bookmarker, importer := newFakeBookmarker(), newFakeImporter()
	run(t, fixture(), bookmarker, importer, DryRun)

	if len(bookmarker.saved) != 0 {
		t.Errorf("dry run created Karakeep bookmarks: %v", bookmarker.saved)
	}
	if len(importer.rows) != 0 {
		t.Errorf("dry run wrote Article Index rows: %v", importer.rows)
	}
}

// A dry run whose duplicate counts are guessed is worthless, so it still reads
// both destinations.
func TestDryRunStillConsultsTheDestinations(t *testing.T) {
	bookmarker, importer := newFakeBookmarker(), newFakeImporter()
	run(t, fixture(), bookmarker, importer, DryRun)

	if len(bookmarker.calls) == 0 {
		t.Error("dry run never consulted Karakeep")
	}
}

func TestDryRunAndApplyAgreeOnEveryCount(t *testing.T) {
	dry := run(t, fixture(), newFakeBookmarker(), newFakeImporter(), DryRun)
	applied := run(t, fixture(), newFakeBookmarker(), newFakeImporter(), Apply)

	for _, class := range Classes {
		if *dry.Counts[class] != *applied.Counts[class] {
			t.Errorf("%s: dry run %+v, apply %+v", class, *dry.Counts[class], *applied.Counts[class])
		}
	}
}

func TestReportCountsEveryDataClass(t *testing.T) {
	report := run(t, fixture(), newFakeBookmarker(), newFakeImporter(), Apply)

	for _, class := range Classes {
		if report.Counts[class] == nil {
			t.Errorf("%s has no counts at all", class)
		}
	}
	if got := *report.Counts[SavedURLs]; got.Source != 1 || got.Transformed != 1 || got.Destination != 1 {
		t.Errorf("saved_urls = %+v", got)
	}
	if got := *report.Counts[Scores]; got.Source != 2 || got.Skipped != 1 || got.Destination != 1 {
		t.Errorf("scores = %+v, want the newsletter sentinel skipped", got)
	}
	if got := *report.Counts[SourceConfig]; got.Source != 12 || got.Skipped != 12 || got.Destination != 0 {
		t.Errorf("source_config = %+v, want all 12 Sources left behind", got)
	}
	if got := *report.Counts[ArticleBodies]; got.Source != 2 || got.Skipped != 2 || got.Destination != 0 {
		t.Errorf("article_bodies = %+v, want both bodies left behind", got)
	}
}

func TestSavedURLsReachKarakeepAndNotThePreferenceInputs(t *testing.T) {
	bookmarker, importer := newFakeBookmarker(), newFakeImporter()
	run(t, fixture(), bookmarker, importer, Apply)

	if bookmarker.saved["https://example.com/2"] != "Two" {
		t.Errorf("the saved URL did not reach Karakeep: %v", bookmarker.saved)
	}
	// preferenceexport reads exactly the rows carrying an Open or a vote.
	for url, row := range importer.rows {
		if url == "https://example.com/2" {
			t.Errorf("the saved URL became an Article Index preference input: %+v", row)
		}
	}
}

func TestRerunningIsIdempotent(t *testing.T) {
	bookmarker, importer := newFakeBookmarker(), newFakeImporter()
	run(t, fixture(), bookmarker, importer, Apply)
	second := run(t, fixture(), bookmarker, importer, Apply)

	if len(bookmarker.saved) != 1 {
		t.Errorf("Karakeep bookmarks after two runs: %v", bookmarker.saved)
	}
	if len(importer.rows) != 1 {
		t.Errorf("Article Index rows after two runs: %v", importer.rows)
	}
	for _, class := range []DataClass{SavedURLs, Scores, Tags, Summaries, Opens, Votes} {
		if got := second.Counts[class].Destination; got != 0 {
			t.Errorf("%s wrote %d rows on the second run, want 0", class, got)
		}
	}
	if got := second.Counts[Scores].Duplicate; got != 1 {
		t.Errorf("scores duplicate on rerun = %d, want 1", got)
	}
}

func TestFailuresAreCountedAndReportedPerClass(t *testing.T) {
	bookmarker, importer := newFakeBookmarker(), newFakeImporter()
	bookmarker.fail["https://example.com/2"] = true
	importer.fail["https://example.com/1"] = true

	report := run(t, fixture(), bookmarker, importer, Apply)

	if got := report.Counts[SavedURLs].Failed; got != 1 {
		t.Errorf("saved_urls failed = %d, want 1", got)
	}
	if got := report.Counts[Scores].Failed; got != 1 {
		t.Errorf("scores failed = %d, want 1", got)
	}
	if len(report.Failures) != 2 {
		t.Fatalf("want 2 failures recorded, got %d: %+v", len(report.Failures), report.Failures)
	}
	if !strings.Contains(report.Render(), "karakeep unreachable") {
		t.Error("the rendered report hides the failure reason")
	}
}

// One destination being down must not abandon the other.
func TestAFailedBookmarkDoesNotStopTheIndexImport(t *testing.T) {
	bookmarker, importer := newFakeBookmarker(), newFakeImporter()
	bookmarker.fail["https://example.com/2"] = true

	run(t, fixture(), bookmarker, importer, Apply)

	if _, ok := importer.rows["https://example.com/1"]; !ok {
		t.Error("the Article Index import stopped when Karakeep failed")
	}
}

func TestSamplesCompareLegacyAgainstTheDestination(t *testing.T) {
	bookmarker, importer := newFakeBookmarker(), newFakeImporter()
	report, err := NewRunner(fixture(), bookmarker, importer, 5).Run(context.Background(), Apply)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(report.Samples) == 0 {
		t.Fatal("want representative records sampled")
	}
	var found bool
	for _, sample := range report.Samples {
		if sample.Before.CanonicalURL != "https://example.com/1" {
			continue
		}
		found = true
		if !sample.Present {
			t.Error("the sampled record is missing from the destination")
		}
		if sample.After.Score != sample.Before.Score || sample.After.Summary != sample.Before.Summary {
			t.Errorf("before %+v does not match after %+v", sample.Before, sample.After)
		}
		if !sample.Matches() {
			t.Errorf("sample reports a mismatch: %s", strings.Join(sample.Differences(), "; "))
		}
	}
	if !found {
		t.Error("the enriched record was never sampled")
	}
	if !strings.Contains(report.Render(), "https://example.com/1") {
		t.Error("the rendered report omits the sampled records")
	}
}

func TestRenderLabelsADryRun(t *testing.T) {
	dry := run(t, fixture(), newFakeBookmarker(), newFakeImporter(), DryRun)
	applied := run(t, fixture(), newFakeBookmarker(), newFakeImporter(), Apply)

	if !strings.Contains(dry.Render(), "DRY RUN") {
		t.Error("a dry run's report does not say so")
	}
	if strings.Contains(applied.Render(), "DRY RUN") {
		t.Error("an applied run's report claims to be a dry run")
	}
}

// The worst failure this migration could have: a destination lookup that
// errors gets read as "not there", and a rerun duplicates every Bookmark.
func TestAFailedLookupNeverBecomesAWrite(t *testing.T) {
	bookmarker, importer := newFakeBookmarker(), newFakeImporter()
	bookmarker.fail["https://example.com/2"] = true
	importer.fail["https://example.com/1"] = true

	report := run(t, fixture(), bookmarker, importer, Apply)

	if len(bookmarker.saved) != 0 {
		t.Errorf("a failed lookup still wrote to Karakeep: %v", bookmarker.saved)
	}
	if len(importer.rows) != 0 {
		t.Errorf("a failed lookup still wrote to the Article Index: %v", importer.rows)
	}
	if report.Counts[SavedURLs].Destination != 0 || report.Counts[Scores].Destination != 0 {
		t.Error("a failed lookup was counted as a successful write")
	}
}
