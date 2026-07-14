package textutil

import (
	"regexp"
	"strings"
)

// headingRe matches a Markdown ATX heading line ("#" .. "######").
var headingRe = regexp.MustCompile(`^#{1,6}\s+\S`)

// bareLinkRe matches a block that is nothing but a single Markdown link or
// image, e.g. "[Unsubscribe](https://...)" or "![](https://...)" — the
// nav/footer noise that survives HTML→Markdown conversion.
var bareLinkRe = regexp.MustCompile(`^!?\[[^\]]*\]\([^)]*\)$`)

// firstSentenceRe captures the shortest run up to (and including) the first
// sentence-ending punctuation. (?s) lets "." match newlines inside the block.
var firstSentenceRe = regexp.MustCompile(`(?s)^.*?[.!?](\s|$)`)

// Distill reduces a Markdown article body to its salient structure: every
// heading, the opening paragraph in full (the "lede"), and one sentence from
// the first paragraph of every section thereafter. It is a purely algorithmic
// extractive pass — no AI call — so it's free, deterministic and fast enough
// to run inline in the ingest path. The result is truncated to max runes as a
// hard cap.
//
// This deliberately does not try to be a general-purpose summarizer: it keeps
// enough for an AI prompt to judge relevance and topic, discarding repeated
// elaboration and boilerplate (bare nav/footer links, short button-like lines).
func Distill(markdown string, max int) string {
	blocks := strings.Split(strings.ReplaceAll(markdown, "\r\n", "\n"), "\n\n")

	var kept []string
	ledeTaken := false
	pendingSectionLead := false

	for _, raw := range blocks {
		block := strings.TrimSpace(raw)
		if block == "" {
			continue
		}

		if headingRe.MatchString(block) {
			kept = append(kept, block)
			pendingSectionLead = true
			continue
		}

		if isBoilerplate(block) {
			continue
		}

		switch {
		case !ledeTaken:
			kept = append(kept, block)
			ledeTaken = true
			pendingSectionLead = false
		case pendingSectionLead:
			kept = append(kept, firstSentence(block))
			pendingSectionLead = false
		}
	}

	return Truncate(strings.Join(kept, "\n\n"), max)
}

// isBoilerplate flags blocks unlikely to carry article substance: bare
// links/images (unsubscribe, share, nav) and blocks too short to be a real
// sentence (button labels like "Read more").
func isBoilerplate(block string) bool {
	if bareLinkRe.MatchString(block) {
		return true
	}
	return len(strings.Fields(block)) <= 3
}

// firstSentence returns the leading sentence of s, or all of s if no
// sentence-ending punctuation is found.
func firstSentence(s string) string {
	if m := firstSentenceRe.FindString(s); m != "" {
		return strings.TrimSpace(m)
	}
	return s
}
