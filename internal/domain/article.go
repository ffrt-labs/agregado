package domain

import "time"

type Article struct {
	ID string
	SourceID string
	ExternalURL string
	Title string
	Author *string
	Summary *string
	Content *string
	ContentHash *string
	PublishedAt *time.Time
	IngestedAt time.Time
	IsRead bool
	ReadAt *time.Time
	WordCount *int
	EstimatedReadMinutes *int
	CreatedAt time.Time
}
