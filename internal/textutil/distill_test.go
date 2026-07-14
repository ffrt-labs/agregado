package textutil

import "testing"

func TestDistill(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{
			name: "lede kept in full, later section trimmed to first sentence",
			in: "This is the opening paragraph. It sets up the whole story with real detail.\n\n" +
				"## Background\n\n" +
				"Here is the first sentence of the section. Here is a second sentence that should be dropped. And a third.",
			max: 1000,
			want: "This is the opening paragraph. It sets up the whole story with real detail.\n\n" +
				"## Background\n\n" +
				"Here is the first sentence of the section.",
		},
		{
			name: "bare markdown link is dropped as boilerplate",
			in: "Real opening paragraph goes here with enough words.\n\n" +
				"[Unsubscribe](https://example.com/unsub)\n\n" +
				"## Next\n\nFirst real sentence of this part. More detail follows here.",
			max: 1000,
			want: "Real opening paragraph goes here with enough words.\n\n" +
				"## Next\n\nFirst real sentence of this part.",
		},
		{
			name: "short button-like line is dropped as boilerplate",
			in:   "Opening paragraph with plenty of words in it.\n\nRead more\n\n## More\n\nSection content sentence one. Sentence two dropped.",
			max:  1000,
			want: "Opening paragraph with plenty of words in it.\n\n## More\n\nSection content sentence one.",
		},
		{
			name: "second paragraph within the same section is dropped entirely",
			in: "Document lede paragraph appears first with real words.\n\n" +
				"## Intro\n\nFirst sentence of the section. Second sentence dropped.\n\n" +
				"A whole extra paragraph that should never appear.",
			max: 1000,
			want: "Document lede paragraph appears first with real words.\n\n" +
				"## Intro\n\nFirst sentence of the section.",
		},
		{
			name: "sentence without terminal punctuation is kept whole",
			in:   "Lede paragraph here with words.\n\n## Section\n\nNo terminal punctuation in this one",
			max:  1000,
			want: "Lede paragraph here with words.\n\n## Section\n\nNo terminal punctuation in this one",
		},
		{
			name: "result is truncated to max runes as a hard cap",
			in:   "This lede paragraph is deliberately long enough to exceed a small cap easily.",
			max:  10,
			want: "This lede ",
		},
		{
			name: "empty input yields empty output",
			in:   "",
			max:  1000,
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Distill(tc.in, tc.max); got != tc.want {
				t.Errorf("Distill(...) = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFirstSentence(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{name: "period", in: "One. Two.", want: "One."},
		{name: "question mark", in: "Really? Yes.", want: "Really?"},
		{name: "no terminator", in: "no ending here", want: "no ending here"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstSentence(tc.in); got != tc.want {
				t.Errorf("firstSentence(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
