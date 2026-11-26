package bridgeadmin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

// deleteTimeout bounds one Delete run. It runs on a context detached from the
// request, so closing the tab does not cut it short; whatever it does not
// finish, Delete again resumes.
const deleteTimeout = 5 * time.Minute

func (a *App) removalRoutes(r chi.Router) {
	r.Post("/sources/{alias}/pause", a.pauseSource)
	r.Post("/sources/{alias}/resume", a.resumeSource)
	r.Get("/sources/{alias}/delete", a.confirmDelete)
	r.Post("/sources/{alias}/delete", a.deleteSource)
}

func (a *App) pauseSource(w http.ResponseWriter, r *http.Request) {
	a.setStatus(w, r, "pause", a.store.Pause)
}

func (a *App) resumeSource(w http.ResponseWriter, r *http.Request) {
	a.setStatus(w, r, "resume", a.store.Resume)
}

func (a *App) setStatus(w http.ResponseWriter, r *http.Request, verb string, apply func(context.Context, string) error) {
	alias := aliasParam(r)
	if alias == "" {
		a.notFound(w, chi.URLParam(r, "alias"))
		return
	}
	if err := apply(r.Context(), alias); err != nil {
		a.fail(w, "Could not "+verb+" "+alias+" in D1", err)
		return
	}
	slog.Info("newsletter source "+verb+"d", "component", "bridge-admin", "alias", alias)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type deletePage struct {
	Source
	Address string
	Issues  IssueSummary
	Error   string
}

func (a *App) confirmDelete(w http.ResponseWriter, r *http.Request) {
	source, ok := a.pathSource(w, r)
	if !ok {
		return
	}
	issues, err := a.store.IssueSummary(r.Context(), source.Alias)
	if err != nil {
		a.fail(w, "Could not count "+source.Alias+"'s issues in D1", err)
		return
	}
	a.render(w, http.StatusOK, "delete.html", deletePage{Source: source, Address: a.address(source.Alias), Issues: issues})
}

type deletedPage struct {
	DisplayName string
	Address     string
}

func (a *App) deleteSource(w http.ResponseWriter, r *http.Request) {
	alias := aliasParam(r)
	if alias == "" {
		a.notFound(w, chi.URLParam(r, "alias"))
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), deleteTimeout)
	defer cancel()

	source, found, err := a.findSource(ctx, alias)
	if err != nil {
		a.fail(w, "Could not read the Newsletter Sources from D1", err)
		return
	}
	if !found {
		// No row, e.g. a retry of a run whose answer was lost. Run the steps
		// anyway: they clear leftovers (a feed, objects) if there are any.
		source = Source{Alias: alias, DisplayName: alias}
	}

	removed, err := a.remove(ctx, alias)
	if err == nil && !found && !removed {
		a.notFound(w, alias)
		return
	}
	if err != nil {
		slog.Error("delete newsletter source failed", "component", "bridge-admin", "alias", alias, "err", err)
		page := deletePage{Source: source, Address: a.address(alias), Error: err.Error()}
		// remove paused it if it was Active; show the status it is left in.
		if current, ok, err := a.findSource(ctx, alias); err == nil && ok {
			page.Source = current
		}
		if issues, sumErr := a.store.IssueSummary(ctx, alias); sumErr == nil {
			page.Issues = issues
		}
		a.render(w, http.StatusBadGateway, "delete.html", page)
		return
	}
	slog.Info("newsletter source deleted", "component", "bridge-admin", "alias", alias)
	a.render(w, http.StatusOK, "deleted.html", deletedPage{DisplayName: source.DisplayName, Address: a.address(alias)})
}

// remove deletes a Newsletter Source and everything the Bridge stored for
// it, in the order spec #225 sets:
//
//  1. Pause an Active Source, so n8n ignores mail arriving meanwhile and
//     stores nothing new. A Pending or Paused one is left as it is: n8n
//     already stores nothing for it, and pausing a Pending Source would let
//     Resume skip Activate.
//  2. Remove the Miniflux feed.
//  3. Remove the R2 objects, keyed by entries id.
//  4. Remove the D1 entries rows.
//  5. Remove the sources row.
//
// The entries rows go only after every object is gone, since they are the
// only record of the keys. Every step succeeds on something already removed,
// so after a failure at any point (the Source is then not Active) running remove
// again picks up where it stopped. Articles already in the Article Index,
// with their Scores and Bookmarks, are not touched (ADR-0003 amendment).
//
// leftovers reports whether a feed or stored issues were found to remove; it
// tells a Source-less Delete that cleaned up from one that found nothing.
func (a *App) remove(ctx context.Context, alias string) (leftovers bool, err error) {
	if err := a.store.Pause(ctx, alias); err != nil {
		return leftovers, fmt.Errorf("could not pause the Source in D1, so nothing was removed: %w", err)
	}

	feeds, err := a.miniflux.Feeds(ctx)
	if err != nil {
		return leftovers, fmt.Errorf("could not read Miniflux feeds to remove this one: %w", err)
	}
	matched := feedsOf(feeds, alias)
	leftovers = len(matched) > 0
	for _, feed := range matched {
		if err := a.miniflux.DeleteFeed(ctx, feed.ID); err != nil {
			return leftovers, fmt.Errorf("could not remove the Miniflux feed: %w", err)
		}
	}

	ids, err := a.store.EntryIDs(ctx, alias)
	if err != nil {
		return leftovers, fmt.Errorf("could not list the stored issues in D1: %w", err)
	}
	leftovers = leftovers || len(ids) > 0
	if len(ids) > 0 && a.objects == nil {
		return leftovers, errors.New("could not remove the stored issues: R2 is not configured")
	}
	for i, id := range ids {
		if err := a.objects.DeleteObject(ctx, id); err != nil {
			return leftovers, fmt.Errorf("could not remove stored issue %d of %d from R2: %w", i+1, len(ids), err)
		}
	}

	if err := a.store.DeleteEntries(ctx, alias); err != nil {
		return leftovers, fmt.Errorf("could not remove the issues from D1: %w", err)
	}
	if err := a.store.DeleteSource(ctx, alias); err != nil {
		return leftovers, fmt.Errorf("could not remove the Source row from D1: %w", err)
	}
	return leftovers, nil
}
