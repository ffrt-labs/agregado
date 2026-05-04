package domain

import "time"

type Tag struct {
	ID        string	`db:"id"`
	Name      string	`db:"name"`
	Slug      string	`db:"slug"`
	Color     string	`db:"color"`
	CreatedAt time.Time	`db:"created_at"`
	UpdatedAt time.Time	`db:"updated_at"`
}
