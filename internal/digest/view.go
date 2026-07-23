package digest

import "time"

// DigestView is the display-ready shape of a computed digest, shared by every
// surface that renders one (the web "Daily Digest" page and the email). It is
// deliberately free of surface-specific concerns — no HTMX attributes, no Nav
// sidebar, no absolute URLs — so both renderers can build from the same data.
type DigestView struct {
	Greeting       string
	DeliveryTime   string
	Date           string
	Intro          string
	Groups         []DigestGroupView
	ClearedCount   int
	CandidateCount int
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
	Reason               *string
	IsSaved              bool
}

// BuildView turns a ComputedDigest into a DigestView. sourceNames maps a
// source ID to its display name; callers build it from whatever source list
// they have (a plain map keeps this package free of storage/repo types, so no
// import cycle forms with internal/api).
func BuildView(computed ComputedDigest, sourceNames map[string]string) DigestView {
	groups := make([]DigestGroupView, 0, len(computed.Groups))
	clearedCount := 0
	for _, group := range computed.Groups {
		clearedCount += len(group.Articles)
		topic := "Uncategorized"
		if group.Tag != nil {
			topic = group.Tag.Name
		}

		items := make([]DigestItemView, len(group.Articles))
		for i, a := range group.Articles {
			sourceName := ""
			if a.SourceID != nil {
				sourceName = sourceNames[*a.SourceID]
			}
			// Newsletters have no web home (external_url is nil since Phase 21);
			// the digest links via /r/{id} in the HTML template regardless, and
			// the plain-text fallback omits the URL for them (ExternalURLOr).
			items[i] = DigestItemView{
				Position:             i + 1,
				SourceName:           sourceName,
				Title:                a.Title,
				ExternalURL:          a.ExternalURLOr(""),
				ID:                   a.ID,
				Summary:              a.Summary,
				PublishedAt:          a.PublishedAt,
				EstimatedReadMinutes: a.EstimatedReadMinutes,
				RelevanceScore:       a.RelevanceScore,
				Reason:               a.RelevanceReason,
				IsSaved:              a.IsSaved,
			}
		}

		groups = append(groups, DigestGroupView{Topic: topic, Summary: group.Summary, Items: items})
	}

	return DigestView{
		Greeting:       timeGreeting(),
		DeliveryTime:   "this morning",
		Date:           computed.Date.Format("Monday, January 2"),
		Intro:          computed.Overview,
		Groups:         groups,
		ClearedCount:   clearedCount,
		CandidateCount: computed.CandidateCount,
	}
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
