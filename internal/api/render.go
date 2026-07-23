package api

import (
	"context"
	"html/template"
	"log"
	"net/http"

	"github.com/felipeafreitas/agregado/internal/tmplfunc"
)

type NavData struct {
	ArticleCount     int
	SourceCount      int
	BookmarkCount    int
	ScoredTodayCount int
}

type NavQuerier interface {
	Count(ctx context.Context) (int, error)
	CountAboveScore(ctx context.Context, minScore int) (int, error)
	CountSaved(ctx context.Context) (int, error)
}

type NavBuilder struct {
	articles NavQuerier
	sources  SourceLister
	minScore int
}

func NewNavBuilder(articles NavQuerier, sources SourceLister, minScore int) *NavBuilder {
	return &NavBuilder{articles: articles, sources: sources, minScore: minScore}
}

func (n *NavBuilder) Build(ctx context.Context) NavData {
	articleCount, _ := n.articles.Count(ctx)
	scoredToday, _ := n.articles.CountAboveScore(ctx, n.minScore)
	sources, _ := n.sources.List(ctx, 999, 0)
	bookmarkCount, _ := n.articles.CountSaved(ctx)
	return NavData{
		ArticleCount:     articleCount,
		SourceCount:      len(sources),
		BookmarkCount:    bookmarkCount,
		ScoredTodayCount: scoredToday,
	}
}

// render wraps a content template in the full app shell (sidebar + nav).
func render(w http.ResponseWriter, filename string, data any) {
	renderWithLayout(w, "layout.html", filename, data)
}

// renderReader wraps a content template in the sidebar-free reader shell, used
// by the public /articles/{id} page so it leaks no nav counts or admin links
// off-network (issue #2).
func renderReader(w http.ResponseWriter, filename string, data any) {
	renderWithLayout(w, "layout_reader.html", filename, data)
}

// renderWithLayout parses the chosen layout, the shared head partial, and the
// content template, then executes the layout. The layout basename doubles as
// the top-level template name ExecuteTemplate targets.
func renderWithLayout(w http.ResponseWriter, layout, filename string, data any) {
	tmpl, err := template.New(layout).Funcs(tmplfunc.Map).ParseFiles(
		"templates/"+layout,
		"templates/head.html",
		"templates/"+filename,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.ExecuteTemplate(w, layout, data); err != nil {
		log.Printf("template execute error (%s): %v", filename, err)
	}
}

func renderPartial(w http.ResponseWriter, filename string, name string, data any) {
	tmpl, err := template.New(name).Funcs(tmplfunc.Map).ParseFiles("templates/" + filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl.ExecuteTemplate(w, name, data)
}
