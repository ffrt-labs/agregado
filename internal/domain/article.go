package domain

import "time"

type Article struct {
	ID       				string `json:"-"`
	SourceID 				*string `db:"source_id" json:"source_id"`
	ExternalURL 			string `db:"external_url" json:"external_url"`
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
