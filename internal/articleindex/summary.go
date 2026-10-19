package articleindex

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/felipeafreitas/agregado/internal/textutil"
)

// partialMarker tells the Summary model (and, via the lead extract, the
// selection model) that the content is Miniflux's feed content, not the
// fetched Article (ADR 0009).
const partialMarker = "[content: partial]"

const (
	maxClaims         = 3
	maxEntities       = 6
	leadExtractWords  = 150
	leadExtractMaxLen = 1 << 20
)

var summaryKinds = []string{"news", "analysis", "tutorial", "opinion", "announcement", "newsletter", "other"}

type structuredSummary struct {
	Kind     string   `json:"kind"`
	Claims   []string `json:"claims"`
	Entities []string `json:"entities"`
}

// normalizeSummary validates a model's structured Summary and re-encodes it
// compactly, enforcing the ADR 0009 output contract. ok is false when the
// response is not usable JSON, so the caller falls back to the lead extract.
func normalizeSummary(raw string) (string, bool) {
	start, end := strings.Index(raw, "{"), strings.LastIndex(raw, "}")
	if start < 0 || end < start {
		return "", false
	}
	var s structuredSummary
	if err := json.Unmarshal([]byte(raw[start:end+1]), &s); err != nil {
		return "", false
	}
	s.Claims = nonEmpty(s.Claims, maxClaims)
	if len(s.Claims) == 0 {
		return "", false
	}
	s.Entities = nonEmpty(s.Entities, maxEntities)
	if s.Kind = strings.ToLower(strings.TrimSpace(s.Kind)); !slices.Contains(summaryKinds, s.Kind) {
		s.Kind = "other"
	}
	out, err := json.Marshal(s)
	if err != nil {
		return "", false
	}
	return string(out), true
}

func nonEmpty(values []string, limit int) []string {
	out := make([]string, 0, limit)
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" && len(out) < limit {
			out = append(out, v)
		}
	}
	return out
}

// leadExtract is the fallback Summary: title plus the first words of the
// cleaned content, so selection is never starved of input.
func leadExtract(title, content string, partial bool) string {
	words := strings.Fields(textutil.Clean(content, leadExtractMaxLen))
	if len(words) > leadExtractWords {
		words = words[:leadExtractWords]
	}
	out := title + "\n" + strings.Join(words, " ")
	if partial {
		out = partialMarker + "\n" + out
	}
	return out
}
