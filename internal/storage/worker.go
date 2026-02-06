package storage

import (
	"context"
	"encoding/json"

	"github.com/felipeafreitas/agregado/internal/domain"
)

type ArticleCreator interface {
	Create(ctx context.Context, article domain.Article) error
}

func NewWorker(repo ArticleCreator) func([]byte) error {
	return func(body []byte) error {
		var article domain.Article
		if err := json.Unmarshal(body, &article); err != nil {
			return err
		}
		if err := repo.Create(context.Background(), article); err != nil {
			return err
		}

		return nil
	}
}
