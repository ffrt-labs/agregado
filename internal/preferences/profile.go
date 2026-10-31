// Package preferences owns PREFERENCES.md: the preference profile a Score is
// judged against. The accepted profile lives in Agregado's own database; n8n
// hands it over from the authoritative Google Doc through a private endpoint
// (ADR-0009), so no file or bind mount is involved.
package preferences

import (
	"errors"
	"fmt"
	"strings"
)

// MaxBytes bounds the profile. It travels in every preference_score prompt,
// so its size is paid once per enriched Article.
const MaxBytes = 8 * 1024

// Sections are the H2 headings a profile must contain, each exactly once and
// holding only bullet points. Headings and bullets are what survives the
// Google Docs Markdown round-trip; anything richer is rejected.
var Sections = []string{"Topics I want", "Topics I skip", "Sources I trust", "Kinds of articles I skip"}

const title = "# PREFERENCES"

// examplePrefix marks the seeded placeholder bullets. They are kept in the
// document as guidance but never reach a model.
const examplePrefix = "(example:"

// Normalize canonicalizes line endings and drops the trailing spaces Google
// Docs appends to list items, so an unchanged Doc normalizes identically on
// every export.
func Normalize(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n")) + "\n"
}

// Validate normalizes text and checks it has the shape described by Sections.
// The returned text is what gets stored.
func Validate(text string) (string, error) {
	text = Normalize(text)
	if len(text) > MaxBytes {
		return "", fmt.Errorf("profile is %d bytes; the limit is %d", len(text), MaxBytes)
	}

	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if !strings.EqualFold(lines[0], title) {
		return "", fmt.Errorf("profile must start with %q", title)
	}

	seen := map[string]bool{}
	section := ""
	for i, line := range lines[1:] {
		n := i + 2
		switch {
		case strings.HasPrefix(line, "## "):
			name := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if !isSection(name) {
				return "", fmt.Errorf("line %d: unknown section %q; sections are %s", n, name, strings.Join(Sections, ", "))
			}
			if seen[name] {
				return "", fmt.Errorf("line %d: section %q appears twice", n, name)
			}
			seen[name], section = true, name
		case strings.HasPrefix(line, "#"):
			return "", fmt.Errorf("line %d: only the title and the %d section headings are allowed", n, len(Sections))
		case strings.HasPrefix(line, "|"):
			return "", fmt.Errorf("line %d: tables are not allowed", n)
		case line == "" || section == "":
			// Blank lines anywhere; free prose only between the title and the
			// first section.
		case !isBullet(line):
			return "", fmt.Errorf("line %d: section %q may only contain bullet points", n, section)
		}
	}
	for _, name := range Sections {
		if !seen[name] {
			return "", fmt.Errorf("missing section %q", name)
		}
	}
	return text, nil
}

// ForModel strips the placeholder bullets, leaving what the reader actually
// wrote.
func ForModel(text string) string {
	var kept []string
	for _, line := range strings.Split(text, "\n") {
		if isBullet(line) && strings.HasPrefix(strings.TrimSpace(line[2:]), examplePrefix) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

func isSection(name string) bool {
	for _, s := range Sections {
		if name == s {
			return true
		}
	}
	return false
}

func isBullet(line string) bool {
	return strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ")
}

// ErrNoProfile means no profile has been handed over yet. Enrichment fails
// rather than scoring against nothing.
var ErrNoProfile = errors.New("no preference profile has been stored yet")
