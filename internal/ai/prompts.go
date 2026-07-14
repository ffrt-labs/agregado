package ai

import "context"

// Operation names — the keys used for editable prompts and log rows.
const (
	OpScore      = "score"
	OpCategorize = "categorize"
	OpSummarize  = "summarize"
	OpDigest     = "digest"
	OpReason     = "reason"
)

// DefaultPrompts are the in-code fallback system prompts, used when the DB has no
// row for an operation (or the store errors). They mirror the migration 000011
// seeds. The categorize default deliberately omits the slug list — the live tags
// are appended at call time (see CloudflareProvider.Categorize).
var DefaultPrompts = map[string]string{
	OpScore:      "You are a content score giver. Given an article title and content, return only a number 1-5. 1=spam/trivial, 3=worth reading, 5=essential global significance. Return only the integer.",
	OpCategorize: "You are a content classifier. Given an article title and content, return exactly one category slug from the list provided. Return only the slug — no explanation, no punctuation.",
	OpSummarize:  "You are a news digest assistant. Given a list of articles — each with a title, a one-line reason it matters, and a short excerpt of its actual content — write a 2-3 sentence summary capturing the key themes. Be concise and direct.",
	OpDigest:     "You are a news digest assistant. Write a 2-sentence introduction for a daily digest email. Mention the main themes and why they matter. Be concise and direct. No bullet points.",
	OpReason:     "You are a news analyst. Given an article title and content, explain in one short sentence (max 20 words) why this article matters to a curious reader. Return only that sentence — no preamble, no quotes, no explanation of your reasoning.",
}

// PromptStore supplies editable system prompts by operation. Implemented by the
// storage prompt repo; kept as an interface here so internal/ai never imports
// internal/storage.
type PromptStore interface {
	SystemPrompt(ctx context.Context, operation string) (string, error)
}

// TagLister supplies the live category slugs injected into the categorize prompt.
type TagLister interface {
	CategorySlugs(ctx context.Context) ([]string, error)
}

// LogEntry captures one completed AI call for the logs table.
type LogEntry struct {
	Operation    string
	Model        string
	SystemPrompt string
	UserPrompt   string
	Response     string
	Success      bool
	Err          string
	DurationMs   int
}

// AILogSink records a completed AI call. Implementations decide whether logging
// is currently enabled and no-op when it is off.
type AILogSink interface {
	Record(ctx context.Context, entry LogEntry)
}
