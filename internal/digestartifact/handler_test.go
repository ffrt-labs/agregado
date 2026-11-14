package digestartifact

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// pickFrontier selects only the given Article IDs, so the Digest's selected
// count differs from its candidate count.
type pickFrontier struct{ ids []string }

func (f pickFrontier) Select(_ context.Context, _ []Candidate) ([]Choice, error) {
	choices := make([]Choice, len(f.ids))
	for i, id := range f.ids {
		choices[i] = Choice{ArticleID: id}
	}
	return choices, nil
}

// The email n8n sends comes from POST /api/private/digests/{date}: the
// stored Articles must reach the rendered HTML and text with their own
// score, kind, topics, title, claims and links, under the real counts.
func TestHandlerRendersTheDigestEmailFromStoredArticles(t *testing.T) {
	store := &memoryStore{artifacts: map[string]Artifact{}, articles: []Article{
		{ID: "a1", CanonicalURL: "https://www.alpha.example/post", Title: "Alpha <rises>", Score: 5,
			Summary: `{"kind":"analysis","claims":["Alpha claim one.","Alpha claim two.","Alpha claim three."],"entities":[]}`,
			Topics:  []string{"AI", "data"}},
		{ID: "b2", CanonicalURL: "https://beta.example/b", Title: "Beta", Score: 4,
			Summary: `{"kind":"news","claims":["Beta claim."],"entities":[]}`, Topics: []string{"science"}},
		{ID: "c3", CanonicalURL: "https://gamma.example/c", Title: "Gamma", Score: 1,
			Summary: "Gamma\nGamma lead sentence. Second sentence."},
		{ID: "d4", CanonicalURL: "https://delta.example/d", Title: "Delta", Score: 1},
	}}
	service := NewService(store, pickFrontier{ids: []string{"a1", "c3"}}, "https://agregado.example/", 3, 10)
	handler := NewHandler("secret", service)

	req := httptest.NewRequest(http.MethodPost, "/api/private/digests/2026-11-14", nil)
	req.Header.Set("X-Enrichment-Secret", "secret")
	response := httptest.NewRecorder()
	handler.Handle(response, req)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", response.Code, response.Body.String())
	}
	var artifact struct{ HTML, Text string }
	if err := json.NewDecoder(response.Body).Decode(&artifact); err != nil {
		t.Fatal(err)
	}
	html := visible(artifact.HTML)

	for _, want := range []string{
		"Saturday, November 14, 2026",
		"2 of 4 stories cleared the bar",
		"01 · alpha.example · Analysis · AI, data · ●●●●● top",
		"Alpha <rises>", "Alpha claim one.", "Alpha claim two.",
		"Exploration · 02 · gamma.example · ●○○○○ low",
		"Gamma lead sentence.",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("html lacks %q:\n%s", want, html)
		}
	}
	for _, link := range []string{
		`href="https://agregado.example/r/a1"`, `href="https://agregado.example/f/a1/up"`, `href="https://agregado.example/f/a1/down"`,
		`href="https://agregado.example/r/c3"`, `href="https://agregado.example/f/c3/up"`, `href="https://agregado.example/f/c3/down"`,
	} {
		if !strings.Contains(artifact.HTML, link) {
			t.Errorf("html lacks %s", link)
		}
	}
	for _, want := range []string{
		"2 of 4 stories cleared the bar",
		"01 [AI · data · analysis · 5/5 · alpha.example]\nAlpha <rises>\n- Alpha claim one.\n- Alpha claim two.\nRead: https://agregado.example/r/a1\n▲ https://agregado.example/f/a1/up\n▼ https://agregado.example/f/a1/down",
		"02 EXPLORATION [1/5 · gamma.example]\nGamma\n- Gamma lead sentence.",
	} {
		if !strings.Contains(artifact.Text, want) {
			t.Errorf("text lacks %q:\n%s", want, artifact.Text)
		}
	}
	for _, unselected := range []string{"Beta", "Delta", "Alpha claim three."} {
		if strings.Contains(artifact.HTML+artifact.Text, unselected) {
			t.Errorf("digest shows %q, which was not selected", unselected)
		}
	}
}
