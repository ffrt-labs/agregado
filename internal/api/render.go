package api

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"strings"
)

type NavData struct {
	ArticleCount  int
	SourceCount   int
	BookmarkCount int
	ClearedCount  int
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
	cleared, _ := n.articles.CountAboveScore(ctx, n.minScore)
	sources, _ := n.sources.List(ctx, 999, 0)
	bookmarkCount, _ := n.articles.CountSaved(ctx)
	return NavData{
		ArticleCount:  articleCount,
		SourceCount:   len(sources),
		BookmarkCount: bookmarkCount,
		ClearedCount:  cleared,
	}
}

var funcMap = template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"dots": func(score *int) string {
		if score == nil {
			return ""
		}
		s := *score
		if s < 1 {
			s = 1
		}
		if s > 5 {
			s = 5
		}
		return strings.Repeat("●", s) + strings.Repeat("○", 5-s)
	},
	"scoreLabel": func(score *int) string {
		if score == nil {
			return ""
		}
		labels := map[int]string{1: "noise", 2: "low", 3: "mid", 4: "high", 5: "top"}
		if l, ok := labels[*score]; ok {
			return l
		}
		return ""
	},
}

func render(w http.ResponseWriter, filename string, data any) {
	tmpl, err := template.New("layout.html").Funcs(funcMap).ParseFiles(
		"templates/layout.html",
		"templates/"+filename,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html")
	if err := tmpl.ExecuteTemplate(w, "layout.html", data); err != nil {
		log.Printf("template execute error (%s): %v", filename, err)
	}
}

func renderPartial(w http.ResponseWriter, filename string, name string, data any) {
	tmpl, err := template.New(name).Funcs(funcMap).ParseFiles("templates/" + filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl.ExecuteTemplate(w, name, data)
}
