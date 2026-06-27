// Package textutil provides small, dependency-free helpers for turning
// HTML-bearing feed text (RSS descriptions, newsletter bodies) into clean
// plain text for prompts and excerpts.
package textutil

import (
	"html"
	"regexp"
	"strings"
)

// htmlTagRe matches any HTML tag.
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// Strip removes HTML tags, decodes HTML entities (&amp; -> &), and collapses
// runs of whitespace into single spaces.
func Strip(s string) string {
	s = htmlTagRe.ReplaceAllString(s, " ")
	s = html.UnescapeString(s)
	return strings.Join(strings.Fields(s), " ")
}

// Truncate shortens s to at most max characters, counting runes (not bytes) so
// a multibyte character is never split. It does not add an ellipsis.
func Truncate(s string, max int) string {
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

// Clean strips HTML from s and truncates the result to max characters.
func Clean(s string, max int) string {
	return Truncate(Strip(s), max)
}
