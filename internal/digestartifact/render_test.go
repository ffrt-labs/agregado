package digestartifact

import (
	"html"
	"regexp"
	"strings"
	"testing"
	"time"
)

var markup, spaces = regexp.MustCompile(`(?s)<!--.*?-->|<style.*?</style>|<[^>]+>`), regexp.MustCompile(`\s+`)

// visible is the HTML part as a reader sees it, so assertions survive markup
// and styling changes from a design round.
func visible(h string) string {
	return strings.TrimSpace(spaces.ReplaceAllString(html.UnescapeString(markup.ReplaceAllString(h, " ")), " "))
}

func renderedItem(a Article, exploration bool) (string, string) {
	return Render(Artifact{
		Date: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), CandidateCount: 9, SelectedCount: 1,
		Items: []Item{{
			Candidate: Candidate{Article: a, Exploration: exploration},
			ReadURL:   "https://x/r/" + a.ID, UpvoteURL: "https://x/f/" + a.ID + "/up", DownvoteURL: "https://x/f/" + a.ID + "/down",
		}},
	})
}

func TestRenderShowsScoreAsDotsAndWord(t *testing.T) {
	for _, tc := range []struct {
		score      int
		html, text string
	}{
		{3, "●●●○○ mid", "3/5"},
		{4, "●●●●○ high", "4/5"},
		{5, "●●●●● top", "5/5"},
	} {
		h, text := renderedItem(Article{ID: "a", CanonicalURL: "https://a.example/x", Title: "A", Score: tc.score}, false)
		if !strings.Contains(visible(h), tc.html) {
			t.Errorf("score %d: html lacks %q", tc.score, tc.html)
		}
		if !strings.Contains(text, tc.text) {
			t.Errorf("score %d: text lacks %q:\n%s", tc.score, tc.text, text)
		}
	}
}

func TestRenderShowsHostWithoutWWWAndFallsBackToAuthor(t *testing.T) {
	h, text := renderedItem(Article{ID: "a", CanonicalURL: "https://www.example.com/post?x=1", Title: "A", Score: 4}, false)
	for _, out := range []string{h, text} {
		if !strings.Contains(out, "example.com") || strings.Contains(out, "www.example.com") {
			t.Fatalf("want bare host example.com in %q", out)
		}
	}
	h, text = renderedItem(Article{ID: "a", CanonicalURL: "not a url", Title: "A", Score: 4, Author: "Jane Doe"}, false)
	for _, out := range []string{h, text} {
		if !strings.Contains(out, "Jane Doe") {
			t.Fatalf("want author fallback in %q", out)
		}
	}
}

func TestRenderShowsKindAndAllTopics(t *testing.T) {
	h, text := renderedItem(Article{ID: "a", CanonicalURL: "https://a.example/x", Title: "A", Score: 4,
		Summary: `{"kind":"analysis","claims":["One."],"entities":[]}`, Topics: []string{"AI", "data"}}, false)
	if want := "01 · a.example · Analysis · AI, data · ●●●●○ high"; !strings.Contains(visible(h), want) {
		t.Fatalf("html meta line lacks %q:\n%s", want, visible(h))
	}
	if want := "01 [AI · data · analysis · 4/5 · a.example]"; !strings.Contains(text, want) {
		t.Fatalf("text meta line lacks %q:\n%s", want, text)
	}
}

func TestRenderLeavesOutMissingKindAndTopics(t *testing.T) {
	h, text := renderedItem(Article{ID: "a", CanonicalURL: "https://a.example/x", Title: "A", Score: 3,
		Summary: "A\nA lead extract has no kind."}, false)
	if want := "01 · a.example · ●●●○○ mid"; !strings.Contains(visible(h), want) {
		t.Fatalf("html meta line lacks %q:\n%s", want, visible(h))
	}
	if want := "01 [3/5 · a.example]"; !strings.Contains(text, want) {
		t.Fatalf("text meta line lacks %q:\n%s", want, text)
	}
}

func TestRenderFlagsExplorationPicks(t *testing.T) {
	h, text := renderedItem(Article{ID: "a", CanonicalURL: "https://a.example/x", Title: "A", Score: 2}, true)
	if want := "Exploration · 01 · a.example · ●●○○○ low"; !strings.Contains(visible(h), want) {
		t.Fatalf("html meta line lacks %q:\n%s", want, visible(h))
	}
	if want := "01 EXPLORATION [2/5 · a.example]"; !strings.Contains(text, want) {
		t.Fatalf("text meta line lacks %q:\n%s", want, text)
	}
	plain, _ := renderedItem(Article{ID: "a", CanonicalURL: "https://a.example/x", Title: "A", Score: 4}, false)
	if strings.Contains(visible(plain), "Exploration ·") {
		t.Fatal("a regular pick must not be flagged as Exploration")
	}
}

func TestRenderLinksTitleAndVoteButtons(t *testing.T) {
	h, text := renderedItem(Article{ID: "a", CanonicalURL: "https://a.example/x", Title: "A", Score: 4}, false)
	for _, want := range []string{
		`href="https://x/r/a"`,
		`href="https://x/f/a/up" target="_blank" aria-label="More like this"`,
		`href="https://x/f/a/down" target="_blank" aria-label="Less like this"`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("html lacks %s", want)
		}
	}
	for _, want := range []string{"Read: https://x/r/a", "▲ https://x/f/a/up", "▼ https://x/f/a/down"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q", want)
		}
	}
	if strings.Contains(h, `href="https://x/"`) || strings.Contains(h, "/sources") {
		t.Error("the email must not link / or /sources: the tunnel blocks them (ADR 0001)")
	}
}

func TestRenderEscapesArticleTextInHTMLOnly(t *testing.T) {
	h, text := renderedItem(Article{ID: "a", CanonicalURL: "https://a.example/x", Title: `<script>alert("x")</script> & co`, Score: 4,
		Summary: `{"kind":"<b>news</b>","claims":["Tom & Jerry <i>fight</i>."],"entities":[]}`, Topics: []string{"<AI>"}}, false)
	for _, raw := range []string{"<script>", "<b>news", "<i>fight", "<AI>"} {
		if strings.Contains(h, raw) {
			t.Errorf("html contains unescaped %q", raw)
		}
	}
	for _, escaped := range []string{"&lt;script&gt;", "&amp; co", "Tom &amp; Jerry &lt;i&gt;fight&lt;/i&gt;.", "&lt;AI&gt;"} {
		if !strings.Contains(h, escaped) {
			t.Errorf("html lacks escaped %q", escaped)
		}
	}
	if !strings.Contains(text, `<script>alert("x")</script> & co`) || !strings.Contains(text, "Tom & Jerry <i>fight</i>.") {
		t.Errorf("text part must stay unescaped:\n%s", text)
	}
}

func TestRenderHeaderCountsClearedStories(t *testing.T) {
	h, text := renderedItem(Article{ID: "a", CanonicalURL: "https://a.example/x", Title: "A", Score: 4}, false)
	for _, out := range []string{visible(h), text} {
		for _, want := range []string{"Agregado · Daily Digest", "Saturday, November 14, 2026", "1 of 9 stories cleared the bar"} {
			if !strings.Contains(out, want) {
				t.Errorf("header lacks %q in %q", want, out)
			}
		}
	}
}

func TestRenderEmptyDigestSaysWhy(t *testing.T) {
	h, text := Render(Artifact{Date: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC), EmptyReason: "Nothing <finished> today."})
	if !strings.Contains(visible(h), "Nothing <finished> today.") || !strings.Contains(h, "&lt;finished&gt;") {
		t.Fatalf("html must show the escaped reason:\n%s", visible(h))
	}
	if !strings.Contains(text, "Nothing <finished> today.") || strings.Contains(text, "Read:") {
		t.Fatalf("text must show the reason and no items:\n%s", text)
	}
}
