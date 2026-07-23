package domain

import "time"

type Article struct {
	ID       				string `json:"-"`
	SourceID 				*string `db:"source_id" json:"source_id"`
	// ExternalURL is the article's real web home, or nil when it has no page of
	// its own (newsletters whose body arrived by email). Nullable since Phase 21
	// (issue #3): before, newsletters carried a synthetic 'newsletter:<uuid>'
	// sentinel here because the column was NOT NULL. "Is this a newsletter?" is
	// now answered by sources.type, never by inspecting this field.
	ExternalURL 			*string `db:"external_url" json:"external_url"`
	Title 					string `json:"title"`
	Author 					*string `json:"author"`
	Summary 				*string `json:"summary"`
	Content 				*string `json:"content"`
	ContentHash 			*string `db:"content_hash" json:"-"`
	PublishedAt 			*time.Time `db:"published_at" json:"published_at"`
	IngestedAt 				time.Time `db:"ingested_at" json:"-"`
	IsRead 					bool `db:"is_read" json:"-"`
	IsSaved 				bool `db:"is_saved" json:"-"`
	SavedAt 				*time.Time `db:"saved_at" json:"-"`
	ReadAt 					*time.Time `db:"read_at" json:"-"`
	WordCount 				*int `db:"word_count" json:"-"`
	EstimatedReadMinutes 	*int `db:"estimated_read_minutes" json:"-"`
	Tags 					[]Tag `db:"-" json:"-"`
	CreatedAt 				time.Time `db:"created_at" json:"-"`
	RelevanceScore 			*int `db:"relevance_score" json:"relevance_score,omitempty"`
	RelevanceReason 		*string `db:"relevance_reason" json:"relevance_reason,omitempty"`
	ParentArticleId			*string `db:"parent_article_id" json:"-"`
	DistilledContent 		*string `db:"distilled_content" json:"-"`
	ContentSource 			*string `db:"content_source" json:"content_source,omitempty"`
	// CanonicalURL is the newsletter's real web home, extracted at parse time
	// (Archived-At header or a "view in browser" anchor). Nullable: newsletters
	// with no web version keep it nil and fall back to the reader page. RSS
	// articles leave it nil — their ExternalURL is already the canonical page.
	CanonicalURL 			*string `db:"canonical_url" json:"canonical_url,omitempty"`
	// RawHTML is transient ingestion provenance: the newsletter's original email
	// HTML, carried through the queue so the worker can persist it in the
	// newsletter_raw_html table after Create yields an id. Never a column on
	// articles (db:"-") — see issue #2, Storage — raw HTML.
	RawHTML 				string `db:"-" json:"raw_html,omitempty"`
}

// ExternalURLOr returns the article's external URL, or fallback when it has no
// web home (ExternalURL is nil — a newsletter, since Phase 21). Keeps the
// nil-unwrap in one place so log lines and view structs, which want a plain
// string, don't each re-derive "no web home" from the nil. The redirect uses
// webURL instead, which needs the canonical-URL precedence and a found/not
// distinction this helper deliberately flattens away.
func (a Article) ExternalURLOr(fallback string) string {
	if a.ExternalURL != nil {
		return *a.ExternalURL
	}
	return fallback
}

// BestText returns the richest text available for this article, in order of
// preference: an algorithmically distilled body, the full content (fetched
// article or feed content:encoded), then the feed's <description> teaser as a
// last resort. Callers that previously hand-rolled this Content-else-Summary
// fallback (worker.go, ranker.go) should use this instead.
func (a Article) BestText() string {
	if a.DistilledContent != nil && *a.DistilledContent != "" {
		return *a.DistilledContent
	}
	if a.Content != nil && *a.Content != "" {
		return *a.Content
	}
	if a.Summary != nil {
		return *a.Summary
	}
	return ""
}
