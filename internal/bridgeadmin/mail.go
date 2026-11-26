package bridgeadmin

import (
	"context"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/felipeafreitas/agregado/internal/resend"
	"github.com/go-chi/chi/v5"
)

// maxOpened caps how many emails the Confirm page opens for links, one
// Resend call each. A Pending Source rarely has more than a welcome mail or
// two; older ones still show subject, sender and date.
const maxOpened = 10

// WithReceivedMail gives the app Resend's receiving API, for Confirm and
// Unclaimed mail. Without it those views say mail is unavailable.
func (a *App) WithReceivedMail(mail ReceivedMail) *App {
	a.mail = mail
	return a
}

// receivedMail is one email as the app shows it: never its body, only the
// links found in it.
type receivedMail struct {
	ID        string
	Subject   string
	From      string
	Received  string
	Links     []Link
	LinkError string
	Opened    bool
}

// unclaimedAlias is recent mail to an alias that has no Source, grouped by
// alias: how many, and the newest one.
type unclaimedAlias struct {
	Alias   string
	Address string
	Count   int
	Latest  receivedMail
}

type confirmPage struct {
	Source
	Address   string
	Mail      []receivedMail
	MailError string
}

func receivedAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format("2006-01-02 15:04 UTC")
}

// sourceByAlias finds one Source among all of them; the table is small.
func (a *App) sourceByAlias(ctx context.Context, alias string) (Source, bool, error) {
	sources, err := a.store.Sources(ctx)
	if err != nil {
		return Source{}, false, err
	}
	for _, s := range sources {
		if s.Alias == alias {
			return s, true, nil
		}
	}
	return Source{}, false, nil
}

func (a *App) confirm(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	source, ok, err := a.sourceByAlias(ctx, chi.URLParam(r, "alias"))
	if err != nil {
		a.fail(w, "Could not read the Newsletter Sources from D1", err)
		return
	}
	if !ok {
		a.render(w, http.StatusNotFound, "error.html", errorPage{Message: "No Newsletter Source has that alias."})
		return
	}
	page := confirmPage{Source: source, Address: a.address(source.Alias)}
	page.Mail, page.MailError = a.mailTo(ctx, page.Address)
	a.render(w, http.StatusOK, "confirm.html", page)
}

// mailTo lists the received mail to one address and the links in the newest
// maxOpened of them. Resend's `to` filter does the narrowing; the local check
// only guards against the filter being ignored. Failures are shown, not
// fatal: the owner can still Activate.
func (a *App) mailTo(ctx context.Context, address string) ([]receivedMail, string) {
	if a.mail == nil {
		return nil, "Resend is not configured, so received mail cannot be shown."
	}
	emails, err := a.mail.ReceivedEmails(ctx, address)
	if err != nil {
		slog.Warn("resend mail unavailable", "component", "bridge-admin", "err", err)
		return nil, err.Error()
	}
	var out []receivedMail
	for _, e := range emails {
		if !sentTo(e, address) {
			continue
		}
		m := receivedMail{ID: e.ID, Subject: e.Subject, From: e.From, Received: receivedAt(e.CreatedAt)}
		if len(out) < maxOpened {
			m.Opened = true
			full, err := a.mail.ReceivedEmail(ctx, e.ID)
			if err != nil {
				m.LinkError = err.Error()
			} else {
				m.Links = ExtractLinks(full.HTML, full.Text)
			}
		}
		out = append(out, m)
	}
	return out, ""
}

// unclaimed groups the latest received mail by alias, keeping only aliases
// on the receiving domain that no Source has, newest first. A nil mail port
// shows nothing; a Resend failure is returned for a notice.
func (a *App) unclaimed(ctx context.Context, sources []Source) ([]unclaimedAlias, string) {
	if a.mail == nil {
		return nil, ""
	}
	emails, err := a.mail.ReceivedEmails(ctx)
	if err != nil {
		slog.Warn("resend mail unavailable", "component", "bridge-admin", "err", err)
		return nil, err.Error()
	}
	known := make(map[string]bool, len(sources))
	for _, s := range sources {
		known[s.Alias] = true
	}
	domain := "@" + strings.ToLower(a.cfg.ReceivingDomain)
	var groups []unclaimedAlias
	index := map[string]int{}
	for _, e := range emails {
		seen := map[string]bool{}
		for _, to := range e.To {
			address := mailbox(to)
			alias, ok := strings.CutSuffix(address, domain)
			if !ok || alias == "" || strings.Contains(alias, "@") || known[alias] || seen[alias] {
				continue
			}
			seen[alias] = true
			if i, ok := index[alias]; ok {
				groups[i].Count++
				continue
			}
			index[alias] = len(groups)
			groups = append(groups, unclaimedAlias{
				Alias: alias, Address: address, Count: 1,
				Latest: receivedMail{ID: e.ID, Subject: e.Subject, From: e.From, Received: receivedAt(e.CreatedAt)},
			})
		}
	}
	return groups, ""
}

func (a *App) activate(w http.ResponseWriter, r *http.Request) {
	alias := chi.URLParam(r, "alias")
	ok, err := a.store.Activate(r.Context(), alias)
	if err != nil {
		a.fail(w, "Could not activate the Source in D1", err)
		return
	}
	if !ok {
		a.render(w, http.StatusConflict, "error.html", errorPage{
			Message: "Nothing changed: only a Pending Source can be activated.",
			Detail:  alias + " is not Pending, or no longer exists.",
		})
		return
	}
	slog.Info("newsletter source activated", "component", "bridge-admin", "alias", alias)
	http.Redirect(w, r, "/?activated="+alias, http.StatusSeeOther)
}

func sentTo(e resend.Email, address string) bool {
	for _, to := range e.To {
		if mailbox(to) == strings.ToLower(address) {
			return true
		}
	}
	return false
}

// mailbox is the lowercase bare address in a recipient, which may come as
// "Name <addr>".
func mailbox(recipient string) string {
	recipient = strings.TrimSpace(recipient)
	if parsed, err := mail.ParseAddress(recipient); err == nil {
		recipient = parsed.Address
	}
	return strings.ToLower(recipient)
}
