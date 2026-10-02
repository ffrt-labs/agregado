package ai

import "testing"

func TestExtractJSONStripsAMarkdownCodeFenceWithLanguageTag(t *testing.T) {
	raw := "```json\n{\"choices\":[{\"article_id\":\"a\",\"why\":\"x\"}]}\n```"
	got := extractJSON(raw)
	want := `{"choices":[{"article_id":"a","why":"x"}]}`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractJSONStripsAMarkdownCodeFenceWithoutLanguageTag(t *testing.T) {
	raw := "```\n{\"choices\":[]}\n```"
	got := extractJSON(raw)
	want := `{"choices":[]}`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractJSONLeavesUnfencedJSONUnchanged(t *testing.T) {
	raw := `{"choices":[{"article_id":"a","why":"x"}]}`
	got := extractJSON(raw)
	if got != raw {
		t.Fatalf("got %q, want unchanged %q", got, raw)
	}
}

func TestExtractJSONHandlesSurroundingProseAroundTheFence(t *testing.T) {
	raw := "Here is my selection:\n```json\n{\"choices\":[]}\n```\nLet me know if you need anything else."
	got := extractJSON(raw)
	want := `{"choices":[]}`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExtractJSONTrimsPlainWhitespaceWithNoFence(t *testing.T) {
	raw := "\n  {\"choices\":[]}  \n"
	got := extractJSON(raw)
	want := `{"choices":[]}`
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
