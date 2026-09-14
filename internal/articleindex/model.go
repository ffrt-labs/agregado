package articleindex

import (
	"context"

	"github.com/felipeafreitas/agregado/internal/ai"
	"github.com/felipeafreitas/agregado/internal/domain"
)

// CloudflareModel adapts the existing cheap-tier provider to the Article Index
// pipeline. It intentionally leaves extraction outside the model boundary.
type CloudflareModel struct {
	provider interface {
		Summarize(context.Context, []domain.Article) (string, error)
		Categorize(context.Context, string, string) (string, error)
		ScoreWithPreferences(context.Context, string, string, string) (int, error)
	}
}

func NewCloudflareModel(provider *ai.CloudflareProvider) *CloudflareModel {
	return &CloudflareModel{provider: provider}
}

func (m *CloudflareModel) Summarize(ctx context.Context, title, content string) (string, error) {
	return m.provider.Summarize(ctx, []domain.Article{{Title: title, Content: &content}})
}
func (m *CloudflareModel) Categorize(ctx context.Context, title, content string) (string, error) {
	return m.provider.Categorize(ctx, title, content)
}
func (m *CloudflareModel) Score(ctx context.Context, title, content, preferences string) (int, error) {
	return m.provider.ScoreWithPreferences(ctx, title, content, preferences)
}
