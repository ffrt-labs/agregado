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
	ParentArticleId			*string `db:"parent_article_id" json:"-"`
}
