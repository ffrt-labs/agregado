package ai

import (
	"context"
	"slices"
	"strings"
)

// CategorizeChecked is Categorize plus whether the answer is one of the live
// allowed slugs (the slug_allowed code check). The answer is returned as-is:
// the check observes, it never rewrites. A disallowed slug is not an error.
//
// It re-reads the slug list rather than threading it through Categorize, so
// Categorize stays unchanged for its other callers; a tag edited between the
// two reads can at worst mis-score one Article.
func (p *CloudflareProvider) CategorizeChecked(ctx context.Context, title, content string) (slug string, allowed bool, err error) {
	if slug, err = p.Categorize(ctx, title, content); err != nil {
		return slug, false, err
	}
	// Same normalisation the tag lookup applies (storage.categorizeArticle).
	return slug, slices.Contains(p.categorySlugs(ctx), strings.TrimSpace(strings.ToLower(slug))), nil
}
