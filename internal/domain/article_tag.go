package domain

import "time"

type ArticleTag struct {
    ArticleID string    `db:"article_id"`
    ID        string    `db:"id"`
    Name      string    `db:"name"`
    Slug      string    `db:"slug"`
    Color     string    `db:"color"`
    CreatedAt time.Time `db:"created_at"`
    UpdatedAt time.Time `db:"updated_at"`
}
