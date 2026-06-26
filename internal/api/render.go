package api

import (
	"html/template"
	"net/http"
	"strings"
)

type NavData struct {
	ArticleCount  int
	SourceCount   int
	BookmarkCount int
	ClearedCount  int
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
	tmpl.ExecuteTemplate(w, "layout.html", data)
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
