package digestartifact

import (
	"encoding/json"
	"strings"
)

const (
	maxShownClaims = 2
	partialMarker  = "[content: partial]"
)

// truncationMarkers match the claim the Summary model spends on a stub or
// truncated Article (ADR 0009 asks it to say so). They are filtered at render
// time so the article_summary prompt stays as the ADR defines it.
var truncationMarkers = []string{"truncated", "stub"}

// claims returns up to maxShownClaims reader-facing claims from a stored
// Summary: structured JSON, or the plain-text lead extract (title line, then
// the content's first words), either with a leading [content: partial] line.
func claims(summary string) []string {
	body, parsed, err := parseSummary(summary)
	var candidates []string
	if err == nil && len(parsed.Claims) > 0 {
		candidates = parsed.Claims
	} else if !strings.HasPrefix(body, "{") {
		// Lead extract: drop the title line, which the item already shows.
		_, content, _ := strings.Cut(body, "\n")
		candidates = sentences(content)
	}
	out := make([]string, 0, maxShownClaims)
	for _, c := range candidates {
		if c = strings.TrimSpace(c); c != "" && !isTruncationClaim(c) {
			if out = append(out, c); len(out) == maxShownClaims {
				break
			}
		}
	}
	return out
}

// summaryKind is the structured Summary's kind ("news", "analysis", ...), or
// "" for a lead extract or a Summary without one.
func summaryKind(summary string) string {
	_, parsed, _ := parseSummary(summary)
	return strings.TrimSpace(parsed.Kind)
}

// structuredSummary is the ADR 0009 contract, minus the unused entities.
type structuredSummary struct {
	Kind   string   `json:"kind"`
	Claims []string `json:"claims"`
}

// parseSummary strips the [content: partial] marker and decodes the rest as
// a structured Summary; err is set for a lead extract.
func parseSummary(summary string) (body string, parsed structuredSummary, err error) {
	body = strings.TrimSpace(strings.TrimPrefix(summary, partialMarker))
	err = json.Unmarshal([]byte(body), &parsed)
	return body, parsed, err
}

func isTruncationClaim(claim string) bool {
	lower := strings.ToLower(claim)
	for _, m := range truncationMarkers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

// sentences splits on terminal punctuation followed by whitespace.
func sentences(text string) []string {
	var out []string
	start := 0
	runes := []rune(text)
	for i, r := range runes {
		if (r == '.' || r == '!' || r == '?') && (i+1 == len(runes) || runes[i+1] == ' ' || runes[i+1] == '\n') {
			out = append(out, string(runes[start:i+1]))
			start = i + 1
		}
	}
	if start < len(runes) {
		out = append(out, string(runes[start:]))
	}
	return out
}
