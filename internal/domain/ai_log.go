package domain

import "time"

// AILog is a persisted record of a single AI request/response.
// Nullable columns are pointers so pgx can scan NULLs from a failed call.
type AILog struct {
	ID           string    `db:"id"`
	Operation    string    `db:"operation"`
	Model        *string   `db:"model"`
	SystemPrompt *string   `db:"system_prompt"`
	UserPrompt   *string   `db:"user_prompt"`
	Response     *string   `db:"response"`
	Success      bool      `db:"success"`
	Error        *string   `db:"error"`
	DurationMs   *int      `db:"duration_ms"`
	CreatedAt    time.Time `db:"created_at"`
}
