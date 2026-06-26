package digest

import (
	"context"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/felipeafreitas/agregado/internal/domain"
)

type ArticleQuerier interface {
	FindUnreadSince(ctx context.Context, since time.Time, minScore, limit int) ([]domain.Article, error)
}

type TagQuerier interface {
	FindAll(ctx context.Context) ([]domain.Tag, error)
}

type Categorizer interface {
	Categorize(ctx context.Context, title, content string) (string, error)
}

type Ranker struct {
	articles           ArticleQuerier
	tags               TagQuerier
	maxArticles        int
	minRelevanceScore  int
	categorizer        Categorizer // optional; nil = skip AI categorization
}

type TaggedArticles struct {
	Tag 		*domain.Tag
	Articles 	[]domain.Article
	Summary		string
}

func NewRanker(articles ArticleQuerier, tags TagQuerier, maxArticles int, minRelevanceScore int, categorizer Categorizer) *Ranker {
	return &Ranker{
		articles:          articles,
		tags:              tags,
		maxArticles:       maxArticles,
		minRelevanceScore: minRelevanceScore,
		categorizer:       categorizer,
	}
}

func (r *Ranker) GetDigestArticles(ctx context.Context, lookbackHours int) ([]TaggedArticles, error) {
	since := time.Now().Add(-time.Duration(lookbackHours) * time.Hour)

	articles, err := r.articles.FindUnreadSince(ctx, since, r.minRelevanceScore, r.maxArticles)
	if err != nil {
		return nil, err
	}

	tags, err := r.tags.FindAll(ctx)
	if err != nil {
		return nil, err
	}

	if r.categorizer != nil {
		slugToTag := make(map[string]domain.Tag, len(tags))
		for _, t := range tags {
			slugToTag[t.Slug] = t
		}
		for i, article := range articles {
			if len(article.Tags) > 0 {
				continue
			}
			text := ""
			if article.Content != nil {
				text = *article.Content
			} else if article.Summary != nil {
				text = *article.Summary
			}
			slug, err := r.categorizer.Categorize(ctx, article.Title, text)
			if err != nil {
				log.Printf("categorize %q: %v", article.Title, err)
				continue
			}
			if tag, ok := slugToTag[strings.TrimSpace(strings.ToLower(slug))]; ok {
				articles[i].Tags = []domain.Tag{tag}
			}
		}
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
