package digestartifact

import "time"

// NewMemoryStore exposes the in-memory fake Store to the external
// digestartifact_test package, whose trace tests drive the Service with a
// real ai.CloudflareProvider (ai imports digestartifact, so they cannot live
// in this package).
func NewMemoryStore(articles []Article, since, until time.Time) Store {
	return &memoryStore{articles: articles, since: since, until: until, artifacts: map[string]Artifact{}}
}
