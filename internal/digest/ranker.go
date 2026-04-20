package digest

import (
	"context"
	"sort"
	"time"

	"github.com/felipeafreitas/agregado/internal/domain"
)

type ArticleQuerier interface {
	FindUnreadSince(ctx context.Context, since time.Time) ([]domain.Article, error)
}

type TagQuerier interface {
	FindAll(ctx context.Context) ([]domain.Tag, error)
}

type Ranker struct {
	articles ArticleQuerier
	tags TagQuerier
	maxArticles int
}

type TaggedArticles struct {
	Tag *domain.Tag
	Articles []domain.Article
}

func NewRanker(articles ArticleQuerier, tags TagQuerier, maxArticles int) *Ranker {
	return &Ranker{
		articles:articles,
		tags:tags,
		maxArticles: maxArticles,
	}
}

func (r *Ranker) GetDigestArticles(ctx context.Context, lookbackHours int) ([]TaggedArticles, error) {
	since := time.Now().Add(-time.Duration(lookbackHours))

	articles, err := r.articles.FindUnreadSince(ctx, since)
	if err != nil {
		return nil, err
	}

	tags, err := r.tags.FindAll(ctx)
	if err != nil {
		return nil, err
	}

	articlesGrouppedByTag := make(map[string][]domain.Article)
	for _, article := range articles {
		if len(article.Tags) == 0 {
			articlesGrouppedByTag["uncategorized"] = append(articlesGrouppedByTag["uncathegorized"], article)
			continue
		}

		for _, tag := range article.Tags {
			articlesGrouppedByTag[tag.ID] = append(articlesGrouppedByTag[tag.ID], article)
		}
	}

	var taggedArticles []TaggedArticles

	for _, tag := range tags {
		articles, ok := articlesGrouppedByTag[tag.ID]
		if !ok {
			continue
		}

		sort.Slice(articles, func(i, j int) bool {
			nextArticlePublishedAt := *articles[j].PublishedAt
			if nextArticlePublishedAt == nil {
				return true
			}

			return articles[i].PublishedAt.After(nextArticlePublishedAt)
		})

		taggedArticles = append(taggedArticles, TaggedArticles{
			Tag: &tag,
			Articles: articles,
		})
	}

	if (len(articlesGrouppedByTag["uncategorized"]) > 0) {
		sort.Slice(articlesGrouppedByTag["uncategorized"], func(i, j int) bool {
			nextArticlePublishedAt := *articles[j].PublishedAt
			if nextArticlePublishedAt == nil {
				return true
			}

			return articles[i].PublishedAt.After(nextArticlePublishedAt)
		})

		taggedArticles = append(articlesGrouppedByTag["uncategorized"], TaggedArticles{
			Tag: &tag,
			Articles: articles,
		})
	}

	sort.Slice(taggedArticles, func(a, b int) bool {
		return taggedArticles[a].Tag.Name > taggedArticles[b].Tag.Name
	})

	return taggedArticles, nil
}
