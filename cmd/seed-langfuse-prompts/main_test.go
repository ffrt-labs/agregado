package main

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/felipeafreitas/agregado/internal/ai"
)

type rows map[string]string

func (r rows) SystemPrompt(_ context.Context, op string) (string, error) { return r[op], nil }

type recordingSeeder struct{ seeded []ai.Prompt }

func (s *recordingSeeder) Seed(_ context.Context, p ai.Prompt) (bool, error) {
	s.seeded = append(s.seeded, p)
	return true, nil
}

func TestSeedCreatesEveryTemplateOperationFromTheLiveAIPromptsRow(t *testing.T) {
	lf := &recordingSeeder{}
	live := rows{
		ai.OpArticleSummary:  "Summary edited in admin.",
		ai.OpCategorize:      "Categorize edited in admin.",
		ai.OpDigestSelect:    "Select edited in admin.",
		ai.OpPreferenceScore: "Score edited in admin.",
	}
	if err := seed(context.Background(), live, lf, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := map[string]ai.Prompt{
		"agregado/article_summary": {System: "Summary edited in admin.", User: "Title: {{title}}\n\nContent: {{content}}"},
		"agregado/categorize": {
			System: "Categorize edited in admin. Allowed slugs: {{allowed_slugs}}.",
			User:   "Title: {{title}}\n\nContent: {{content}}",
		},
		"agregado/digest_select":    {System: "Select edited in admin.", User: "Candidates:\n{{candidates}}"},
		"agregado/preference_score": {System: "Score edited in admin.", User: "PREFERENCES.md:\n{{preferences}}\n\nTitle: {{title}}\n\nContent: {{content}}"},
	}
	if len(lf.seeded) != len(want) {
		t.Fatalf("seeded %d prompts, want %d", len(lf.seeded), len(want))
	}
	for _, got := range lf.seeded {
		w, ok := want[got.Name]
		if !ok || got.System != w.System || got.User != w.User {
			t.Fatalf("seeded %+v, want the ai_prompts row and the default user template %+v", got, w)
		}
	}
}

func TestSeedFallsBackToTheInCodeDefault(t *testing.T) {
	lf := &recordingSeeder{}
	if err := seed(context.Background(), rows{}, lf, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, got := range lf.seeded {
		if !strings.HasPrefix(got.System, ai.DefaultPrompts[strings.TrimPrefix(got.Name, "agregado/")]) {
			t.Fatalf("seeded %s system = %q, want the in-code default", got.Name, got.System)
		}
	}
}

func TestSeedLeavesLegacyOperationsAlone(t *testing.T) {
	lf := &recordingSeeder{}
	if err := seed(context.Background(), rows{}, lf, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, got := range lf.seeded {
		for _, legacy := range []string{ai.OpScore, ai.OpSummarize, ai.OpDigest, ai.OpReason} {
			if got.Name == ai.PromptName(legacy) {
				t.Fatalf("seeded legacy operation %s", got.Name)
			}
		}
	}
}
