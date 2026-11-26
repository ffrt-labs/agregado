package bridgeadmin

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/felipeafreitas/agregado/internal/resend"
)

// Activate is fakeStore's half of SourceStore.Activate: only a Pending row
// turns Active.
func (f *fakeStore) Activate(_ context.Context, alias string) (bool, error) {
	for i, s := range f.sources {
		if s.Alias == alias && s.Status == StatusPending {
			f.sources[i].Status = StatusActive
			return true, nil
		}
	}
	return false, nil
}

// fakeMail is Resend's receiving API: a newest-first inbox plus the bodies
// the detail call returns, honouring the exact-match `to` filter.
type fakeMail struct {
	inbox   []resend.Email
	bodies  map[string]resend.Email
	listErr error
	filters [][]string
	opened  []string
}

func (f *fakeMail) ReceivedEmails(_ context.Context, recipients ...string) ([]resend.Email, error) {
	f.filters = append(f.filters, recipients)
	if f.listErr != nil {
		return nil, f.listErr
	}
	if len(recipients) == 0 {
		return f.inbox, nil
	}
	var out []resend.Email
	for _, e := range f.inbox {
		for _, to := range e.To {
			for _, want := range recipients {
				if to == want {
					out = append(out, e)
				}
			}
		}
	}
	return out, nil
}

func (f *fakeMail) ReceivedEmail(_ context.Context, id string) (resend.Email, error) {
	f.opened = append(f.opened, id)
	body, ok := f.bodies[id]
	if !ok {
		return resend.Email{}, errors.New("resend: not found")
	}
	return body, nil
}

func newMailApp(store *fakeStore, mail *fakeMail) http.Handler {
	app := New(store, &fakeMiniflux{}, mail, nil, Config{
		ReceivingDomain: "news.example.com",
		BridgeOrigin:    "https://bridge.example.com",
	})
	app.now = func() time.Time { return now }
	return app.Routes()
}

func pendingTLDR() *fakeStore {
	return &fakeStore{sources: []Source{
		{Alias: "tldr", DisplayName: "TLDR", Status: StatusPending},
		{Alias: "morning-brew", DisplayName: "Morning Brew", Status: StatusActive},
	}}
}

func TestConfirm_ShowsAPendingSourcesMailWithItsLinksButNeverTheEmail(t *testing.T) {
	mail := &fakeMail{
		inbox: []resend.Email{
			{ID: "e2", To: []string{"morning-brew@news.example.com"}, From: "brew@morningbrew.com", Subject: "Today's Brew"},
			{ID: "e1", To: []string{"tldr@news.example.com"}, From: "TLDR <dan@tldr.tech>", Subject: "Confirm <your> subscription",
				CreatedAt: time.Date(2026, 11, 25, 7, 30, 0, 0, time.UTC)},
		},
		bodies: map[string]resend.Email{
			"e1": {ID: "e1", HTML: `<p style="color:red">Click</p><script>steal()</script>
				<a href="https://tldr.tech/confirm?t=a&amp;u=1">Yes, <b>subscribe</b> me</a>`},
		},
	}
	rec := get(t, newMailApp(pendingTLDR(), mail), "/sources/tldr/confirm")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	mustContain(t, body,
		"TLDR", "tldr@news.example.com",
		"Confirm &lt;your&gt; subscription", "TLDR &lt;dan@tldr.tech&gt;", "2026-11-25 07:30 UTC",
		`href="https://tldr.tech/confirm?t=a&amp;u=1"`, "Yes, subscribe me",
		`action="/sources/tldr/activate"`,
	)
	for _, leaked := range []string{"Today's Brew", "<script>", "steal()", `style="color:red"`, "<b>subscribe</b>"} {
		if strings.Contains(body, leaked) {
			t.Errorf("page leaks %q\n%s", leaked, body)
		}
	}
	if len(mail.filters) != 1 || len(mail.filters[0]) != 1 || mail.filters[0][0] != "tldr@news.example.com" {
		t.Errorf("Resend list filters = %v, want [[tldr@news.example.com]]", mail.filters)
	}
	if len(mail.opened) != 1 || mail.opened[0] != "e1" {
		t.Errorf("opened = %v, want [e1]", mail.opened)
	}
}

func TestConfirm_ResendDownStillLetsTheOwnerActivate(t *testing.T) {
	mail := &fakeMail{listErr: errors.New("resend: 401 restricted_api_key")}
	rec := get(t, newMailApp(pendingTLDR(), mail), "/sources/tldr/confirm")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "Could not read mail from Resend: resend: 401 restricted_api_key", `action="/sources/tldr/activate"`)
}

func TestConfirm_UnknownAliasIs404(t *testing.T) {
	if rec := get(t, newMailApp(pendingTLDR(), &fakeMail{}), "/sources/nope/confirm"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestActivate_TurnsAPendingSourceActiveAndSaysSoOnTheList(t *testing.T) {
	store := pendingTLDR()
	app := newMailApp(store, &fakeMail{})

	rec := post(t, app, "/sources/tldr/activate", nil)
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/?activated=tldr" {
		t.Fatalf("status = %d, location %q", rec.Code, rec.Header().Get("Location"))
	}
	if store.sources[0].Status != StatusActive {
		t.Errorf("tldr status = %q, want active", store.sources[0].Status)
	}
	mustContain(t, get(t, app, "/?activated=tldr").Body.String(), "Activated TLDR")
}

func TestActivate_RefusesASourceThatIsNotPending(t *testing.T) {
	store := pendingTLDR()
	rec := post(t, newMailApp(store, &fakeMail{}), "/sources/morning-brew/activate", nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	mustContain(t, rec.Body.String(), "only a Pending Source can be activated")
	if store.sources[1].Status != StatusActive {
		t.Errorf("status changed to %q", store.sources[1].Status)
	}
}

func TestList_PendingSourcesLinkToConfirm(t *testing.T) {
	body := get(t, newMailApp(pendingTLDR(), &fakeMail{}), "/").Body.String()
	mustContain(t, body, `href="/sources/tldr/confirm"`)
	if strings.Contains(body, `href="/sources/morning-brew/confirm"`) {
		t.Errorf("an Active Source links to Confirm\n%s", body)
	}
}

func TestList_UnclaimedMailOffersToRegisterEachUnknownAlias(t *testing.T) {
	mail := &fakeMail{inbox: []resend.Email{
		{ID: "e5", To: []string{"Lenny <Lenny@News.Example.com>"}, From: "lenny@substack.com", Subject: "Welcome to Lenny's",
			CreatedAt: time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC)},
		{ID: "e4", To: []string{"tldr@news.example.com"}, From: "dan@tldr.tech", Subject: "Known Source"},
		{ID: "e3", To: []string{"someone@elsewhere.example"}, From: "x@y.z", Subject: "Other domain"},
		{ID: "e2", To: []string{"lenny@news.example.com"}, From: "lenny@substack.com", Subject: "Confirm Lenny's"},
		{ID: "e1", To: []string{"odd alias@news.example.com"}, From: "a@b.c", Subject: "<b>odd</b>"},
	}}
	rec := get(t, newMailApp(pendingTLDR(), mail), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	mustContain(t, body,
		"Unclaimed mail",
		"lenny@news.example.com", "2 emails", "Welcome to Lenny&#39;s", "2026-11-26 09:00 UTC",
		`href="/sources/new?alias=lenny"`,
		"odd alias@news.example.com", "&lt;b&gt;odd&lt;/b&gt;", "cannot be registered",
	)
	for _, wrong := range []string{"Known Source", "Other domain", "Confirm Lenny", `href="/sources/new?alias=odd`} {
		if strings.Contains(body, wrong) {
			t.Errorf("unclaimed mail shows %q\n%s", wrong, body)
		}
	}
	if len(mail.filters) != 1 || len(mail.filters[0]) != 0 {
		t.Errorf("list filters = %v, want one unfiltered call", mail.filters)
	}
	if len(mail.opened) != 0 {
		t.Errorf("the List opened emails %v; it needs no bodies", mail.opened)
	}
}

func TestList_ResendDownLeavesTheSourcesListed(t *testing.T) {
	rec := get(t, newMailApp(pendingTLDR(), &fakeMail{listErr: errors.New("resend: timeout")}), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "TLDR", "Resend is unreachable, so unclaimed mail is unknown: resend: timeout")
}

func TestD1Store_ActivateOnlyTurnsPendingRowsActive(t *testing.T) {
	store, calls := d1Stub(t, map[string]string{"UPDATE sources SET status": `[]`})
	ok, err := store.Activate(context.Background(), "tldr")
	if err != nil || !ok {
		t.Fatalf("activate = %v, %v", ok, err)
	}
	assertStatusMove(t, (*calls)[0], StatusActive, "tldr", StatusPending)

	untouched, _ := d1Stub(t, nil)
	if ok, err := untouched.Activate(context.Background(), "tldr"); err != nil || ok {
		t.Errorf("no row changed: activate = %v, %v, want false", ok, err)
	}
}
