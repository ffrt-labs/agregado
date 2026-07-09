// Package tmplfunc holds the html/template FuncMap shared by every template
// surface (the web UI renderer and the digest email generator). It lives as a
// leaf package — depending only on internal/textutil — so both internal/api and
// internal/digest can import it without creating an import cycle.
package tmplfunc

import (
	"fmt"
	"html/template"
	"strings"
	"time"

	"github.com/felipeafreitas/agregado/internal/textutil"
)

// excerptChars is the maximum number of characters shown in an article excerpt.
const excerptChars = 200

// Map is the shared set of template helpers. It is built once at package init
// and never mutated, so a single shared value is safe to hand to every template.
var Map = template.FuncMap{
	"add": func(a, b int) int { return a + b },
	"excerpt": func(s *string) string {
		if s == nil {
			return ""
		}
		clean := textutil.Strip(*s)
		if len([]rune(clean)) > excerptChars {
			return textutil.Truncate(clean, excerptChars) + "…"
		}
		return clean
	},
	"dots": func(score *int) string {
		if score == nil {
			return ""
		}
		s := *score
		if s < 1 {
			s = 1
		}
		if s > 5 {
			s = 5
		}
		return strings.Repeat("●", s) + strings.Repeat("○", 5-s)
	},
	"scoreLabel": func(score *int) string {
		if score == nil {
			return ""
		}
		labels := map[int]string{1: "noise", 2: "low", 3: "mid", 4: "high", 5: "top"}
		if l, ok := labels[*score]; ok {
			return l
		}
		return ""
	},
	// timeAgo renders a compact recency label (e.g. "42m", "6h", "3d") for an
	// email meta line, where table-based layouts can't rely on flex gaps to
	// space out a "Jan 2" style absolute date the way the web UI does.
	"timeAgo": func(t *time.Time) string {
		if t == nil {
			return ""
		}
		d := time.Since(*t)
		switch {
		case d < time.Minute:
			return "just now"
		case d < time.Hour:
			return fmt.Sprintf("%dm", int(d.Minutes()))
		case d < 24*time.Hour:
			return fmt.Sprintf("%dh", int(d.Hours()))
		case d < 7*24*time.Hour:
			return fmt.Sprintf("%dd", int(d.Hours()/24))
		default:
			return t.Format("Jan 2")
		}
	},
}
