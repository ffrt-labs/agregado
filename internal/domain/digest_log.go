package domain

import "time"

type DigestLog struct {
	ID 				string		`db:"id"`
	SentAt			time.Time	`db:"sent_at"`
	ArticleCount	int			`db:"article_count"`
	RecipientEmail	string		`db:"recipient_email"`
}
