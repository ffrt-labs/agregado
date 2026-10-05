// Package digestartifact owns the persisted, daily Digest. It intentionally
// has no scheduler or delivery code: n8n chooses when to retrieve/send it.
package digestartifact

import (
	"context"
	"fmt"
	"html/template"
	"log/slog"
	"sort"
	"strings"
	"time"
)

type Article struct {
	ID, CanonicalURL, Title, Summary, Source string
	Score                                    int
	Topics                                   []string
}
type Candidate struct {
	Article
	Exploration bool
}
// ArticleID/Why must carry explicit json tags: OpDigestSelect's prompt
// (internal/ai/prompts.go) promises a snake_case {"article_id":...,"why":...}
// shape, and encoding/json's untagged case-insensitive fallback does not
// bridge that naming convention — it matched "Why" against "why" by luck but
// never "ArticleID" against "article_id", so every real selection silently
// discarded its article IDs.
type Choice struct {
	ArticleID string `json:"article_id"`
	Why       string `json:"why"`
}
type Item struct {
	Candidate
	Why, ReadURL, UpvoteURL, DownvoteURL string
}
// WindowStart/WindowEnd are the Enrichment window the candidates came from;
// the next Digest's window starts at this one's WindowEnd. EmptyReason is set
// only on an artifact with no items, which is never persisted.
type Artifact struct {
	ID                                            string
	Date                                          time.Time
	Subject, HTML, Text                           string
	Items                                         []Item
	CandidateCount, FloorPassCount, SelectedCount int
	WindowStart, WindowEnd                        time.Time
	EmptyReason                                   string
}

// Pool is the Articles whose Enrichment completed in [Since, Until): Since is
// the previous persisted Digest's WindowEnd, bounded by the fallback lookback.
type Pool struct {
	Since, Until time.Time
	Articles     []Article
}

// FallbackLookback bounds the candidate window when there is no previous
// Digest, or the previous one is older than this (e.g. after an outage).
const FallbackLookback = 72 * time.Hour

type Store interface {
	Candidates(ctx context.Context, day time.Time, lookback time.Duration) (Pool, error)
	Find(context.Context, time.Time) (Artifact, bool, error)
	Save(context.Context, Artifact) (Artifact, bool, error)
}

// Frontier is deliberately one call: it orders the prepared candidates and
// writes each selection's explanation together.
type Frontier interface {
	Select(context.Context, []Candidate) ([]Choice, error)
}
type Service struct {
	store      Store
	frontier   Frontier
	baseURL    string
	floor, max int
}

func NewService(store Store, frontier Frontier, baseURL string, floor, max int) *Service {
	if max <= 0 || max > 10 {
		max = 10
	}
	if floor <= 0 {
		floor = 3
	}
	return &Service{store: store, frontier: frontier, baseURL: strings.TrimSuffix(baseURL, "/"), floor: floor, max: max}
}
// ForDate's intermediate steps are logged at Info — the frontier call in
// particular can legitimately run for tens of seconds to several minutes
// (a single request scoring/explaining several candidates against a
// reasoning model), so a live `docker logs` tail should be able to show
// that it's in flight rather than going silent until it finally succeeds or
// times out (agregado#145's live verification hit exactly that gap: a 90s
// wait with no visibility into how far the request had gotten).
func (s *Service) ForDate(ctx context.Context, day time.Time) (Artifact, bool, error) {
	day = date(day)
	dateStr := day.Format("2006-01-02")
	if saved, ok, err := s.store.Find(ctx, day); err != nil || ok {
		return saved, false, err
	}
	pool, err := s.store.Candidates(ctx, day, FallbackLookback)
	if err != nil {
		return Artifact{}, false, err
	}
	candidates := uniqueAndDiverse(pool.Articles, s.floor, s.max)
	slog.Info("digest: candidates prepared", "component", "digestartifact", "date", dateStr,
		"since", pool.Since, "until", pool.Until,
		"pool", len(pool.Articles), "unique", candidates.total, "floor_pass", candidates.floorPass, "selected", len(candidates.items))
	artifact := Artifact{ID: day.Format("20060102"), Date: day, CandidateCount: candidates.total, FloorPassCount: candidates.floorPass, Subject: "Your Daily Digest - " + day.Format("January 2, 2006"), WindowStart: pool.Since, WindowEnd: pool.Until}
	if len(candidates.items) > 0 {
		slog.Info("digest: calling frontier select", "component", "digestartifact", "date", dateStr, "candidates", len(candidates.items))
		start := time.Now()
		choices, err := s.frontier.Select(ctx, candidates.items)
		if err != nil {
			slog.Error("digest: frontier select failed", "component", "digestartifact", "date", dateStr, "elapsed", time.Since(start).String(), "err", err)
			return Artifact{}, false, err
		}
		slog.Info("digest: frontier select returned", "component", "digestartifact", "date", dateStr, "elapsed", time.Since(start).String(), "choices", len(choices))
		byID := make(map[string]Choice, len(choices))
		for _, choice := range choices {
			byID[choice.ArticleID] = choice
		}
		for _, candidate := range candidates.items {
			if choice, ok := byID[candidate.ID]; ok {
				artifact.Items = append(artifact.Items, s.item(candidate, choice.Why))
			}
		}
	}
	artifact.SelectedCount = len(artifact.Items)
	if artifact.SelectedCount == 0 {
		// Still returned so n8n sends it (#79's AC12), but not persisted: the
		// endpoint is idempotent, and a saved empty artifact would be re-sent
		// for this date even after the pipeline recovers. Not persisting also
		// keeps the next window anchored to the last Digest that had Articles.
		artifact.EmptyReason = emptyReason(artifact)
		artifact.HTML, artifact.Text = render(artifact)
		slog.Warn("digest: empty, not persisted", "component", "digestartifact", "date", dateStr, "reason", artifact.EmptyReason)
		return artifact, false, nil
	}
	artifact.HTML, artifact.Text = render(artifact)
	return s.store.Save(ctx, artifact)
}

type prepared struct {
	items            []Candidate
	total, floorPass int
}

func uniqueAndDiverse(articles []Article, floor, max int) prepared {
	seen := map[string]bool{}
	unique := make([]Article, 0, len(articles))
	for _, a := range articles {
		if a.CanonicalURL != "" && !seen[a.CanonicalURL] {
			seen[a.CanonicalURL] = true
			unique = append(unique, a)
		}
	}
	sort.SliceStable(unique, func(i, j int) bool { return unique[i].Score > unique[j].Score })
	passing, rejected := []Article{}, []Article{}
	for _, a := range unique {
		if a.Score >= floor {
			passing = append(passing, a)
		} else {
			rejected = append(rejected, a)
		}
	}
	selected := diverse(passing, max)
	// Tops up to three even when nothing passes: low-scoring candidates must
	// never force an empty Digest (#79's AC12).
	if len(selected) < 3 {
		for _, a := range diverse(rejected, 3-len(selected)) {
			a.Exploration = true
			selected = append(selected, a)
		}
	}
	return prepared{items: selected, total: len(unique), floorPass: len(passing)}
}

// diverse greedily applies a small repeat penalty. It biases source/topic
// variety but never imposes quotas or excludes a high-scoring Article.
func diverse(articles []Article, limit int) []Candidate {
	remaining := append([]Article(nil), articles...)
	out := []Candidate{}
	sources, topics := map[string]int{}, map[string]int{}
	for len(remaining) > 0 && len(out) < limit {
		best := 0
		bestValue := -1 << 30
		for i, a := range remaining {
			value := a.Score*100 - sources[a.Source]*10
			for _, topic := range a.Topics {
				value -= topics[topic] * 5
			}
			if value > bestValue {
				best, bestValue = i, value
			}
		}
		a := remaining[best]
		remaining = append(remaining[:best], remaining[best+1:]...)
		out = append(out, Candidate{Article: a})
		sources[a.Source]++
		for _, topic := range a.Topics {
			topics[topic]++
		}
	}
	return out
}
func (s *Service) item(c Candidate, why string) Item {
	return Item{Candidate: c, Why: why, ReadURL: s.baseURL + "/r/" + c.ID, UpvoteURL: s.baseURL + "/f/" + c.ID + "/up", DownvoteURL: s.baseURL + "/f/" + c.ID + "/down"}
}
func emptyReason(a Artifact) string {
	if a.CandidateCount == 0 {
		// The window is on the database clock (TIMESTAMP columns), so no zone
		// is claimed here.
		return fmt.Sprintf("No Articles finished Enrichment between %s and %s.", a.WindowStart.Format("Jan 2 15:04"), a.WindowEnd.Format("Jan 2 15:04"))
	}
	return fmt.Sprintf("The selection model chose none of %d candidate Articles.", a.CandidateCount)
}
func render(a Artifact) (string, string) {
	if len(a.Items) == 0 {
		return "<p>" + template.HTMLEscapeString(a.EmptyReason) + "</p>", a.EmptyReason + "\n"
	}
	var html, text strings.Builder
	html.WriteString("<ol>")
	for _, i := range a.Items {
		label := template.HTMLEscapeString(i.Title)
		if i.Exploration {
			label += " <em>Exploration pick</em>"
		}
		fmt.Fprintf(&html, "<li><a href=%q>%s</a><p>%s</p><a href=%q>👍</a> <a href=%q>👎</a></li>", i.ReadURL, label, template.HTMLEscapeString(i.Why), i.UpvoteURL, i.DownvoteURL)
		fmt.Fprintf(&text, "%s%s\n%s\n%s\n👍 %s  👎 %s\n\n", i.Title, map[bool]string{true: " [Exploration pick]", false: ""}[i.Exploration], i.Why, i.ReadURL, i.UpvoteURL, i.DownvoteURL)
	}
	html.WriteString("</ol>")
	return html.String(), text.String()
}
func date(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
