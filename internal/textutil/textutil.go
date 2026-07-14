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

// htmlBlockRe matches a style/script/head/noscript element including its
// contents. Go's RE2 engine has no backreferences, so the closing tag can't be
// tied to the opening one by name — an alternation over these four names in
// both positions is good enough in practice, since a <style> block is never
// closed by </script>.
var htmlBlockRe = regexp.MustCompile(`(?is)<(style|script|head|noscript)\b[^>]*>.*?</(style|script|head|noscript)\s*>`)

// Strip removes HTML tags, decodes HTML entities (&amp; -> &), and collapses
// runs of whitespace into single spaces. style/script/head/noscript elements
// are removed along with their contents (not just their tags), so inline CSS
// and JS never leak into the stripped text.
func Strip(s string) string {
	s = htmlBlockRe.ReplaceAllString(s, " ")
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
