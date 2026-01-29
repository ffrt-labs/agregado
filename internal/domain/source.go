package domain

import "time"

type Type string

const (
    Rss  Type = "rss"
    Newsletter Type = "newsletter"
    Manual Type = "manual"
)

type Source struct {
	ID string
	Name string
	Type Type
	URL *string
	EmailSender *string
	Priority int
	IsActive bool
	LastFetchedAt *time.Time
	LastError *string
	ErrorCount int
	CreatedAt time.Time
	UpdatedAt time.Time
}
