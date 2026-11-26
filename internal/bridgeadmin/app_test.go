package bridgeadmin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/felipeafreitas/agregado/internal/miniflux"
)

var now = time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

// fakeStore is the Bridge's D1 as the app sees it: Source rows plus per-Source
// issue stats, held in memory.
type fakeStore struct {
	sources   []Source
	hashes    map[string]string
	stats     map[string]IssueStats
	statsFrom time.Time
	insertErr error
	removed   []string
	issues    fakeIssues // delete_test.go
}

func (f *fakeStore) Sources(context.Context) ([]Source, error) { return f.sources, nil }

func (f *fakeStore) IssueStats(_ context.Context, since time.Time) (map[string]IssueStats, error) {
	f.statsFrom = since
	return f.stats, nil
}

func (f *fakeStore) SourceExists(_ context.Context, alias string) (bool, error) {
	for _, s := range f.sources {
		if s.Alias == alias {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeStore) InsertPending(_ context.Context, s Source, hash string) error {
	if f.insertErr != nil {
		return f.insertErr
	}
	s.Status = StatusPending
	f.sources = append(f.sources, s)
	if f.hashes == nil {
		f.hashes = map[string]string{}
	}
	f.hashes[s.Alias] = hash
	return nil
}

func (f *fakeStore) RemovePending(_ context.Context, alias string) error {
	f.removed = append(f.removed, alias)
	kept := f.sources[:0]
	for _, s := range f.sources {
		if !(s.Alias == alias && s.Status == StatusPending) {
			kept = append(kept, s)
		}
	}
	f.sources = kept
	delete(f.hashes, alias)
	return nil
}

// fakeMiniflux holds feeds and categories in memory. createErr makes the next
// subscription fail the way Miniflux does when its synchronous fetch fails.
type fakeMiniflux struct {
	feeds      []miniflux.Feed
	categories []miniflux.Category
	feedsErr   error
	createErr  error
	subscribed []miniflux.NewFeed
	// delete_test.go
	deleteErr    error
	deletedFeeds []int64
}

func (f *fakeMiniflux) Feeds(context.Context) ([]miniflux.Feed, error) { return f.feeds, f.feedsErr }

func (f *fakeMiniflux) Categories(context.Context) ([]miniflux.Category, error) {
	return f.categories, nil
}

func (f *fakeMiniflux) CreateCategory(_ context.Context, title string) (miniflux.Category, error) {
	c := miniflux.Category{ID: int64(100 + len(f.categories)), Title: title}
	f.categories = append(f.categories, c)
	return c, nil
}

func (f *fakeMiniflux) CreateFeed(_ context.Context, feed miniflux.NewFeed) (int64, error) {
	if f.createErr != nil {
		return 0, f.createErr
	}
	f.subscribed = append(f.subscribed, feed)
	return int64(500 + len(f.subscribed)), nil
}

func newTestApp(store *fakeStore, mf *fakeMiniflux) http.Handler {
	app := New(store, mf, Config{
		ReceivingDomain: "news.example.com",
		BridgeOrigin:    "https://bridge.example.com",
	})
	app.now = func() time.Time { return now }
	return app.Routes()
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func post(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func mustContain(t *testing.T, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("page is missing %q\n%s", want, body)
		}
	}
}

func TestList_ShowsEachSourceWithAddressStatusStatsAndFetchHealth(t *testing.T) {
	lastTLDR := time.Date(2026, 11, 25, 7, 30, 0, 0, time.UTC)
	store := &fakeStore{
		sources: []Source{
			{Alias: "tldr", DisplayName: "TLDR", Status: StatusActive},
			{Alias: "dw-berlin-briefing", DisplayName: "DW Berlin Briefing", Status: StatusActive},
			{Alias: "syntax", DisplayName: "Syntax", Status: StatusPending},
		},
		stats: map[string]IssueStats{
			"tldr":               {LastIssueAt: &lastTLDR, Last30Days: 21},
			"dw-berlin-briefing": {LastIssueAt: ptr(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)), Last30Days: 0},
		},
	}
	mf := &fakeMiniflux{feeds: []miniflux.Feed{
		{FeedURL: "https://bridge.example.com/feed/tldr.atom", CheckedAt: now.Add(-time.Hour)},
		{FeedURL: "https://bridge.example.com/feed/dw-berlin-briefing.atom", CheckedAt: now.Add(-time.Hour),
			ParsingErrorCount: 3, ParsingErrorMessage: "401 Unauthorized"},
		{FeedURL: "https://elsewhere.example/feed/syntax.atom"},
	}}

	rec := get(t, newTestApp(store, mf), "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	mustContain(t, body,
		"TLDR", "tldr@news.example.com", "Active", "2026-11-25", ">21<", "Fetch OK",
		"DW Berlin Briefing", "dw-berlin-briefing@news.example.com", "2026-10-01", ">0<",
		"Fetch failing", "401 Unauthorized",
		"Syntax", "syntax@news.example.com", "Pending", "No issues yet", "No Miniflux feed",
	)
	if want := now.Add(-30 * 24 * time.Hour); !store.statsFrom.Equal(want) {
		t.Errorf("stats window starts %v, want %v", store.statsFrom, want)
	}
}

func TestList_StillRendersWhenMinifluxIsDown(t *testing.T) {
	store := &fakeStore{sources: []Source{{Alias: "tldr", DisplayName: "TLDR", Status: StatusActive}}}
	mf := &fakeMiniflux{feedsErr: errors.New("connection refused")}

	rec := get(t, newTestApp(store, mf), "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "TLDR", "Miniflux is unreachable", "connection refused", "Fetch unknown")
}

func TestAddForm_DefaultsToTheNewslettersCategory(t *testing.T) {
	mf := &fakeMiniflux{categories: []miniflux.Category{{ID: 1, Title: "Tech"}, {ID: 7, Title: "newsletters"}}}

	rec := get(t, newTestApp(&fakeStore{}, mf), "/sources/new")

	mustContain(t, rec.Body.String(), `<option value="7" selected>newsletters</option>`, `<option value="1">Tech</option>`)
}

func TestAddForm_OffersToCreateNewslettersWhenMissing(t *testing.T) {
	mf := &fakeMiniflux{categories: []miniflux.Category{{ID: 1, Title: "Tech"}}}

	rec := get(t, newTestApp(&fakeStore{}, mf), "/sources/new")

	mustContain(t, rec.Body.String(), `<option value="" selected>Newsletters (created on save)</option>`)
}

func TestAdd_RegistersAPendingSourceAndSubscribesMinifluxWithItsPassword(t *testing.T) {
	store := &fakeStore{}
	mf := &fakeMiniflux{categories: []miniflux.Category{{ID: 7, Title: "Newsletters"}}}
	app := newTestApp(store, mf)

	rec := post(t, app, "/sources", url.Values{"display_name": {"Lenny's Newsletter"}, "alias": {"lenny"}, "category_id": {"7"}})

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/?added=lenny" {
		t.Fatalf("status %d location %q, want 303 to /?added=lenny\n%s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	if len(store.sources) != 1 || store.sources[0] != (Source{Alias: "lenny", DisplayName: "Lenny's Newsletter", Status: StatusPending}) {
		t.Fatalf("sources = %+v", store.sources)
	}
	if len(mf.subscribed) != 1 {
		t.Fatalf("subscriptions = %+v", mf.subscribed)
	}
	sub := mf.subscribed[0]
	if sub.FeedURL != "https://bridge.example.com/feed/lenny.atom" || sub.CategoryID != 7 || sub.Username != "lenny" {
		t.Errorf("subscription = %+v", sub)
	}
	if len(sub.Password) != 48 || store.hashes["lenny"] != HashSecret(sub.Password) {
		t.Errorf("D1 holds %q, which is not the hash of the password Miniflux got", store.hashes["lenny"])
	}

	list := get(t, app, "/?added=lenny").Body.String()
	mustContain(t, list, "Added Lenny&#39;s Newsletter", "lenny@news.example.com")
	if strings.Contains(list, sub.Password) {
		t.Error("the feed password reached a page; it must be discarded after subscribing")
	}
}

func TestAdd_SuggestsTheAliasWhenLeftBlank(t *testing.T) {
	store := &fakeStore{}
	mf := &fakeMiniflux{categories: []miniflux.Category{{ID: 7, Title: "Newsletters"}}}

	rec := post(t, newTestApp(store, mf), "/sources", url.Values{"display_name": {"DW Berlin Briefing"}, "category_id": {"7"}})

	if rec.Code != http.StatusSeeOther || len(store.sources) != 1 || store.sources[0].Alias != "dw-berlin-briefing" {
		t.Fatalf("status %d, sources %+v", rec.Code, store.sources)
	}
}

func TestAdd_CreatesTheNewslettersCategoryWhenNoneIsPicked(t *testing.T) {
	mf := &fakeMiniflux{categories: []miniflux.Category{{ID: 1, Title: "Tech"}}}

	rec := post(t, newTestApp(&fakeStore{}, mf), "/sources", url.Values{"display_name": {"Syntax"}, "alias": {"syntax"}, "category_id": {""}})

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body)
	}
	if len(mf.categories) != 2 || mf.categories[1].Title != "Newsletters" {
		t.Fatalf("categories = %+v, want Newsletters created", mf.categories)
	}
	if mf.subscribed[0].CategoryID != mf.categories[1].ID {
		t.Errorf("subscribed into category %d, want the new Newsletters (%d)", mf.subscribed[0].CategoryID, mf.categories[1].ID)
	}
}

func TestAdd_ReusesAnExistingNewslettersCategoryCaseInsensitively(t *testing.T) {
	mf := &fakeMiniflux{categories: []miniflux.Category{{ID: 7, Title: "NEWSLETTERS"}}}

	post(t, newTestApp(&fakeStore{}, mf), "/sources", url.Values{"display_name": {"Syntax"}, "alias": {"syntax"}})

	if len(mf.categories) != 1 || len(mf.subscribed) != 1 || mf.subscribed[0].CategoryID != 7 {
		t.Fatalf("categories %+v, subscriptions %+v", mf.categories, mf.subscribed)
	}
}

func TestAdd_RejectsAnInvalidAliasWithoutWritingAnything(t *testing.T) {
	store := &fakeStore{}
	mf := &fakeMiniflux{categories: []miniflux.Category{{ID: 7, Title: "Newsletters"}}}

	rec := post(t, newTestApp(store, mf), "/sources", url.Values{"display_name": {"Syntax"}, "alias": {"Syntax FM"}, "category_id": {"7"}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "lowercase letters, digits and single hyphens", `value="Syntax FM"`, `value="Syntax"`)
	if len(store.sources) != 0 || len(mf.subscribed) != 0 {
		t.Errorf("wrote %+v / %+v despite the invalid alias", store.sources, mf.subscribed)
	}
}

func TestAdd_RejectsATakenAlias(t *testing.T) {
	store := &fakeStore{sources: []Source{{Alias: "tldr", DisplayName: "TLDR", Status: StatusActive}}}
	mf := &fakeMiniflux{categories: []miniflux.Category{{ID: 7, Title: "Newsletters"}}}

	rec := post(t, newTestApp(store, mf), "/sources", url.Values{"display_name": {"TLDR AI"}, "alias": {"tldr"}, "category_id": {"7"}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "tldr is already a Newsletter Source")
	if len(store.sources) != 1 || len(mf.subscribed) != 0 {
		t.Errorf("sources %+v, subscriptions %+v", store.sources, mf.subscribed)
	}
}

func TestAdd_RequiresADisplayName(t *testing.T) {
	rec := post(t, newTestApp(&fakeStore{}, &fakeMiniflux{}), "/sources", url.Values{"display_name": {"  "}, "alias": {"x"}})

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "display name is required")
}

func TestAdd_RollsTheSourceBackWhenMinifluxCannotSubscribe(t *testing.T) {
	store := &fakeStore{}
	mf := &fakeMiniflux{
		categories: []miniflux.Category{{ID: 7, Title: "Newsletters"}},
		createErr:  errors.New("miniflux: POST /v1/feeds returned 400 Bad Request: unauthorized"),
	}

	rec := post(t, newTestApp(store, mf), "/sources", url.Values{"display_name": {"Syntax"}, "alias": {"syntax"}, "category_id": {"7"}})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "Miniflux could not subscribe", "unauthorized", "Nothing was saved")
	if len(store.sources) != 0 || len(store.removed) != 1 || store.removed[0] != "syntax" {
		t.Errorf("after the failed subscription D1 holds %+v (removed %v); want the Pending row rolled back", store.sources, store.removed)
	}
}

func TestAdd_DoesNotSubscribeWhenTheD1InsertFails(t *testing.T) {
	store := &fakeStore{insertErr: errors.New("d1: UNIQUE constraint failed")}
	mf := &fakeMiniflux{categories: []miniflux.Category{{ID: 7, Title: "Newsletters"}}}

	rec := post(t, newTestApp(store, mf), "/sources", url.Values{"display_name": {"Syntax"}, "alias": {"syntax"}, "category_id": {"7"}})

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "UNIQUE constraint failed")
	if len(mf.subscribed) != 0 {
		t.Errorf("subscribed %+v without a D1 row behind it", mf.subscribed)
	}
}

func TestHealth(t *testing.T) {
	rec := get(t, newTestApp(&fakeStore{}, &fakeMiniflux{}), "/health")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"ok"`) {
		t.Fatalf("health = %d %s", rec.Code, rec.Body)
	}
}

func ptr[T any](v T) *T { return &v }
