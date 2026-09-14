// Package digestartifact owns the persisted, daily Digest. It intentionally
// has no scheduler or delivery code: n8n chooses when to retrieve/send it.
package digestartifact

import (
	"context"
	"fmt"
	"html/template"
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
type Choice struct{ ArticleID, Why string }
type Item struct {
	Candidate
	Why, ReadURL, UpvoteURL, DownvoteURL string
}
type Artifact struct {
	ID                                            string
	Date                                          time.Time
	Subject, HTML, Text                           string
	Items                                         []Item
	CandidateCount, FloorPassCount, SelectedCount int
}
type Store interface {
	Candidates(context.Context, time.Time) ([]Article, error)
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
func (s *Service) ForDate(ctx context.Context, day time.Time) (Artifact, bool, error) {
	day = date(day)
	if saved, ok, err := s.store.Find(ctx, day); err != nil || ok {
		return saved, false, err
	}
	articles, err := s.store.Candidates(ctx, day)
	if err != nil {
		return Artifact{}, false, err
	}
	candidates := uniqueAndDiverse(articles, s.floor, s.max)
	artifact := Artifact{ID: day.Format("20060102"), Date: day, CandidateCount: candidates.total, FloorPassCount: candidates.floorPass, Subject: "Your Daily Digest - " + day.Format("January 2, 2006")}
	if len(candidates.items) > 0 {
		choices, err := s.frontier.Select(ctx, candidates.items)
		if err != nil {
			return Artifact{}, false, err
		}
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
	if len(selected) > 0 && len(selected) < 3 {
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
func render(a Artifact) (string, string) {
	if len(a.Items) == 0 {
		return "<p>No Articles passed the Digest quality floor today.</p>", "No Articles passed the Digest quality floor today.\n"
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
