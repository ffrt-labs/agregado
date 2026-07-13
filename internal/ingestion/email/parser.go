package email

import (
	"fmt"
	"net/mail"
	"strings"

	"github.com/felipeafreitas/agregado/internal/domain"
	"github.com/google/uuid"
	"github.com/jaytaylor/html2text"
)

type Parser struct {
}

func NewParser() *Parser {
	return &Parser{}
}

// SenderInfo is the stable newsletter identity resolved from the email's
// headers, as opposed to the SMTP envelope sender (payload.From), which is an
// ESP bounce/VERP address that rotates on every send — see PRD F3.3.
type SenderInfo struct {
	// Identity is the lookup/dedup key: List-Id (RFC 2919), falling back to
	// the From-header address, falling back to the envelope sender.
	Identity string
	// Address is the human From-header email address (falls back to the
	// envelope sender if the From header is missing/unparseable).
	Address string
	// Name is the From-header display name (falls back to Address).
	Name string
}

func (p *Parser) Parse(payload Payload) (*domain.Article, SenderInfo, error) {
	title := payload.Subject

	var content string
	content, err := html2text.FromString(payload.Html)

	if content == "" || err != nil {
		content = payload.Text
	}

	sender := resolveSender(payload.Headers, payload.From)

	author := sender.Name

	return &domain.Article{
		Title: title,
		Content: &content,
		ExternalURL: fmt.Sprintf("newsletter:%s", uuid.New().String()),
		Author: &author,
	}, sender, nil
}

// resolveSender derives the stable newsletter identity from the parsed email
// headers (forwarded verbatim, lowercased, by the Cloudflare Worker). Priority:
// List-Id (RFC 2919, the gold standard for mailing-list identity) > the
// From-header address (stable per newsletter) > the envelope sender (last
// resort — this is the rotating address the original bug keyed on).
func resolveSender(headers Headers, envelopeFrom string) SenderInfo {
	address := envelopeFrom
	name := envelopeFrom

	if from, err := mail.ParseAddress(headers["from"]); err == nil {
		address = from.Address
		if from.Name != "" {
			name = from.Name
		} else {
			name = from.Address
		}
	}

	identity := extractListID(headers["list-id"])
	if identity == "" {
		identity = address
	}

	return SenderInfo{Identity: identity, Address: address, Name: name}
}

// extractListID pulls the stable identifier out of a raw List-Id header value.
// Per RFC 2919 the format is `Display Phrase <list.id.value>`; the angle-bracket
// portion is the actual stable identity, the phrase is just a human label.
func extractListID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	if start := strings.LastIndex(raw, "<"); start != -1 {
		if end := strings.Index(raw[start:], ">"); end != -1 {
			return raw[start+1 : start+end]
		}
	}

	return raw
}
