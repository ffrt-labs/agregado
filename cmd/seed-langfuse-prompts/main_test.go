package main

import (
	"context"
	"io"
	"testing"

	"github.com/felipeafreitas/agregado/internal/ai"
)

type recordingSeeder struct{ seeded []ai.Prompt }

func (s *recordingSeeder) Seed(_ context.Context, p ai.Prompt) (bool, error) {
	s.seeded = append(s.seeded, p)
	return true, nil
}

func TestSeedCreatesEveryTemplateOperationFromTheInCodeDefault(t *testing.T) {
	lf := &recordingSeeder{}
	if err := seed(context.Background(), lf, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := map[string]ai.Prompt{
		"agregado/article_summary": {System: ai.DefaultPrompts[ai.OpArticleSummary], User: "Title: {{title}}\n\nContent: {{content}}"},
		"agregado/categorize": {
			System: ai.DefaultPrompts[ai.OpCategorize] + " Allowed slugs: {{allowed_slugs}}.",
			User:   "Title: {{title}}\n\nContent: {{content}}",
		},
		"agregado/digest_select":    {System: ai.DefaultPrompts[ai.OpDigestSelect], User: "Candidates:\n{{candidates}}"},
		"agregado/preference_score": {System: ai.DefaultPrompts[ai.OpPreferenceScore], User: "PREFERENCES.md:\n{{preferences}}\n\nTitle: {{title}}\n\nContent: {{content}}"},
	}
	if len(lf.seeded) != len(want) {
		t.Fatalf("seeded %d prompts, want %d", len(lf.seeded), len(want))
	}
	for _, got := range lf.seeded {
		w, ok := want[got.Name]
		if !ok || got.System != w.System || got.User != w.User {
			t.Fatalf("seeded %+v, want the in-code default %+v", got, w)
		}
	}
}

func TestSeedLeavesLegacyOperationsAlone(t *testing.T) {
	lf := &recordingSeeder{}
	if err := seed(context.Background(), lf, io.Discard); err != nil {
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
