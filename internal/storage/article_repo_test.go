package storage

import "testing"

func TestSortClause(t *testing.T) {
	const recentClause = "ORDER BY COALESCE(published_at, ingested_at) DESC"
	const relevantClause = "ORDER BY relevance_score DESC NULLS LAST, COALESCE(published_at, ingested_at) DESC"

	cases := []struct {
		name string
		sort string
		want string
	}{
		{name: "relevant", sort: "relevant", want: relevantClause},
		{name: "recent", sort: "recent", want: recentClause},
		{name: "empty defaults to recent", sort: "", want: recentClause},
		{name: "unrecognized value defaults to recent, never reaches SQL as text", sort: "'; DROP TABLE articles; --", want: recentClause},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sortClause(tc.sort); got != tc.want {
				t.Errorf("sortClause(%q) = %q, want %q", tc.sort, got, tc.want)
			}
		})
	}
}
