package domain

import "time"

type Type string

const (
	Rss        Type = "rss"
	Newsletter Type = "newsletter"
)

type Source struct {
	ID 				string
	Name 			string
	Type 			Type
	URL 			*string
	EmailSender 	*string `db:"email_sender"`
	Priority 		int
	IsActive 		bool `db:"is_active" json:"is_active"`
	LastFetchedAt	*time.Time `db:"last_fetched_at"`
	LastError 		*string `db:"last_error"`
	ErrorCount 		int `db:"error_count"`
	DefaultTagID 	*string `db:"default_tag_id"`
	CreatedAt 		time.Time `db:"created_at"`
	UpdatedAt 		time.Time `db:"updated_at"`
	ExtractLinks	bool `db:"extract_links" json:"extract_links"`
	Summarize		bool `db:"summarize" json:"summarize"`
}
