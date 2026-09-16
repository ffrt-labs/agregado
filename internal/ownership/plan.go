// Package ownership moves the old Agregado Postgres data to whichever tool
// finally owns it (issue #83): saved URLs become Karakeep Bookmarks, and
// Scores, tags, summaries, Opens and current votes become Article Index rows.
// Source configuration and Article bodies are deliberately left behind.
//
// Classify is the whole decision, and it is pure: every rule about what moves,
// what merges and what is dropped is decided from legacy rows alone, with no
// database or HTTP client in sight. The Runner only carries the result out.
package ownership

import (
	"net/url"
	"sort"
	"strings"
	"time"
)

// DataClass is one kind of legacy data with one final owner. Excluded classes
// are listed alongside the migrated ones on purpose: an exclusion that is only
// visible as an absence cannot be verified.
type DataClass string

const (
	// SavedURLs is the only class whose destination is Karakeep.
	SavedURLs DataClass = "saved_urls"
	Scores    DataClass = "scores"
	Tags      DataClass = "tags"
	Summaries DataClass = "summaries"
	Opens     DataClass = "opens"
	Votes     DataClass = "votes"
	// SourceConfig and ArticleBodies never enter the new schema. They are
	// counted so a dry run reports them as deliberately skipped.
	SourceConfig  DataClass = "source_config"
	ArticleBodies DataClass = "article_bodies"
)

// Classes is the report order: destinations first, exclusions last.
var Classes = []DataClass{SavedURLs, Scores, Tags, Summaries, Opens, Votes, SourceConfig, ArticleBodies}

// Destination names where a class lands, for the report.
func (c DataClass) Destination() string {
	switch c {
	case SavedURLs:
		return "karakeep"
	case SourceConfig, ArticleBodies:
		return "(excluded)"
	default:
		return "article_index"
	}
}

// Counts is the per-class tally a dry run reports. Source counts legacy rows
// carrying the class; Transformed counts what survived classification;
// Destination counts what the destination actually accepted as new.
type Counts struct {
	Source      int
	Destination int
	Transformed int
	Skipped     int
	Duplicate   int
	Failed      int
}

// LegacyArticle is one row of the old articles table joined with its tags and
// its current vote. It deliberately carries no body — only HasBody, so the
// bodies left behind can be counted without loading tens of megabytes of text.
type LegacyArticle struct {
	ID           string
	ExternalURL  string
	CanonicalURL string
	Title        string
	Author       string
	Summary      string
	PublishedAt  *time.Time
	Score        int // 0 when the legacy Article was never scored
	Tags         []string
	IsSaved      bool
	SavedAt      *time.Time
	ReadAt       *time.Time
	Vote         string // "up", "down", or "" — the current vote only
	VotedAt      *time.Time
	HasBody      bool
	CreatedAt    time.Time
}

// BookmarkImport is a Save replayed into Karakeep. It carries a URL and a
// title and nothing else: no Enrichment crosses the seam (ADR-0005).
type BookmarkImport struct {
	URL       string
	Title     string
	LegacyIDs []string
}

// IndexImport is one migrated Article Index record, merged from every legacy
// row sharing its canonical URL. It has no content field, by the same rule the
// live Article Index follows.
type IndexImport struct {
	CanonicalURL string
	Title        string
	Author       string
	Summary      string
	PublishedAt  *time.Time
	Score        int
	Tags         []string
	OpenedAt     *time.Time
	Vote         string
	VotedAt      *time.Time
	LegacyIDs    []string
}

// Plan is what Classify decided. Counts is keyed by every class in Classes,
// so a report never has to distinguish "zero" from "not measured".
type Plan struct {
	Bookmarks []BookmarkImport
	Records   []IndexImport
	Counts    map[DataClass]*Counts
}

// ExcludeSources records the Sources left behind. Source configuration has no
// row in the new schema, so it can only be counted from outside Classify.
func (p *Plan) ExcludeSources(total int) {
	p.Counts[SourceConfig].Source += total
	p.Counts[SourceConfig].Skipped += total
}

// webURL returns the Article's real web home, preferring the canonical URL
// extracted at parse time over the external one. Newsletters that never had a
// page of their own — and the old `newsletter:<uuid>` sentinel — return false:
// both destinations key on a URL, so such an Article cannot move.
func webURL(a LegacyArticle) (string, bool) {
	for _, candidate := range []string{a.CanonicalURL, a.ExternalURL} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		parsed, err := url.ParseRequestURI(candidate)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			continue
		}
		return candidate, true
	}
	return "", false
}

// carried reports which classes this legacy row holds data for. Note what is
// absent: IsSaved is never a preference signal. A Save is Bookmark data, and
// Bookmark data must not feed PREFERENCES.md (issue #83), so it contributes to
// SavedURLs alone and never to Opens or Votes.
func carried(a LegacyArticle) []DataClass {
	var classes []DataClass
	if a.IsSaved {
		classes = append(classes, SavedURLs)
	}
	if a.Score > 0 {
		classes = append(classes, Scores)
	}
	if len(a.Tags) > 0 {
		classes = append(classes, Tags)
	}
	if strings.TrimSpace(a.Summary) != "" {
		classes = append(classes, Summaries)
	}
	if a.ReadAt != nil {
		classes = append(classes, Opens)
	}
	if a.Vote != "" {
		classes = append(classes, Votes)
	}
	return classes
}

// indexClasses are the classes that land in the Article Index — everything
// carried except the Save, which belongs to Karakeep.
func indexClasses(classes []DataClass) []DataClass {
	var kept []DataClass
	for _, class := range classes {
		if class != SavedURLs {
			kept = append(kept, class)
		}
	}
	return kept
}

func newCounts() map[DataClass]*Counts {
	counts := make(map[DataClass]*Counts, len(Classes))
	for _, class := range Classes {
		counts[class] = &Counts{}
	}
	return counts
}

// Classify decides, for a batch of legacy Articles, what moves where. Rows
// sharing a canonical URL are merged rather than dropped, because the Article
// Index is keyed by URL and the old table was not: the same page could arrive
// twice through two Sources.
func Classify(articles []LegacyArticle) Plan {
	plan := Plan{Counts: newCounts()}

	// Deterministic order: the merge rules are "first row wins", so the input
	// order must not decide the outcome. Oldest first, ties broken by id.
	ordered := append([]LegacyArticle(nil), articles...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
		}
		return ordered[i].ID < ordered[j].ID
	})

	records := map[string]*IndexImport{}
	bookmarks := map[string]*BookmarkImport{}
	// seen tracks, per URL, which classes already contributed, so a second row
	// merging into the same record counts as a duplicate rather than a source
	// of new data.
	seen := map[string]map[DataClass]bool{}

	for _, article := range ordered {
		if article.HasBody {
			plan.Counts[ArticleBodies].Source++
			plan.Counts[ArticleBodies].Skipped++
		}

		classes := carried(article)
		for _, class := range classes {
			plan.Counts[class].Source++
		}
		if len(classes) == 0 {
			continue
		}

		key, ok := webURL(article)
		if !ok {
			for _, class := range classes {
				plan.Counts[class].Skipped++
			}
			continue
		}

		if seen[key] == nil {
			seen[key] = map[DataClass]bool{}
		}

		if article.IsSaved {
			if existing, ok := bookmarks[key]; ok {
				plan.Counts[SavedURLs].Duplicate++
				existing.LegacyIDs = append(existing.LegacyIDs, article.ID)
			} else {
				bookmarks[key] = &BookmarkImport{URL: key, Title: article.Title, LegacyIDs: []string{article.ID}}
				plan.Counts[SavedURLs].Transformed++
			}
		}

		indexed := indexClasses(classes)
		if len(indexed) == 0 {
			continue
		}

		record, existed := records[key]
		if !existed {
			record = &IndexImport{CanonicalURL: key}
			records[key] = record
		}
		record.LegacyIDs = append(record.LegacyIDs, article.ID)
		merge(record, article)

		for _, class := range indexed {
			if seen[key][class] {
				plan.Counts[class].Duplicate++
				continue
			}
			seen[key][class] = true
			plan.Counts[class].Transformed++
		}
	}

	for _, record := range records {
		sort.Strings(record.Tags)
		plan.Records = append(plan.Records, *record)
	}
	sort.Slice(plan.Records, func(i, j int) bool {
		return plan.Records[i].CanonicalURL < plan.Records[j].CanonicalURL
	})
	for _, bookmark := range bookmarks {
		plan.Bookmarks = append(plan.Bookmarks, *bookmark)
	}
	sort.Slice(plan.Bookmarks, func(i, j int) bool { return plan.Bookmarks[i].URL < plan.Bookmarks[j].URL })

	return plan
}

// merge folds one legacy row into the record for its canonical URL. Scalar
// fields are first-non-empty-wins (the oldest row is the authoritative one);
// the two signal fields have their own rules, because they carry meaning that
// "first wins" would get wrong.
func merge(record *IndexImport, article LegacyArticle) {
	if record.Title == "" {
		record.Title = article.Title
	}
	if record.Author == "" {
		record.Author = article.Author
	}
	if record.Summary == "" {
		record.Summary = strings.TrimSpace(article.Summary)
	}
	if record.Score == 0 {
		record.Score = article.Score
	}
	if record.PublishedAt == nil {
		record.PublishedAt = article.PublishedAt
	}
	for _, tag := range article.Tags {
		if !contains(record.Tags, tag) {
			record.Tags = append(record.Tags, tag)
		}
	}
	// An Open is the *first* time you opened the page, so deduplicating two
	// rows for one URL keeps the earliest — matching what the live Article
	// Index does when the same record is opened twice.
	if article.ReadAt != nil && (record.OpenedAt == nil || article.ReadAt.Before(*record.OpenedAt)) {
		record.OpenedAt = article.ReadAt
	}
	// The migrated vote is the *current* one, so the most recent wins. An
	// undated vote never displaces a dated one — it carries no evidence of
	// being more recent.
	if article.Vote != "" && newerVote(record, article) {
		record.Vote, record.VotedAt = article.Vote, article.VotedAt
	}
}

func newerVote(record *IndexImport, article LegacyArticle) bool {
	switch {
	case record.Vote == "":
		return true
	case article.VotedAt == nil:
		return false
	case record.VotedAt == nil:
		return true
	default:
		return article.VotedAt.After(*record.VotedAt)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
