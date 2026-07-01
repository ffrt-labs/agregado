package domain

import "time"

// AIPrompt is an editable system prompt for one AI operation
// ('score' | 'categorize' | 'summarize' | 'digest').
type AIPrompt struct {
	Operation    string    `db:"operation"`
	SystemPrompt string    `db:"system_prompt"`
	UpdatedAt    time.Time `db:"updated_at"`
}
