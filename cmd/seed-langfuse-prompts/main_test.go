package main

import (
	"context"
	"io"
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

func TestSeedUsesTheLiveAIPromptsRow(t *testing.T) {
	lf := &recordingSeeder{}
	if err := seed(context.Background(), rows{ai.OpPreferenceScore: "Edited in admin."}, lf, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(lf.seeded) != 1 {
		t.Fatalf("seeded %d prompts, want 1", len(lf.seeded))
	}
	got := lf.seeded[0]
	if got.Name != "agregado/preference_score" || got.System != "Edited in admin." ||
		got.User != "PREFERENCES.md:\n{{preferences}}\n\nTitle: {{title}}\n\nContent: {{content}}" {
		t.Fatalf("seeded %+v, want the ai_prompts row and the default user template", got)
	}
}

func TestSeedFallsBackToTheInCodeDefault(t *testing.T) {
	lf := &recordingSeeder{}
	if err := seed(context.Background(), rows{}, lf, io.Discard); err != nil {
		t.Fatal(err)
	}
	if got := lf.seeded[0].System; got != ai.DefaultPrompts[ai.OpPreferenceScore] {
		t.Fatalf("seeded system = %q, want the in-code default", got)
	}
}
