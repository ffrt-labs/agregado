package bridgeadmin

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/felipeafreitas/agregado/internal/miniflux"
)

// The fakes' Rename/Reconnect methods behave like the real systems; the
// wrappers below record calls and inject failures for these tests only.

func (f *fakeStore) RenameSource(_ context.Context, alias, displayName string) error {
	for i := range f.sources {
		if f.sources[i].Alias == alias {
			f.sources[i].DisplayName = displayName
			return nil
		}
	}
	return errors.New("no such Source")
}

func (f *fakeStore) SetSecretHash(_ context.Context, alias, hash string) error {
	for _, s := range f.sources {
		if s.Alias == alias {
			if f.hashes == nil {
				f.hashes = map[string]string{}
			}
			f.hashes[alias] = hash
			return nil
		}
	}
	return errors.New("no such Source")
}

func (f *fakeMiniflux) UpdateFeed(_ context.Context, id int64, update miniflux.FeedUpdate) error {
	for i := range f.feeds {
		if f.feeds[i].ID == id {
			if update.Title != "" {
				f.feeds[i].Title = update.Title
			}
			return nil
		}
	}
	return errors.New("miniflux: PUT /v1/feeds: 404 Not Found")
}

func (f *fakeMiniflux) RefreshFeed(context.Context, int64) error { return nil }

// rrStore is fakeStore with injectable write failures and a record of renames.
type rrStore struct {
	*fakeStore
	renameErr error // fails the first rename only, like a D1 blip
	hashErr   error
	renames   []string
}

func (s *rrStore) RenameSource(ctx context.Context, alias, displayName string) error {
	s.renames = append(s.renames, displayName)
	if err := s.renameErr; err != nil {
		s.renameErr = nil
		return err
	}
	return s.fakeStore.RenameSource(ctx, alias, displayName)
}

func (s *rrStore) SetSecretHash(ctx context.Context, alias, hash string) error {
	if s.hashErr != nil {
		return s.hashErr
	}
	return s.fakeStore.SetSecretHash(ctx, alias, hash)
}

// rrMiniflux is fakeMiniflux recording feed updates and refreshes.
type rrMiniflux struct {
	*fakeMiniflux
	updateErr  error
	refreshErr error
	updates    []miniflux.FeedUpdate
	refreshed  []int64
}

func (m *rrMiniflux) UpdateFeed(ctx context.Context, id int64, update miniflux.FeedUpdate) error {
	m.updates = append(m.updates, update)
	if m.updateErr != nil {
		return m.updateErr
	}
	return m.fakeMiniflux.UpdateFeed(ctx, id, update)
}

func (m *rrMiniflux) RefreshFeed(_ context.Context, id int64) error {
	m.refreshed = append(m.refreshed, id)
	return m.refreshErr
}

func newRRApp(store SourceStore, mf Miniflux) http.Handler {
	app := New(store, mf, nil, nil, Config{ReceivingDomain: "news.example.com", BridgeOrigin: "https://bridge.example.com"})
	app.now = func() time.Time { return now }
	return app.Routes()
}

// lennyFixture is one Active Source whose Miniflux feed exists.
func lennyFixture() (*rrStore, *rrMiniflux) {
	store := &rrStore{fakeStore: &fakeStore{
		sources: []Source{{Alias: "lenny", DisplayName: "Lenny", Status: StatusActive}},
		hashes:  map[string]string{"lenny": "old-hash"},
	}}
	mf := &rrMiniflux{fakeMiniflux: &fakeMiniflux{
		categories: []miniflux.Category{{ID: 7, Title: "Newsletters"}},
		feeds: []miniflux.Feed{
			{ID: 41, Title: "Someone else", FeedURL: "https://elsewhere.example/feed/lenny-podcast.atom"},
			{ID: 42, Title: "Lenny", FeedURL: "https://bridge.example.com/feed/lenny.atom"},
		},
	}}
	return store, mf
}

func TestRename_UpdatesTheD1NameAndTheMinifluxTitleTogether(t *testing.T) {
	store, mf := lennyFixture()
	app := newRRApp(store, mf)

	rec := post(t, app, "/sources/lenny/rename", url.Values{"display_name": {"  Lenny's Newsletter "}, "alias": {"hacked"}})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/sources/lenny/edit?done=renamed" {
		t.Fatalf("status %d location %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if got := store.sources[0]; got != (Source{Alias: "lenny", DisplayName: "Lenny's Newsletter", Status: StatusActive}) {
		t.Errorf("D1 source = %+v, want only the display name changed", got)
	}
	if mf.feeds[1].Title != "Lenny's Newsletter" || mf.feeds[0].Title != "Someone else" {
		t.Errorf("Miniflux feeds = %+v, want the Bridge feed retitled and nothing else", mf.feeds)
	}
	if len(mf.updates) != 1 || mf.updates[0] != (miniflux.FeedUpdate{Title: "Lenny's Newsletter"}) {
		t.Errorf("Miniflux updates = %+v, want the title only", mf.updates)
	}

	page := get(t, app, "/sources/lenny/edit?done=renamed").Body.String()
	mustContain(t, page, "Renamed to Lenny&#39;s Newsletter", `value="Lenny&#39;s Newsletter"`, "lenny@news.example.com")
}

func TestRename_FindsTheFeedWhateverItsOrigin(t *testing.T) {
	store, mf := lennyFixture()
	mf.feeds[1].FeedURL = "http://bridge-old.workers.dev/feed/lenny.atom"

	rec := post(t, newRRApp(store, mf), "/sources/lenny/rename", url.Values{"display_name": {"Lenny's Newsletter"}})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/sources/lenny/edit?done=renamed" {
		t.Fatalf("status %d location %q", rec.Code, rec.Header().Get("Location"))
	}
	if mf.feeds[1].Title != "Lenny's Newsletter" {
		t.Errorf("Miniflux feeds = %+v", mf.feeds)
	}
}

func TestRename_PutsTheOldNameBackInD1WhenMinifluxRefuses(t *testing.T) {
	store, mf := lennyFixture()
	mf.updateErr = errors.New("miniflux: PUT /v1/feeds/42 returned 500: boom")

	rec := post(t, newRRApp(store, mf), "/sources/lenny/rename", url.Values{"display_name": {"Lenny's Newsletter"}})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "boom", "Nothing was renamed")
	if store.sources[0].DisplayName != "Lenny" || mf.feeds[1].Title != "Lenny" {
		t.Errorf("D1 %q, Miniflux %q: want both still Lenny", store.sources[0].DisplayName, mf.feeds[1].Title)
	}
}

func TestRename_LeavesMinifluxAloneWhenD1Fails(t *testing.T) {
	store, mf := lennyFixture()
	store.renameErr = errors.New("d1: 503")

	rec := post(t, newRRApp(store, mf), "/sources/lenny/rename", url.Values{"display_name": {"Lenny's Newsletter"}})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "d1: 503")
	if len(mf.updates) != 0 || store.sources[0].DisplayName != "Lenny" {
		t.Errorf("updates %+v, D1 %q", mf.updates, store.sources[0].DisplayName)
	}
}

func TestRename_WithoutAMinifluxFeedRenamesD1AndPointsToReconnect(t *testing.T) {
	store, mf := lennyFixture()
	mf.feeds = mf.feeds[:1] // the Bridge feed was deleted in Miniflux by hand

	rec := post(t, newRRApp(store, mf), "/sources/lenny/rename", url.Values{"display_name": {"Lenny's Newsletter"}})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/sources/lenny/edit?done=renamed-no-feed" {
		t.Fatalf("status %d location %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if store.sources[0].DisplayName != "Lenny's Newsletter" || len(mf.updates) != 0 {
		t.Errorf("D1 %q, updates %+v", store.sources[0].DisplayName, mf.updates)
	}
	page := get(t, newRRApp(store, mf), "/sources/lenny/edit?done=renamed-no-feed").Body.String()
	mustContain(t, page, "Miniflux has no feed for this Source", "Reconnect")
}

func TestRename_WritesNothingWhenMinifluxIsUnreachable(t *testing.T) {
	store, mf := lennyFixture()
	mf.feedsErr = errors.New("connection refused")

	rec := post(t, newRRApp(store, mf), "/sources/lenny/rename", url.Values{"display_name": {"Lenny's Newsletter"}})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	if len(store.renames) != 0 {
		t.Errorf("renamed in D1 %v while Miniflux was down", store.renames)
	}
}

func TestRename_RequiresADisplayName(t *testing.T) {
	store, mf := lennyFixture()

	rec := post(t, newRRApp(store, mf), "/sources/lenny/rename", url.Values{"display_name": {"   "}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "display name is required")
	if len(store.renames) != 0 || len(mf.updates) != 0 {
		t.Errorf("renames %v, updates %+v", store.renames, mf.updates)
	}
}

func TestReconnect_RotatesThePasswordAndGivesItToTheExistingFeed(t *testing.T) {
	store, mf := lennyFixture()
	app := newRRApp(store, mf)

	rec := post(t, app, "/sources/lenny/reconnect", nil)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/sources/lenny/edit?done=reconnected" {
		t.Fatalf("status %d location %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if len(mf.updates) != 1 {
		t.Fatalf("updates = %+v", mf.updates)
	}
	update := mf.updates[0]
	if update.Username != "lenny" || len(update.Password) != 48 || update.Title != "" {
		t.Errorf("update = %+v, want username lenny and a fresh 48-char password only", update)
	}
	if hash := store.hashes["lenny"]; hash == "old-hash" || hash != HashSecret(update.Password) {
		t.Errorf("D1 hash %q is not the hash of the password Miniflux got", hash)
	}
	if len(mf.refreshed) != 1 || mf.refreshed[0] != 42 {
		t.Errorf("refreshed %v, want feed 42 fetched with the new password", mf.refreshed)
	}
	if len(mf.subscribed) != 0 {
		t.Errorf("subscribed %+v, want the existing feed reused", mf.subscribed)
	}
	page := get(t, app, "/sources/lenny/edit?done=reconnected").Body.String()
	mustContain(t, page, "Reconnected")
	if strings.Contains(page, update.Password) || strings.Contains(rec.Body.String(), update.Password) {
		t.Error("the feed password reached a page")
	}
}

func TestReconnect_ResubscribesAFeedDeletedFromMinifluxByHand(t *testing.T) {
	store, mf := lennyFixture()
	mf.feeds = mf.feeds[:1] // only the unrelated feed with the same alias in its path is left

	rec := post(t, newRRApp(store, mf), "/sources/lenny/reconnect", nil)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/sources/lenny/edit?done=resubscribed" {
		t.Fatalf("status %d location %q\n%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if len(mf.subscribed) != 1 {
		t.Fatalf("subscribed = %+v", mf.subscribed)
	}
	sub := mf.subscribed[0]
	if sub.FeedURL != "https://bridge.example.com/feed/lenny.atom" || sub.CategoryID != 7 || sub.Username != "lenny" {
		t.Errorf("subscription = %+v", sub)
	}
	if store.hashes["lenny"] != HashSecret(sub.Password) {
		t.Errorf("D1 hash %q is not the hash of the password Miniflux got", store.hashes["lenny"])
	}
	if len(mf.updates) != 0 {
		t.Errorf("updated %+v, a feed that is not the Source's", mf.updates)
	}
	if store.sources[0].Status != StatusActive {
		t.Errorf("status = %q, want Reconnect to leave it alone", store.sources[0].Status)
	}
}

func TestReconnect_ResubscribingCreatesTheNewslettersCategoryIfMissing(t *testing.T) {
	store, mf := lennyFixture()
	mf.feeds, mf.categories = nil, []miniflux.Category{{ID: 1, Title: "Tech"}}

	post(t, newRRApp(store, mf), "/sources/lenny/reconnect", nil)

	if len(mf.categories) != 2 || len(mf.subscribed) != 1 || mf.subscribed[0].CategoryID != mf.categories[1].ID {
		t.Errorf("categories %+v, subscribed %+v", mf.categories, mf.subscribed)
	}
}

func TestReconnect_WritesNothingWhenMinifluxIsUnreachable(t *testing.T) {
	store, mf := lennyFixture()
	mf.feedsErr = errors.New("connection refused")

	rec := post(t, newRRApp(store, mf), "/sources/lenny/reconnect", nil)

	if rec.Code != http.StatusBadGateway || store.hashes["lenny"] != "old-hash" {
		t.Fatalf("status %d, hash %q: want 502 and the old password kept", rec.Code, store.hashes["lenny"])
	}
	mustContain(t, rec.Body.String(), "connection refused", "The old password still works")
}

func TestReconnect_LeavesMinifluxAloneWhenD1Fails(t *testing.T) {
	store, mf := lennyFixture()
	store.hashErr = errors.New("d1: 503")

	rec := post(t, newRRApp(store, mf), "/sources/lenny/reconnect", nil)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "d1: 503", "The old password still works")
	if len(mf.updates) != 0 || len(mf.subscribed) != 0 {
		t.Errorf("updates %+v, subscribed %+v", mf.updates, mf.subscribed)
	}
}

func TestReconnect_SaysToRetryWhenMinifluxMissesTheNewPassword(t *testing.T) {
	store, mf := lennyFixture()
	mf.updateErr = errors.New("miniflux: PUT /v1/feeds/42 returned 500: boom")

	rec := post(t, newRRApp(store, mf), "/sources/lenny/reconnect", nil)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "boom", "Reconnect again")
}

func TestReconnect_ReportsAFeedThatStillFailsAfterTheNewPassword(t *testing.T) {
	store, mf := lennyFixture()
	mf.refreshErr = errors.New("miniflux: PUT /v1/feeds/42/refresh returned 400: 401 Unauthorized")

	rec := post(t, newRRApp(store, mf), "/sources/lenny/reconnect", nil)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "Miniflux has the new password", "401 Unauthorized")
	if store.hashes["lenny"] != HashSecret(mf.updates[0].Password) {
		t.Error("D1 and Miniflux disagree on the password")
	}
}

func TestList_LinksEachSourceToItsRenameAndReconnectPage(t *testing.T) {
	store, mf := lennyFixture()

	mustContain(t, get(t, newRRApp(store, mf), "/").Body.String(), `<a href="/sources/lenny/edit">Lenny</a>`)
}

func TestRenameAndEdit_UnknownSourceIs404(t *testing.T) {
	store, mf := lennyFixture()
	app := newRRApp(store, mf)

	if rec := post(t, app, "/sources/nope/rename", url.Values{"display_name": {"X"}}); rec.Code != http.StatusNotFound {
		t.Errorf("rename status = %d", rec.Code)
	}
	if rec := get(t, app, "/sources/nope/edit"); rec.Code != http.StatusNotFound {
		t.Errorf("edit status = %d", rec.Code)
	}
	if rec := post(t, app, "/sources/nope/reconnect", nil); rec.Code != http.StatusNotFound {
		t.Errorf("reconnect status = %d", rec.Code)
	}
	if len(mf.updates)+len(mf.subscribed) != 0 {
		t.Errorf("touched Miniflux for an unknown Source: %+v %+v", mf.updates, mf.subscribed)
	}
}
