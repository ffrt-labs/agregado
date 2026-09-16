package ownership

import (
	"testing"
	"time"
)

func at(day int) *time.Time {
	t := time.Date(2026, 3, day, 12, 0, 0, 0, time.UTC)
	return &t
}

func TestClassifyResolvesTheCanonicalURLBeforeTheExternalOne(t *testing.T) {
	plan := Classify([]LegacyArticle{{
		ID: "a", ExternalURL: "https://feed.example/redirect", CanonicalURL: "https://example.com/post",
		Title: "Post", Score: 4,
	}})

	if len(plan.Records) != 1 {
		t.Fatalf("want 1 index record, got %d", len(plan.Records))
	}
	if got := plan.Records[0].CanonicalURL; got != "https://example.com/post" {
		t.Errorf("canonical URL = %q, want the canonical_url column", got)
	}
}

func TestClassifyFallsBackToTheExternalURL(t *testing.T) {
	plan := Classify([]LegacyArticle{{
		ID: "a", ExternalURL: "https://example.com/post", Title: "Post", Score: 4,
	}})

	if len(plan.Records) != 1 || plan.Records[0].CanonicalURL != "https://example.com/post" {
		t.Fatalf("want the external URL used as the key, got %+v", plan.Records)
	}
}

func TestClassifySkipsArticlesWithNoWebHome(t *testing.T) {
	// A newsletter that never had a page of its own cannot be keyed by URL in
	// either destination.
	plan := Classify([]LegacyArticle{{ID: "a", Title: "Issue 12", Score: 5, Summary: "s"}})

	if len(plan.Records) != 0 {
		t.Fatalf("want no index records, got %d", len(plan.Records))
	}
	for _, class := range []DataClass{Scores, Summaries} {
		if got := plan.Counts[class].Skipped; got != 1 {
			t.Errorf("%s skipped = %d, want 1", class, got)
		}
	}
}

func TestClassifySkipsNonHTTPURLs(t *testing.T) {
	plan := Classify([]LegacyArticle{{ID: "a", ExternalURL: "newsletter:9f3c", Title: "Issue", Score: 3}})

	if len(plan.Records) != 0 {
		t.Fatalf("want the sentinel URL skipped, got %+v", plan.Records)
	}
}

func TestClassifyCountsEveryDataClassSeparately(t *testing.T) {
	plan := Classify([]LegacyArticle{
		{ID: "a", ExternalURL: "https://example.com/1", Title: "One", Score: 5, Tags: []string{"tech"}, Summary: "sum", ReadAt: at(1), Vote: "up"},
		{ID: "b", ExternalURL: "https://example.com/2", Title: "Two"},
	})

	for class, want := range map[DataClass]int{
		Scores: 1, Tags: 1, Summaries: 1, Opens: 1, Votes: 1,
	} {
		if got := plan.Counts[class].Source; got != want {
			t.Errorf("%s source = %d, want %d", class, got, want)
		}
		if got := plan.Counts[class].Transformed; got != want {
			t.Errorf("%s transformed = %d, want %d", class, got, want)
		}
	}
}

func TestClassifySendsSavedURLsToKarakeepOnly(t *testing.T) {
	plan := Classify([]LegacyArticle{{
		ID: "a", ExternalURL: "https://example.com/keep", Title: "Keep", IsSaved: true, SavedAt: at(2),
	}})

	if len(plan.Bookmarks) != 1 || plan.Bookmarks[0].URL != "https://example.com/keep" {
		t.Fatalf("want one bookmark import, got %+v", plan.Bookmarks)
	}
	if plan.Counts[SavedURLs].Transformed != 1 {
		t.Errorf("saved_urls transformed = %d, want 1", plan.Counts[SavedURLs].Transformed)
	}
}

// The load-bearing boundary of issue #83: a Save is Bookmark data, and Bookmark
// data must never reach PREFERENCES.md. preferenceexport reads exactly the rows
// carrying an Open or a vote, so a Save must produce neither.
func TestClassifyNeverTurnsASaveIntoAPreferenceSignal(t *testing.T) {
	plan := Classify([]LegacyArticle{{
		ID: "a", ExternalURL: "https://example.com/keep", Title: "Keep", IsSaved: true, SavedAt: at(2),
	}})

	for _, record := range plan.Records {
		if record.OpenedAt != nil {
			t.Errorf("a Save became an Open: %+v", record)
		}
		if record.Vote != "" {
			t.Errorf("a Save became a vote: %+v", record)
		}
	}
	if plan.Counts[Opens].Source != 0 || plan.Counts[Votes].Source != 0 {
		t.Errorf("a Save was counted as a preference signal: opens=%+v votes=%+v",
			plan.Counts[Opens], plan.Counts[Votes])
	}
}

// A saved Article that was also genuinely read keeps its Open: that is read
// data, not Bookmark data. Only is_saved/saved_at are barred from the Index.
func TestClassifyKeepsARealOpenOnASavedArticle(t *testing.T) {
	plan := Classify([]LegacyArticle{{
		ID: "a", ExternalURL: "https://example.com/keep", Title: "Keep",
		IsSaved: true, SavedAt: at(2), ReadAt: at(1), Score: 4,
	}})

	if len(plan.Records) != 1 || plan.Records[0].OpenedAt == nil {
		t.Fatalf("want the real Open preserved, got %+v", plan.Records)
	}
	if !plan.Records[0].OpenedAt.Equal(*at(1)) {
		t.Errorf("OpenedAt = %v, want read_at", plan.Records[0].OpenedAt)
	}
}

func TestClassifyMergesArticlesSharingACanonicalURL(t *testing.T) {
	plan := Classify([]LegacyArticle{
		{ID: "a", ExternalURL: "https://example.com/post", Title: "Post", Score: 4,
			Tags: []string{"tech"}, ReadAt: at(5), Vote: "down", VotedAt: at(5)},
		{ID: "b", CanonicalURL: "https://example.com/post", Title: "Post (resent)",
			Summary: "the summary", Tags: []string{"ai", "tech"}, ReadAt: at(3), Vote: "up", VotedAt: at(9)},
	})

	if len(plan.Records) != 1 {
		t.Fatalf("want the two rows merged into one, got %d", len(plan.Records))
	}
	record := plan.Records[0]
	if record.Title != "Post" {
		t.Errorf("Title = %q, want the first row's title", record.Title)
	}
	if record.Summary != "the summary" {
		t.Errorf("Summary = %q, want the later row to fill the gap", record.Summary)
	}
	if record.Score != 4 {
		t.Errorf("Score = %d, want 4", record.Score)
	}
	if got := record.Tags; len(got) != 2 || got[0] != "ai" || got[1] != "tech" {
		t.Errorf("Tags = %v, want the sorted union", got)
	}
	// An Open is the *first* open, so deduplication keeps the earliest.
	if !record.OpenedAt.Equal(*at(3)) {
		t.Errorf("OpenedAt = %v, want the earliest read_at", record.OpenedAt)
	}
	// "Current" vote: the most recent one wins.
	if record.Vote != "up" {
		t.Errorf("Vote = %q, want the most recent vote", record.Vote)
	}
}

func TestClassifyCountsMergedRowsAsDuplicates(t *testing.T) {
	plan := Classify([]LegacyArticle{
		{ID: "a", ExternalURL: "https://example.com/post", Title: "Post", Score: 4, ReadAt: at(5)},
		{ID: "b", ExternalURL: "https://example.com/post", Title: "Post", Score: 4, ReadAt: at(3)},
	})

	if got := plan.Counts[Opens].Source; got != 2 {
		t.Errorf("opens source = %d, want both rows counted at the source", got)
	}
	if got := plan.Counts[Opens].Duplicate; got != 1 {
		t.Errorf("opens duplicate = %d, want 1", got)
	}
	if got := plan.Counts[Opens].Transformed; got != 1 {
		t.Errorf("opens transformed = %d, want 1 deduplicated Open", got)
	}
}

func TestClassifyDropsArticlesCarryingNothingWorthKeeping(t *testing.T) {
	plan := Classify([]LegacyArticle{{ID: "a", ExternalURL: "https://example.com/1", Title: "Bare"}})

	if len(plan.Records) != 0 {
		t.Errorf("want no index record for an unenriched, unread, unvoted Article, got %+v", plan.Records)
	}
	if len(plan.Bookmarks) != 0 {
		t.Errorf("want no bookmark for an unsaved Article, got %+v", plan.Bookmarks)
	}
}

func TestClassifyIsDeterministic(t *testing.T) {
	articles := []LegacyArticle{
		{ID: "c", ExternalURL: "https://example.com/3", Title: "Three", Score: 3},
		{ID: "a", ExternalURL: "https://example.com/1", Title: "One", Score: 1},
		{ID: "b", ExternalURL: "https://example.com/2", Title: "Two", Score: 2},
	}

	first, second := Classify(articles), Classify(articles)
	for i := range first.Records {
		if first.Records[i].CanonicalURL != second.Records[i].CanonicalURL {
			t.Fatalf("Classify is not deterministic at %d", i)
		}
	}
	if first.Records[0].CanonicalURL != "https://example.com/1" {
		t.Errorf("records are not ordered by canonical URL: %q", first.Records[0].CanonicalURL)
	}
}

func TestExcludedClassesAreCountedRatherThanSilent(t *testing.T) {
	plan := Classify([]LegacyArticle{
		{ID: "a", ExternalURL: "https://example.com/1", Title: "One", Score: 3, HasBody: true},
		{ID: "b", ExternalURL: "https://example.com/2", Title: "Two", Score: 3},
	})
	plan.ExcludeSources(7)

	if got := plan.Counts[ArticleBodies]; got.Source != 1 || got.Skipped != 1 || got.Destination != 0 {
		t.Errorf("article_bodies = %+v, want 1 source, 1 skipped, 0 destination", got)
	}
	if got := plan.Counts[SourceConfig]; got.Source != 7 || got.Skipped != 7 || got.Destination != 0 {
		t.Errorf("source_config = %+v, want 7 source, 7 skipped, 0 destination", got)
	}
}

func TestClassifyPrefersADatedVoteOverAnUndatedOne(t *testing.T) {
	plan := Classify([]LegacyArticle{
		{ID: "a", ExternalURL: "https://example.com/post", Title: "Post", Vote: "up", VotedAt: at(4)},
		{ID: "b", ExternalURL: "https://example.com/post", Title: "Post", Vote: "down"},
	})

	if len(plan.Records) != 1 {
		t.Fatalf("want 1 record, got %d", len(plan.Records))
	}
	if plan.Records[0].Vote != "up" {
		t.Errorf("Vote = %q, want the dated vote to win over an undated one", plan.Records[0].Vote)
	}
}
