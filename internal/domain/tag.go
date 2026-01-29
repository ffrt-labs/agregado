package domain

import "time"

type Tag struct {
	ID        string
	Name      string
	Slug      string
	Color     string
	CreatedAt time.Time
	UpdatedAt time.Time
}
