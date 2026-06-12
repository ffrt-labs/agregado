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
	Tag 		*domain.Tag
	Articles 	[]domain.Article
	Summary		string
}

func NewRanker(articles ArticleQuerier, tags TagQuerier, maxArticles int) *Ranker {
	return &Ranker{
		articles:articles,
		tags:tags,
		maxArticles: maxArticles,
	}
}

func (r *Ranker) GetDigestArticles(ctx context.Context, lookbackHours int) ([]TaggedArticles, error) {
	since := time.Now().Add(-time.Duration(lookbackHours) * time.Hour)

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
			articlesGrouppedByTag["uncategorized"] = append(articlesGrouppedByTag["uncategorized"], article)
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
			if articles[j].PublishedAt == nil {
				return true
			}

			if articles[i].PublishedAt == nil {
				return false
			}

			nextArticlePublishedAt := *articles[j].PublishedAt
			return articles[i].PublishedAt.After(nextArticlePublishedAt)
		})

		taggedArticles = append(taggedArticles, TaggedArticles{
			Tag: &tag,
			Articles: articles,
		})
	}

	if (len(articlesGrouppedByTag["uncategorized"]) > 0) {
		uncategorizedArticles := articlesGrouppedByTag["uncategorized"]

		sort.Slice(uncategorizedArticles, func(i, j int) bool {
			if uncategorizedArticles[j].PublishedAt == nil {
				return true
			}

			if uncategorizedArticles[i].PublishedAt == nil {
				return false
			}

			nextArticlePublishedAt := *uncategorizedArticles[j].PublishedAt
			return uncategorizedArticles[i].PublishedAt.After(nextArticlePublishedAt)
		})

		taggedArticles = append(taggedArticles, TaggedArticles{
			Tag: nil,
			Articles: uncategorizedArticles,
		})
	}

	sort.Slice(taggedArticles, func(a, b int) bool {
		if taggedArticles[a].Tag == nil {
			return false
		}

		return taggedArticles[a].Tag.Name > taggedArticles[b].Tag.Name
	})

	return taggedArticles, nil
}
