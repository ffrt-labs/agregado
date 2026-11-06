package ai

import (
	"context"
	"slices"
	"strings"

	"github.com/felipeafreitas/agregado/internal/textutil"
)

// CategorizeChecked is Categorize plus whether the answer is one of the
// allowed slugs the prompt offered (the slug_allowed code check). The answer
// is returned as-is: the check observes, it never rewrites. A disallowed slug
// is not an error.
func (p *CloudflareProvider) CategorizeChecked(ctx context.Context, title, content string) (slug string, allowed bool, err error) {
	return p.categorize(ctx, title, content)
}

// categorize reads the live tag slugs once, so the prompt and the
// slug_allowed check always see the same list.
func (p *CloudflareProvider) categorize(ctx context.Context, title, content string) (slug string, allowed bool, err error) {
	// The live tag slugs are a variable so the classifier can only pick from
	// current tags, however a prompt version phrases the list.
	slugs := p.categorySlugs(ctx)
	slug, err = p.completeTemplate(ctx, OpCategorize, map[string]string{
		"allowed_slugs": strings.Join(slugs, ", "),
		"title":         title,
		"content":       textutil.Clean(content, p.maxContentChars),
	}, shortAnswerOptions)
	if err != nil {
		return slug, false, err
	}
	// Same normalisation the tag lookup applies (storage.categorizeArticle).
	return slug, slices.Contains(slugs, strings.TrimSpace(strings.ToLower(slug))), nil
}
