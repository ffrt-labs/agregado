package api

import (
	"context"
	"net/http"
	"time"

	"github.com/felipeafreitas/agregado/internal/digest"
)

type DigestScheduler interface {
	Today(ctx context.Context) (digest.ComputedDigest, error)
}

type DigestArticleCounter interface {
	Count(ctx context.Context) (int, error)
}

type DigestPageData struct {
	Greeting     string
	DeliveryTime string
	Date         string
	Intro        string
	Groups       []DigestGroupView
	Nav          NavData
}

type DigestGroupView struct {
	Topic   string
	Summary string
	Items   []DigestItemView
}

type DigestItemView struct {
	Position             int
	SourceName           string
	Title                string
	ExternalURL          string
	ID                   string
	Summary              *string
	PublishedAt          *time.Time
	EstimatedReadMinutes *int
	RelevanceScore       *int
	IsSaved              bool
}

type DigestHandler struct {
	scheduler DigestScheduler
	sources   SourceLister
	articles  DigestArticleCounter
}

func NewDigestHandler(scheduler DigestScheduler, sources SourceLister, articles DigestArticleCounter) *DigestHandler {
	return &DigestHandler{
		scheduler: scheduler,
		sources:   sources,
		articles:  articles,
	}
}

func (h *DigestHandler) HomePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	computed, err := h.scheduler.Today(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	sources, _ := h.sources.List(ctx, 100, 0)
	sourceMap := make(map[string]string, len(sources))
	for _, s := range sources {
		sourceMap[s.ID] = s.Name
	}

	articleCount, _ := h.articles.Count(ctx)

	seen := make(map[string]struct{})
	for _, group := range computed.Groups {
		for _, a := range group.Articles {
			seen[a.ID] = struct{}{}
		}
	}

	groups := make([]DigestGroupView, 0, len(computed.Groups))
	for _, group := range computed.Groups {
		topic := "Uncategorized"
		if group.Tag != nil {
			topic = group.Tag.Name
		}

		items := make([]DigestItemView, len(group.Articles))
		for i, a := range group.Articles {
			sourceName := ""
			if a.SourceID != nil {
				sourceName = sourceMap[*a.SourceID]
			}
			items[i] = DigestItemView{
				Position:             i + 1,
				SourceName:           sourceName,
				Title:                a.Title,
				ExternalURL:          a.ExternalURL,
				ID:                   a.ID,
				Summary:              a.Summary,
				PublishedAt:          a.PublishedAt,
				EstimatedReadMinutes: a.EstimatedReadMinutes,
				RelevanceScore:       a.RelevanceScore,
			}
		}

		groups = append(groups, DigestGroupView{Topic: topic, Summary: group.Summary, Items: items})
	}

	render(w, "digest.html", DigestPageData{
		Greeting:     timeGreeting(),
		DeliveryTime: "this morning",
		Date:         computed.Date.Format("Monday, January 2"),
		Intro:        computed.Overview,
		Groups:       groups,
		Nav: NavData{
			ArticleCount: articleCount,
			ClearedCount: len(seen),
			SourceCount:  len(sources),
		},
	})
}

func timeGreeting() string {
	hour := time.Now().Hour()
	switch {
	case hour < 12:
		return "Good morning."
	case hour < 18:
		return "Good afternoon."
	default:
		return "Good evening."
	}
}
