package ownership

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Sample is one representative record compared before (as classified from the
// old database) and after (as read back from the destination). The comparison
// is the acceptance check: a migration nobody checked is a migration nobody
// can trust.
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
