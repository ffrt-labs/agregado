package articleindex

import (
	"context"

	"github.com/felipeafreitas/agregado/internal/ai"
)

// CloudflareModel adapts the existing cheap-tier provider to the Article Index
// pipeline. It intentionally leaves extraction outside the model boundary.
type CloudflareModel struct {
	provider interface {
		SummarizeArticle(context.Context, string, string) (string, error)
		Categorize(context.Context, string, string) (string, error)
		ScoreWithPreferences(context.Context, string, string, string) (int, error)
	}
}

func NewCloudflareModel(provider *ai.CloudflareProvider) *CloudflareModel {
	return &CloudflareModel{provider: provider}
}

// Summarize returns the raw structured-claims response; Service validates it.
func (m *CloudflareModel) Summarize(ctx context.Context, title, content string) (string, error) {
	return m.provider.SummarizeArticle(ctx, title, content)
}
func (m *CloudflareModel) Categorize(ctx context.Context, title, content string) (string, error) {
	return m.provider.Categorize(ctx, title, content)
}
func (m *CloudflareModel) Score(ctx context.Context, title, content, preferences string) (int, error) {
	return m.provider.ScoreWithPreferences(ctx, title, content, preferences)
}
