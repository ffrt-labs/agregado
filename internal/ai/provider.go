package ai

import (
	"context"

	"github.com/felipeafreitas/agregado/internal/domain"
)

type Provider interface {
	Summarize	(ctx context.Context, articles []domain.Article) (string, error)
	Categorize	(ctx context.Context, title, content string) (string, error)
}
