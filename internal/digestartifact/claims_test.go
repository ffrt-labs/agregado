package digestartifact

import (
	"strings"
	"testing"
)

func renderedSummary(summary string) (string, string) {
	return render(Artifact{Items: []Item{{
		Candidate: Candidate{Article: Article{ID: "a", Title: "Title A", Summary: summary}},
		ReadURL:   "https://x/r/a", UpvoteURL: "https://x/f/a/up", DownvoteURL: "https://x/f/a/down",
	}}})
}

func TestRenderShowsUpToTwoClaimsFromStructuredSummary(t *testing.T) {
	html, text := renderedSummary(`{"kind":"news","claims":["First claim.","Second claim.","Third claim."],"entities":["Acme"]}`)
	for _, out := range []string{html, text} {
		if !strings.Contains(out, "First claim.") || !strings.Contains(out, "Second claim.") {
			t.Fatalf("want first two claims in %q", out)
		}
		if strings.Contains(out, "Third claim.") || strings.Contains(out, "Acme") {
			t.Fatalf("want only two claims, no entities, in %q", out)
		}
	}
}

func TestRenderShowsLeadExtractSentencesWithoutTheTitle(t *testing.T) {
	html, text := renderedSummary("Title A\nOne thing happened. Another thing followed! A third thing came later.")
	for _, out := range []string{html, text} {
		if !strings.Contains(out, "One thing happened.") || !strings.Contains(out, "Another thing followed!") {
			t.Fatalf("want first two sentences in %q", out)
		}
		if strings.Contains(out, "third thing") {
			t.Fatalf("want only two claims in %q", out)
		}
	}
	if strings.Count(text, "Title A") != 1 {
		t.Fatalf("title must appear once, not repeated as a claim: %q", text)
	}
}

func TestRenderStripsPartialMarker(t *testing.T) {
	for _, summary := range []string{
		"[content: partial]\n" + `{"kind":"news","claims":["Partial claim."],"entities":[]}`,
		"[content: partial]\nTitle A\nPartial claim.",
	} {
		html, text := renderedSummary(summary)
		for _, out := range []string{html, text} {
			if !strings.Contains(out, "Partial claim.") || strings.Contains(out, "content: partial") {
				t.Fatalf("want claim without marker in %q", out)
			}
		}
	}
}

func TestRenderDropsTruncationClaims(t *testing.T) {
	html, text := renderedSummary(`{"kind":"news","claims":["The article is truncated.","Real claim.","This is only a stub."],"entities":[]}`)
	for _, out := range []string{html, text} {
		if !strings.Contains(out, "Real claim.") || strings.Contains(strings.ToLower(out), "truncated") || strings.Contains(out, "stub") {
			t.Fatalf("want only the real claim in %q", out)
		}
	}
}

func TestRenderWithNoClaimsLeftIsTitleAndLinksOnly(t *testing.T) {
	html, text := renderedSummary(`{"kind":"other","claims":["The article is truncated."],"entities":[]}`)
	if strings.Contains(html, "<p>") || strings.Contains(strings.ToLower(html+text), "truncated") {
		t.Fatalf("want no claim block: %q %q", html, text)
	}
	if want := "Title A\nhttps://x/r/a\n👍 https://x/f/a/up  👎 https://x/f/a/down\n\n"; text != want {
		t.Fatalf("text = %q, want %q", text, want)
	}
}
