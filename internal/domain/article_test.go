package domain

import "testing"

func TestArticleBestText(t *testing.T) {
	str := func(s string) *string { return &s }

	cases := []struct {
		name string
		a    Article
		want string
	}{
		{
			name: "distilled content wins when present",
			a: Article{
				DistilledContent: str("distilled"),
				Content:          str("full content"),
				Summary:          str("teaser"),
			},
			want: "distilled",
		},
		{
			name: "content wins over summary when distilled is absent",
			a: Article{
				Content: str("full content"),
				Summary: str("teaser"),
			},
			want: "full content",
		},
		{
			name: "empty content falls through to summary",
			a: Article{
				Content: str(""),
				Summary: str("teaser"),
			},
			want: "teaser",
		},
		{
			name: "nil content falls through to summary",
			a: Article{
				Summary: str("teaser"),
			},
			want: "teaser",
		},
		{
			name: "everything nil yields empty string",
			a:    Article{},
			want: "",
		},
		{
			name: "empty distilled falls through to content",
			a: Article{
				DistilledContent: str(""),
				Content:          str("full content"),
			},
			want: "full content",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.BestText(); got != tc.want {
				t.Errorf("BestText() = %q, want %q", got, tc.want)
			}
		})
	}
}
