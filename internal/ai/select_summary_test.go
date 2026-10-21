package ai

import (
	"strings"
	"testing"
)

func TestFlattenSummary(t *testing.T) {
	structured := `{"kind":"news","claims":["Rates rose.","Banks fell."],"entities":["Fed","ECB & co"]}`
	tests := []struct{ name, in, want string }{
		{"structured", structured, "news: Rates rose.; Banks fell. (entities: Fed, ECB & co)"},
		{"structured without entities", `{"kind":"opinion","claims":["A take."],"entities":[]}`, "opinion: A take."},
		{"partial structured", "[content: partial]\n" + structured, "[content: partial] news: Rates rose.; Banks fell. (entities: Fed, ECB & co)"},
		{"lead extract", "Some title\nfirst words of the article", "Some title\nfirst words of the article"},
		{"partial lead extract", "[content: partial]\nSome title\nfirst words", "[content: partial]\nSome title\nfirst words"},
		{"json without claims is left alone", `{"kind":"news"}`, `{"kind":"news"}`},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := flattenSummary(tt.in); got != tt.want {
				t.Errorf("flattenSummary(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDecodeSelectionReadsOrderedArticleIDs(t *testing.T) {
	ids, err := decodeSelection("```json\n{\"article_ids\":[\"b\",\"a\"]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(ids, ",") != "b,a" {
		t.Fatalf("ids = %v, want [b a]", ids)
	}
	if _, err := decodeSelection("not json"); err == nil {
		t.Fatal("want a decode error")
	}
}

func TestDigestSelectPromptDropsPromotionsBelowScoreFourAndAsksForIDsOnly(t *testing.T) {
	prompt := DefaultPrompts[OpDigestSelect]
	for _, want := range []string{"announcement", "promotion", "webinar", "score is 4 or higher", "article_ids"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q: %s", want, prompt)
		}
	}
	if strings.Contains(prompt, "why") {
		t.Errorf("prompt must not ask for a why: %s", prompt)
	}
}
