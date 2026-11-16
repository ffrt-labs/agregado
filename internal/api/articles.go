package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/go-chi/chi/v5"
)

// ArticleOpener is what Open, the fallback for pre-Article-Index links, needs:
// find the Article, then mark it read.
type ArticleOpener interface {
	GetById(ctx context.Context, id string) (*domain.Article, error)
	MarkRead(ctx context.Context, id string) error
}

type ArticleHandler struct {
	articles ArticleOpener
}

func NewArticleHandler(articles ArticleOpener) *ArticleHandler {
	return &ArticleHandler{articles: articles}
}

// webURL returns the article's real web home and whether one exists. Precedence
// (issue #2, preserved through Phase 21): the canonical URL extracted at parse
// time, else external_url when it is a real page. A newsletter with no web
// version has a nil canonical URL and a nil external_url (Phase 21 replaced the
// newsletter:<uuid> sentinel with NULL), so it returns ("", false). Type is not
// consulted here — both URL fields being empty is itself the answer.
func webURL(a *domain.Article) (string, bool) {
	if a.CanonicalURL != nil && *a.CanonicalURL != "" {
		return *a.CanonicalURL, true
	}
	if a.ExternalURL != nil && *a.ExternalURL != "" {
		return *a.ExternalURL, true
	}
	return "", false
}

// Open is the fallback for pre-Article-Index links (GET /r/{id}) — ids the
// Article Index does not know: it marks the article read and redirects to its
// web home. The in-app reader page was removed (#238), so an Article with no
// web home has nowhere to go and is a 404.
func (a *ArticleHandler) Open(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	article, err := a.articles.GetById(r.Context(), id)
	if err != nil {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	target, ok := webURL(article)
	if !ok {
		http.Error(w, "Not Found", http.StatusNotFound)
		return
	}

	if err := a.articles.MarkRead(r.Context(), id); err != nil {
		slog.Error("articles: mark read failed", "component", "api", "article_id", id, "err", err)
	}

	http.Redirect(w, r, target, http.StatusFound)
}
