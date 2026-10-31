package preferences

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/felipeafreitas/agregado/internal/preferenceexport"
)

// seed is the Google Doc's Markdown export as created: list items end in two
// spaces and the placeholders are still in place.
const seed = `# PREFERENCES

Agregado scores every Article against this profile.

## Topics I want

- (example: distributed systems and backend architecture)
- Go and Postgres internals

## Topics I skip

- (example: crypto price speculation)

## Sources I trust

- (example: a newsletter)

## Kinds of articles I skip

- (example: listicles)
`

func TestValidateAcceptsTheDocExportAndDropsTrailingSpaces(t *testing.T) {
	got, err := Validate(seed)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if strings.Contains(got, "  \n") {
		t.Fatalf("trailing spaces survived normalization:\n%s", got)
	}
}

func TestValidateRejectsAnythingButTheFourBulletSections(t *testing.T) {
	cases := map[string]string{
		"missing title":    strings.Replace(seed, "# PREFERENCES", "# Prefs", 1),
		"unknown section":  strings.Replace(seed, "## Sources I trust", "## Sources I love", 1),
		"missing section":  strings.Replace(seed, "## Sources I trust\n\n- (example: a newsletter)\n", "", 1),
		"duplicate":        seed + "\n## Topics I want\n\n- more\n",
		"prose in section": strings.Replace(seed, "- Go and Postgres internals", "Go and Postgres internals", 1),
		"subheading":       strings.Replace(seed, "- Go and Postgres internals", "### Go", 1),
		"table":            strings.Replace(seed, "- Go and Postgres internals", "| a | b |", 1),
		"too large":        strings.Replace(seed, "- Go and Postgres internals", "- "+strings.Repeat("x", MaxBytes), 1),
	}
	for name, text := range cases {
		if _, err := Validate(text); err == nil {
			t.Errorf("%s: Validate accepted an invalid profile", name)
		}
	}
}

func TestForModelDropsOnlyThePlaceholderBullets(t *testing.T) {
	got := ForModel(Normalize(seed))
	if strings.Contains(got, "(example:") {
		t.Fatalf("placeholder reached the model:\n%s", got)
	}
	if !strings.Contains(got, "- Go and Postgres internals") || !strings.Contains(got, "## Sources I trust") {
		t.Fatalf("real content was dropped:\n%s", got)
	}
}

type fakeStore struct {
	versions []string
	sources  []string
}

func (s *fakeStore) Current(context.Context) (string, error) {
	if len(s.versions) == 0 {
		return "", ErrNoProfile
	}
	return s.versions[len(s.versions)-1], nil
}

func (s *fakeStore) Save(_ context.Context, text, source string) (bool, error) {
	if len(s.versions) > 0 && s.versions[len(s.versions)-1] == text {
		return false, nil
	}
	s.versions, s.sources = append(s.versions, text), append(s.sources, source)
	return true, nil
}

type fakeSignals []preferenceexport.Signal

func (f fakeSignals) Signals(context.Context) ([]preferenceexport.Signal, error) { return f, nil }

type fakeRewriter struct {
	output   string
	calls    int
	evidence string
}

func (r *fakeRewriter) RegeneratePreferences(_ context.Context, _, evidence string) (string, error) {
	r.calls++
	r.evidence = evidence
	return r.output, nil
}

func TestReplaceStoresAnUneditedDocOnlyOnce(t *testing.T) {
	store := &fakeStore{}
	s := NewService(store, fakeSignals{}, &fakeRewriter{})
	for i, want := range []bool{true, false} {
		_, changed, err := s.Replace(context.Background(), seed)
		if err != nil || changed != want {
			t.Fatalf("Replace #%d: changed=%v err=%v, want changed=%v", i+1, changed, err, want)
		}
	}
	if store.sources[0] != SourceManual {
		t.Fatalf("source = %q, want %q", store.sources[0], SourceManual)
	}
}

func TestRegenerateWithoutEvidenceMakesNoModelCall(t *testing.T) {
	store := &fakeStore{versions: []string{Normalize(seed)}}
	rewriter := &fakeRewriter{}
	result, err := NewService(store, fakeSignals{}, rewriter).Regenerate(context.Background())
	if err != nil || result.Changed || rewriter.calls != 0 || result.Profile != Normalize(seed) {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, rewriter.calls)
	}
}

func TestRegenerateStoresValidOutput(t *testing.T) {
	store := &fakeStore{versions: []string{Normalize(seed)}}
	rewriter := &fakeRewriter{output: strings.Replace(seed, "- Go and Postgres internals", "- Go and Postgres internals\n- Homelab networking", 1)}
	result, err := NewService(store, fakeSignals{{Title: "Tailscale ACLs", Vote: "up"}}, rewriter).Regenerate(context.Background())
	if err != nil || !result.Changed || result.Signals != 1 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if store.sources[len(store.sources)-1] != SourceRegenerated || !strings.Contains(result.Profile, "Homelab networking") {
		t.Fatalf("stored %q from %q", result.Profile, store.sources)
	}
}

func TestRegenerateRejectsInvalidOutputAndKeepsTheAcceptedProfile(t *testing.T) {
	store := &fakeStore{versions: []string{Normalize(seed)}}
	rewriter := &fakeRewriter{output: "Sure! Here is your profile:\n" + seed}
	_, err := NewService(store, fakeSignals{{Title: "x", Vote: "down"}}, rewriter).Regenerate(context.Background())
	if err == nil || len(store.versions) != 1 {
		t.Fatalf("err=%v versions=%d, want rejection and no new version", err, len(store.versions))
	}
}

func TestEvidencePutsVotesBeforeOpensAndNewestOpensFirst(t *testing.T) {
	older, newer := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	signals := boundSignals([]preferenceexport.Signal{
		{Title: "old open", OpenedAt: &older},
		{Title: "new open", OpenedAt: &newer},
		{Title: "downvoted", Vote: "down", Tags: []string{"crypto"}},
	})
	lines := strings.Split(strings.TrimSpace(Evidence(signals)), "\n")
	want := []string{"- [voted down (strong -)] downvoted | tags: crypto", "- [opened (weak +)] new open", "- [opened (weak +)] old open"}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestHandlerRejectsAMalformedDocWith400(t *testing.T) {
	h := NewHandler("secret", NewService(&fakeStore{}, fakeSignals{}, &fakeRewriter{}))
	r := httptest.NewRequest(http.MethodPut, "/api/private/preferences", strings.NewReader("# PREFERENCES\n"))
	r.Header.Set("X-Enrichment-Secret", "secret")
	w := httptest.NewRecorder()
	h.Put(w, r)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "missing section") {
		t.Fatalf("status %d body %q", w.Code, w.Body.String())
	}
}

func TestHandlerRequiresTheSharedSecret(t *testing.T) {
	h := NewHandler("secret", NewService(&fakeStore{}, fakeSignals{}, &fakeRewriter{}))
	for _, handle := range []http.HandlerFunc{h.Get, h.Put, h.Regenerate} {
		w := httptest.NewRecorder()
		handle(w, httptest.NewRequest(http.MethodGet, "/", strings.NewReader(seed)))
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("status %d without secret", w.Code)
		}
	}
}

func TestRegenerateBeforeAnyProfileIsAConflict(t *testing.T) {
	h := NewHandler("secret", NewService(&fakeStore{}, fakeSignals{}, &fakeRewriter{}))
	r := httptest.NewRequest(http.MethodPost, "/api/private/preferences/regenerate", nil)
	r.Header.Set("X-Enrichment-Secret", "secret")
	w := httptest.NewRecorder()
	h.Regenerate(w, r)
	if w.Code != http.StatusConflict {
		t.Fatalf("status %d", w.Code)
	}
	if _, err := NewService(&fakeStore{}, fakeSignals{}, nil).Read(context.Background()); !errors.Is(err, ErrNoProfile) {
		t.Fatalf("Read err = %v", err)
	}
}
