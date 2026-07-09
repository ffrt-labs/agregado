// Package opml marshals Sources to and from OPML 2.0, the standard feed
// subscription list format (Feedly, NetNewsWire, Reeder, etc.).
package opml

import (
	"encoding/xml"

	"github.com/felipeafreitas/agregado/internal/domain"
)

type Document struct {
	XMLName xml.Name `xml:"opml"`
	Version string   `xml:"version,attr"`
	Head    Head     `xml:"head"`
	Body    Body     `xml:"body"`
}

type Head struct {
	Title string `xml:"title"`
}

type Body struct {
	Outlines []Outline `xml:"outline"`
}

type Outline struct {
	Type   string `xml:"type,attr,omitempty"`
	Text   string `xml:"text,attr"`
	Title  string `xml:"title,attr"`
	XMLURL string `xml:"xmlUrl,attr,omitempty"`
}

// Export renders sources as an OPML 2.0 document. Newsletter sources (no URL)
// are included with an empty xmlUrl so they round-trip in the export, but are
// naturally skipped on import since ParseImportCandidates keys off xmlUrl
// presence.
func Export(sources []domain.Source) ([]byte, error) {
	doc := Document{
		Version: "2.0",
		Head:    Head{Title: "Agregado Sources"},
	}
	for _, s := range sources {
		o := Outline{
			Type:  string(s.Type),
			Text:  s.Name,
			Title: s.Name,
		}
		if s.URL != nil {
			o.XMLURL = *s.URL
		}
		doc.Body.Outlines = append(doc.Body.Outlines, o)
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}

	return append([]byte(xml.Header), body...), nil
}

// Candidate is a source worth importing — an outline with a feed URL.
type Candidate struct {
	Name string
	URL  string
}

// ParseImportCandidates reads an OPML document and returns the outlines that
// have a feed URL. Matching is keyed on xmlUrl presence rather than
// type="rss", so OPML exported by third-party readers (which don't always set
// the type attribute) imports cleanly too. Outlines without xmlUrl (including
// agregado's own newsletter-source entries) are skipped.
func ParseImportCandidates(data []byte) ([]Candidate, error) {
	var doc Document
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}

	var candidates []Candidate
	for _, o := range doc.Body.Outlines {
		if o.XMLURL == "" {
			continue
		}
		name := o.Title
		if name == "" {
			name = o.Text
		}
		candidates = append(candidates, Candidate{Name: name, URL: o.XMLURL})
	}

	return candidates, nil
}
