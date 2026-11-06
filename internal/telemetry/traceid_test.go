package telemetry

import "testing"

// Expected values come from `printf %s <key> | sha256sum | cut -c1-32`, the
// same digest the Langfuse Python SDK's create_trace_id(seed=<key>) returns,
// so the harness and ad-hoc scripts can find any Agregado trace.
func TestTraceIDHashesNamespacedKey(t *testing.T) {
	cases := []struct {
		name, key, want string
	}{
		{"article index record", ArticleIndexKey("index-1"), "bd81d27a19295169b9a7b3c12b25ac55"},
		{"article index uuid", ArticleIndexKey("0b4f3c1e-8a2d-4f7a-9c1e-2d3f4a5b6c7d"), "2e9b2c5f85bef9024a8ec318ce4939e8"},
		{"digest artifact", DigestKey("20261030"), "b4f16bf8f8258d554a9689d65f43151c"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := TraceIDHex(tc.key); got != tc.want {
				t.Fatalf("TraceIDHex(%q) = %s, want %s", tc.key, got, tc.want)
			}
			id := TraceID(tc.key)
			if len(id) != 16 {
				t.Fatalf("TraceID length = %d, want 16", len(id))
			}
		})
	}
}

func TestTraceKeysAreNamespaced(t *testing.T) {
	if got := ArticleIndexKey("x"); got != "article-index:x" {
		t.Fatalf("ArticleIndexKey = %q", got)
	}
	if got := DigestKey("x"); got != "digest:x" {
		t.Fatalf("DigestKey = %q", got)
	}
	if TraceIDHex(ArticleIndexKey("x")) == TraceIDHex(DigestKey("x")) {
		t.Fatal("article and digest keys with the same id share a trace")
	}
}
