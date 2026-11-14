// Command digest-preview renders the Digest email from fixture data, with no
// database or model call, so the template can be checked in a browser:
//
//	go run ./cmd/digest-preview > preview.html
//	go run ./cmd/digest-preview -empty > preview.html
//	go run ./cmd/digest-preview -text
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/felipeafreitas/agregado/internal/digestartifact"
)

const baseURL = "https://agregado.example"

func main() {
	empty := flag.Bool("empty", false, "render the empty Digest")
	text := flag.Bool("text", false, "print the plain-text part instead of the HTML")
	flag.Parse()

	html, plain := digestartifact.Render(fixture(*empty))
	if *text {
		fmt.Fprint(os.Stdout, plain)
		return
	}
	fmt.Fprint(os.Stdout, html)
}

func fixture(empty bool) digestartifact.Artifact {
	a := digestartifact.Artifact{Date: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), CandidateCount: 37}
	if empty {
		a.EmptyReason = "No Articles finished Enrichment between Nov 13 08:00 and Nov 14 08:00."
		return a
	}
	items := []digestartifact.Candidate{
		// Score 5, several topics, structured Summary.
		{Article: digestartifact.Article{ID: "1", Score: 5, CanonicalURL: "https://www.example.com/2026/11/frontier-models",
			Title:   "Frontier labs quietly shift from scale to post-training",
			Summary: `{"kind":"analysis","claims":["Three of the four largest labs now spend more on post-training than on pre-training runs.","The shift shows up first in release notes, not in benchmarks."],"entities":["OpenAI","Anthropic"]}`,
			Topics:  []string{"AI", "industry", "research"}}},
		// Score 4, one topic, partial content marker.
		{Article: digestartifact.Article{ID: "2", Score: 4, CanonicalURL: "https://news.example.org/postgres-18",
			Title:   "Postgres 18 ships asynchronous I/O & <virtual> generated columns",
			Summary: "[content: partial]\n" + `{"kind":"news","claims":["Async I/O lands behind a setting that defaults to worker mode.","Virtual generated columns become the default."],"entities":["PostgreSQL"]}`,
			Topics:  []string{"data"}}},
		// Score 3, no topics, lead extract (no kind).
		{Article: digestartifact.Article{ID: "3", Score: 3, CanonicalURL: "https://blog.example.net/notes/week-46",
			Title:   "Weeknotes: rebuilding the garden shed",
			Summary: "Weeknotes: rebuilding the garden shed\nThe old roof finally gave in. This week was mostly about choosing between felt and metal sheets. Metal won."}},
		// Exploration pick below the floor; the URL can't be parsed, so the author shows.
		{Exploration: true, Article: digestartifact.Article{ID: "4", Score: 2, CanonicalURL: "::not a url", Author: "Jane Doe",
			Title:   "Why medieval bridges outlast modern ones",
			Summary: `{"kind":"opinion","claims":["Masonry arches carry load in compression, which stone handles for centuries."],"entities":[]}`,
			Topics:  []string{"history", "engineering"}}},
	}
	for _, c := range items {
		a.Items = append(a.Items, digestartifact.Item{Candidate: c,
			ReadURL: baseURL + "/r/" + c.ID, UpvoteURL: baseURL + "/f/" + c.ID + "/up", DownvoteURL: baseURL + "/f/" + c.ID + "/down"})
	}
	a.SelectedCount = len(a.Items)
	return a
}
