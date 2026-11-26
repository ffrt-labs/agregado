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

// fakeIssues is the D1 entries table as Delete sees it: entry ids per Source
// alias, with their published times, plus one-shot failures by step name
// ("pause", "entryIDs", "deleteEntries", "deleteSource").
type fakeIssues struct {
	entries map[string][]fakeEntry
	failing map[string]error
}

type fakeEntry struct {
	id          string
	publishedAt time.Time
}

func (f *fakeStore) fail(step string) error {
	if err, ok := f.issues.failing[step]; ok {
		delete(f.issues.failing, step)
		return err
	}
	return nil
}

func (f *fakeStore) setStatus(alias, status string, onlyFrom ...string) {
	for i, s := range f.sources {
		if s.Alias != alias {
			continue
		}
		if len(onlyFrom) > 0 && s.Status != onlyFrom[0] {
			return
		}
		f.sources[i].Status = status
	}
}

func (f *fakeStore) Pause(_ context.Context, alias string) error {
	if err := f.fail("pause"); err != nil {
		return err
	}
	f.setStatus(alias, StatusPaused, StatusActive)
	return nil
}

func (f *fakeStore) Resume(_ context.Context, alias string) error {
	f.setStatus(alias, StatusActive, StatusPaused)
	return nil
}

func (f *fakeStore) IssueSummary(_ context.Context, alias string) (IssueSummary, error) {
	var summary IssueSummary
	for _, e := range f.issues.entries[alias] {
		summary.Count++
		if summary.LastIssueAt == nil || e.publishedAt.After(*summary.LastIssueAt) {
			at := e.publishedAt
			summary.LastIssueAt = &at
		}
	}
	return summary, nil
}

func (f *fakeStore) EntryIDs(_ context.Context, alias string) ([]string, error) {
	if err := f.fail("entryIDs"); err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range f.issues.entries[alias] {
		ids = append(ids, e.id)
	}
	return ids, nil
}

func (f *fakeStore) DeleteEntries(_ context.Context, alias string) error {
	if err := f.fail("deleteEntries"); err != nil {
		return err
	}
	delete(f.issues.entries, alias)
	return nil
}

func (f *fakeStore) DeleteSource(_ context.Context, alias string) error {
	if err := f.fail("deleteSource"); err != nil {
		return err
	}
	if len(f.issues.entries[alias]) > 0 {
		return errors.New("d1: FOREIGN KEY constraint failed")
	}
	kept := f.sources[:0]
	for _, s := range f.sources {
		if s.Alias != alias {
			kept = append(kept, s)
		}
	}
	f.sources = kept
	return nil
}

func (f *fakeMiniflux) DeleteFeed(_ context.Context, id int64) error {
	if f.deleteErr != nil {
		err := f.deleteErr
		f.deleteErr = nil
		return err
	}
	kept := f.feeds[:0]
	for _, feed := range f.feeds {
		if feed.ID != id {
			kept = append(kept, feed)
		}
	}
	f.feeds = kept
	f.deletedFeeds = append(f.deletedFeeds, id)
	return nil
}

// fakeObjects is the R2 bucket: the keys present. failAfter, when positive,
// lets that many deletes through and then fails the next one (once).
type fakeObjects struct {
	keys      map[string]bool
	failAfter int
	deletes   int
}

func (f *fakeObjects) DeleteObject(_ context.Context, key string) error {
	if f.failAfter > 0 && f.deletes == f.failAfter {
		f.failAfter = 0
		return errors.New("r2: delete returned 500 Internal Server Error: boom")
	}
	f.deletes++
	delete(f.keys, key)
	return nil
}

// deleteFixture is one Active Source, "syntax", with three stored issues, a
// Miniflux feed and the three R2 objects, next to another Source that must be
// left alone.
func deleteFixture() (*fakeStore, *fakeMiniflux, *fakeObjects) {
	store := &fakeStore{
		sources: []Source{
			{Alias: "syntax", DisplayName: "Syntax", Status: StatusActive},
			{Alias: "tldr", DisplayName: "TLDR", Status: StatusActive},
		},
		issues: fakeIssues{entries: map[string][]fakeEntry{
			"syntax": {
				{id: "e1", publishedAt: time.Date(2026, 11, 1, 8, 0, 0, 0, time.UTC)},
				{id: "e2", publishedAt: time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)},
				{id: "e3", publishedAt: time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)},
			},
			"tldr": {{id: "t1", publishedAt: time.Date(2026, 11, 25, 8, 0, 0, 0, time.UTC)}},
		}, failing: map[string]error{}},
	}
	mf := &fakeMiniflux{feeds: []miniflux.Feed{
		{ID: 11, FeedURL: "https://bridge.example.com/feed/syntax.atom"},
		{ID: 12, FeedURL: "https://bridge.example.com/feed/tldr.atom"},
	}}
	objects := &fakeObjects{keys: map[string]bool{"e1": true, "e2": true, "e3": true, "t1": true}}
	return store, mf, objects
}

func newDeleteApp(store *fakeStore, mf *fakeMiniflux, objects *fakeObjects) http.Handler {
	app := New(store, mf, nil, objects, Config{ReceivingDomain: "news.example.com", BridgeOrigin: "https://bridge.example.com"})
	app.now = func() time.Time { return now }
	return app.Routes()
}

func statusOf(store *fakeStore, alias string) string {
	for _, s := range store.sources {
		if s.Alias == alias {
			return s.Status
		}
	}
	return "(deleted)"
}

func TestPause_PausesTheSourceAndKeepsEverything(t *testing.T) {
	store, mf, objects := deleteFixture()
	app := newDeleteApp(store, mf, objects)

	rec := post(t, app, "/sources/syntax/pause", nil)

	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/" {
		t.Fatalf("status %d location %q, want 303 to /", rec.Code, rec.Header().Get("Location"))
	}
	if got := statusOf(store, "syntax"); got != StatusPaused {
		t.Errorf("status = %s, want paused", got)
	}
	if len(store.issues.entries["syntax"]) != 3 || len(mf.feeds) != 2 || len(objects.keys) != 4 {
		t.Errorf("Pause removed something: entries %v, feeds %v, objects %v", store.issues.entries, mf.feeds, objects.keys)
	}
	mustContain(t, get(t, app, "/").Body.String(), "Paused", `action="/sources/syntax/resume"`)
}

func TestResume_MakesAPausedSourceActiveAgain(t *testing.T) {
	store, mf, objects := deleteFixture()
	store.sources[0].Status = StatusPaused
	app := newDeleteApp(store, mf, objects)

	rec := post(t, app, "/sources/syntax/resume", nil)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status = %d", rec.Code)
	}
	if got := statusOf(store, "syntax"); got != StatusActive {
		t.Errorf("status = %s, want active", got)
	}
	mustContain(t, get(t, app, "/").Body.String(), `action="/sources/syntax/pause"`)
}

// Only an Active Source can be paused, so a Paused one always had been
// Active and Resume can never let a Pending Source skip Activate.
func TestPause_LeavesAPendingSourcePending(t *testing.T) {
	store, mf, objects := deleteFixture()
	store.sources[0].Status = StatusPending
	app := newDeleteApp(store, mf, objects)

	post(t, app, "/sources/syntax/pause", nil)
	post(t, app, "/sources/syntax/resume", nil)

	if got := statusOf(store, "syntax"); got != StatusPending {
		t.Errorf("status = %s, want pending: only Activate makes a Pending Source active", got)
	}
}

func TestList_OffersNoPauseOnAPendingSource(t *testing.T) {
	store, mf, objects := deleteFixture()
	store.sources[0].Status = StatusPending

	body := get(t, newDeleteApp(store, mf, objects), "/").Body.String()

	if strings.Contains(body, `action="/sources/syntax/pause"`) || strings.Contains(body, `action="/sources/syntax/resume"`) {
		t.Errorf("a Pending row offers Pause or Resume\n%s", body)
	}
	mustContain(t, body, `action="/sources/tldr/pause"`, `href="/sources/syntax/delete"`)
}

func TestDelete_AFailedDeleteOfAPendingSourceCannotBeResumedIntoActive(t *testing.T) {
	store, mf, objects := deleteFixture()
	store.sources[0].Status = StatusPending
	mf.deleteErr = errors.New("miniflux: DELETE /v1/feeds/11 returned 500: boom")
	app := newDeleteApp(store, mf, objects)

	rec := post(t, app, "/sources/syntax/delete", nil)
	post(t, app, "/sources/syntax/resume", nil)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "stays Pending")
	if got := statusOf(store, "syntax"); got != StatusPending {
		t.Errorf("status = %s, want pending", got)
	}
}

func TestList_OffersPauseAndDeleteForEachSource(t *testing.T) {
	store, mf, objects := deleteFixture()

	body := get(t, newDeleteApp(store, mf, objects), "/").Body.String()

	mustContain(t, body, `action="/sources/syntax/pause"`, `href="/sources/syntax/delete"`, `href="/sources/tldr/delete"`)
}

func TestDeleteConfirmation_SaysWhatGoes(t *testing.T) {
	store, mf, objects := deleteFixture()

	rec := get(t, newDeleteApp(store, mf, objects), "/sources/syntax/delete")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "Syntax", "syntax@news.example.com", "3 issues, last on 2026-11-20",
		`action="/sources/syntax/delete"`)
	if statusOf(store, "syntax") != StatusActive || len(objects.keys) != 4 {
		t.Error("showing the confirmation changed something")
	}
}

func TestDeleteConfirmation_UnknownSourceIs404(t *testing.T) {
	store, mf, objects := deleteFixture()

	if rec := get(t, newDeleteApp(store, mf, objects), "/sources/nope/delete"); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDelete_RemovesFeedObjectsEntriesAndSourceAndRemindsToUnsubscribe(t *testing.T) {
	store, mf, objects := deleteFixture()

	rec := post(t, newDeleteApp(store, mf, objects), "/sources/syntax/delete", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body)
	}
	mustContain(t, rec.Body.String(), "Deleted Syntax", "unsubscribe", "syntax@news.example.com")
	assertSyntaxGoneAndTLDRKept(t, store, mf, objects)
}

func assertSyntaxGoneAndTLDRKept(t *testing.T, store *fakeStore, mf *fakeMiniflux, objects *fakeObjects) {
	t.Helper()
	if got := statusOf(store, "syntax"); got != "(deleted)" {
		t.Errorf("syntax is still a Source (%s)", got)
	}
	if _, ok := store.issues.entries["syntax"]; ok {
		t.Errorf("syntax entries left: %v", store.issues.entries["syntax"])
	}
	for _, key := range []string{"e1", "e2", "e3"} {
		if objects.keys[key] {
			t.Errorf("R2 object %s left", key)
		}
	}
	if len(mf.feeds) != 1 || mf.feeds[0].ID != 12 {
		t.Errorf("feeds = %+v, want only tldr's", mf.feeds)
	}
	if statusOf(store, "tldr") != StatusActive || len(store.issues.entries["tldr"]) != 1 || !objects.keys["t1"] {
		t.Error("deleting syntax touched tldr")
	}
}

func TestDelete_RemovesTheFeedWhateverItsOrigin(t *testing.T) {
	store, mf, objects := deleteFixture()
	mf.feeds[0].FeedURL = "http://bridge-old.workers.dev/feed/Syntax.atom"

	rec := post(t, newDeleteApp(store, mf, objects), "/sources/syntax/delete", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body)
	}
	assertSyntaxGoneAndTLDRKept(t, store, mf, objects)
}

func TestDelete_PausesBeforeTouchingAnythingElse(t *testing.T) {
	store, mf, objects := deleteFixture()
	mf.deleteErr = errors.New("miniflux: DELETE /v1/feeds/11 returned 500: boom")

	post(t, newDeleteApp(store, mf, objects), "/sources/syntax/delete", nil)

	if got := statusOf(store, "syntax"); got != StatusPaused {
		t.Errorf("status = %s, want paused once Delete has started", got)
	}
}

func TestDelete_StopsIfPauseFails(t *testing.T) {
	store, mf, objects := deleteFixture()
	store.issues.failing["pause"] = errors.New("d1: network down")

	rec := post(t, newDeleteApp(store, mf, objects), "/sources/syntax/delete", nil)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "network down")
	if len(mf.deletedFeeds) != 0 || objects.deletes != 0 {
		t.Error("Delete went on although the Source could not be paused")
	}
}

// Each step can fail; the Source then stays Paused, the page shows the error
// and offers Delete again, and a second Delete finishes the job.
func TestDelete_InterruptedAtAnyStepResumesOnRetry(t *testing.T) {
	steps := map[string]func(*fakeStore, *fakeMiniflux, *fakeObjects){
		"miniflux feed": func(_ *fakeStore, mf *fakeMiniflux, _ *fakeObjects) {
			mf.deleteErr = errors.New("miniflux: DELETE /v1/feeds/11 returned 500: boom")
		},
		"listing entries": func(s *fakeStore, _ *fakeMiniflux, _ *fakeObjects) {
			s.issues.failing["entryIDs"] = errors.New("d1: boom")
		},
		"r2 objects midway": func(_ *fakeStore, _ *fakeMiniflux, o *fakeObjects) {
			o.failAfter = 1
		},
		"d1 entries": func(s *fakeStore, _ *fakeMiniflux, _ *fakeObjects) {
			s.issues.failing["deleteEntries"] = errors.New("d1: boom")
		},
		"source row": func(s *fakeStore, _ *fakeMiniflux, _ *fakeObjects) {
			s.issues.failing["deleteSource"] = errors.New("d1: boom")
		},
	}
	for name, breakIt := range steps {
		t.Run(name, func(t *testing.T) {
			store, mf, objects := deleteFixture()
			breakIt(store, mf, objects)
			app := newDeleteApp(store, mf, objects)

			first := post(t, app, "/sources/syntax/delete", nil)

			if first.Code != http.StatusBadGateway {
				t.Fatalf("first delete: status = %d, want 502", first.Code)
			}
			mustContain(t, first.Body.String(), "boom", "Delete again", `action="/sources/syntax/delete"`)
			if got := statusOf(store, "syntax"); got != StatusPaused {
				t.Fatalf("after the failure status = %s, want paused", got)
			}
			mustContain(t, get(t, app, "/").Body.String(), "Syntax", "Paused")

			second := post(t, app, "/sources/syntax/delete", url.Values{})

			if second.Code != http.StatusOK {
				t.Fatalf("second delete: status = %d\n%s", second.Code, second.Body)
			}
			mustContain(t, second.Body.String(), "Deleted Syntax")
			assertSyntaxGoneAndTLDRKept(t, store, mf, objects)
		})
	}
}

// Deleting an alias that has no Source and nothing left behind (an unknown
// alias, or a second Delete after the first finished) is a 404, not a
// "Deleted" page for something that was never there.
func TestDelete_AnAliasWithNothingLeftIs404(t *testing.T) {
	store, mf, objects := deleteFixture()
	app := newDeleteApp(store, mf, objects)
	post(t, app, "/sources/syntax/delete", nil)

	for _, alias := range []string{"syntax", "nope"} {
		rec := post(t, app, "/sources/"+alias+"/delete", nil)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404\n%s", alias, rec.Code, rec.Body)
		}
		mustContain(t, rec.Body.String(), "no such Newsletter Source")
	}
}

// A Delete whose Source row is gone but which left something behind (here a
// Miniflux feed) still clears it and reports Deleted.
func TestDelete_AGoneSourceWithLeftoversIsCleanedUp(t *testing.T) {
	store, mf, objects := deleteFixture()
	store.sources = store.sources[1:]
	delete(store.issues.entries, "syntax")

	rec := post(t, newDeleteApp(store, mf, objects), "/sources/syntax/delete", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d\n%s", rec.Code, rec.Body)
	}
	mustContain(t, rec.Body.String(), "Deleted syntax")
	if len(mf.feeds) != 1 || mf.feeds[0].ID != 12 {
		t.Errorf("feeds = %+v, want syntax's removed", mf.feeds)
	}
}

func TestDelete_FailsClearlyWhenMinifluxIsUnreachable(t *testing.T) {
	store, mf, objects := deleteFixture()
	mf.feedsErr = errors.New("connection refused")

	rec := post(t, newDeleteApp(store, mf, objects), "/sources/syntax/delete", nil)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d", rec.Code)
	}
	mustContain(t, rec.Body.String(), "connection refused")
	if objects.deletes != 0 || len(store.issues.entries["syntax"]) != 3 {
		t.Error("went past the Miniflux step without knowing whether the feed was removed")
	}
}
