package api

import (
	"html/template"
	"net/http"
)

func render(w http.ResponseWriter, filename string, data any) {
	tmpl, err := template.ParseFiles("templates/layout.html", "templates/"+filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl.ExecuteTemplate(w, "layout.html", data)
}

func renderPartial(w http.ResponseWriter, filename string, name string, data any) {
	tmpl, err := template.ParseFiles("templates/"+filename)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl.ExecuteTemplate(w, name, data)
}
