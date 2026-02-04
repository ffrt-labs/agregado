package domain

import "time"

type Article struct {
	ID       string
	SourceID *string `db:"source_id"`
	ExternalURL string `db:"external_url"`
	Title string
	Author *string
	Summary *string
	Content *string
	ContentHash *string `db:"content_hash"`
	PublishedAt *time.Time `db:"published_at"`
	IngestedAt time.Time `db:"ingested_at"`
	IsRead bool `db:"is_read"`
	ReadAt *time.Time `db:"read_at"`
	WordCount *int `db:"word_count"`
	EstimatedReadMinutes *int `db:"estimated_read_minutes"`
	Tags []Tag `db:"-"`
	CreatedAt time.Time `db:"created_at"`
}
