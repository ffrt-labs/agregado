package bridgeadmin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/felipeafreitas/agregado/internal/miniflux"
	"github.com/go-chi/chi/v5"
)

// Rename and Reconnect (#231) work on one Source from its edit page.
func (a *App) renameReconnectRoutes(r chi.Router) {
	r.Get("/sources/{alias}/edit", a.editSource)
	r.Post("/sources/{alias}/rename", a.renameSource)
	r.Post("/sources/{alias}/reconnect", a.reconnectSource)
}

type editPage struct {
	Source
	Address string
	Notice  string
	Error   string
}

// feedFor finds the Miniflux feed of a Source (feedsOf). ok is false when
// Miniflux has no such feed.
func (a *App) feedFor(ctx context.Context, alias string) (feed miniflux.Feed, ok bool, err error) {
	feeds, err := a.miniflux.Feeds(ctx)
	if err != nil {
		return miniflux.Feed{}, false, fmt.Errorf("could not read the Miniflux feeds: %w", err)
	}
	if matched := feedsOf(feeds, alias); len(matched) > 0 {
		return matched[0], true, nil
	}
	return miniflux.Feed{}, false, nil
}

func (a *App) editSource(w http.ResponseWriter, r *http.Request) {
	source, ok := a.pathSource(w, r)
	if !ok {
		return
	}
	page := editPage{Source: source}
	switch r.URL.Query().Get("done") {
	case "renamed":
		page.Notice = "Renamed to " + source.DisplayName + ", in the app and in Miniflux."
	case "renamed-no-feed":
		page.Notice = "Renamed to " + source.DisplayName + ". Miniflux has no feed for this Source: Reconnect to subscribe it again under the new name."
	case "reconnected":
		page.Notice = "Reconnected: the feed password was rotated and Miniflux fetched the feed with the new one."
	case "resubscribed":
		page.Notice = "Reconnected: Miniflux had no feed for this Source, so it was subscribed again with a new password."
	}
	a.renderEdit(w, http.StatusOK, page)
}

func (a *App) renderEdit(w http.ResponseWriter, status int, page editPage) {
	page.Address = a.address(page.Alias)
	a.render(w, status, "edit.html", page)
}

func (a *App) renameSource(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	source, ok := a.pathSource(w, r)
	if !ok {
		return
	}
	hadFeed, err := a.rename(ctx, source, strings.TrimSpace(r.PostFormValue("display_name")))
	if err != nil {
		status := http.StatusBadGateway
		var input errInput
		if errors.As(err, &input) {
			status = http.StatusUnprocessableEntity
		} else {
			slog.Error("rename newsletter source failed", "component", "bridge-admin", "alias", source.Alias, "err", err)
		}
		a.renderEdit(w, status, editPage{Source: source, Error: err.Error()})
		return
	}
	slog.Info("newsletter source renamed", "component", "bridge-admin", "alias", source.Alias, "miniflux_feed", hadFeed)
	done := "renamed"
	if !hadFeed {
		done = "renamed-no-feed"
	}
	http.Redirect(w, r, "/sources/"+source.Alias+"/edit?done="+done, http.StatusSeeOther)
}

// rename sets the display name in D1 (the Atom feed title) and the Miniflux
// feed title together; the alias never changes. hadFeed is false when
// Miniflux has no feed for the Source, in which case only D1 changes and a
// Reconnect resubscribes under the new name.
//
// D1 goes first. If Miniflux then refuses the new title, the old name is put
// back in D1 so the two never disagree; the owner just renames again.
func (a *App) rename(ctx context.Context, source Source, name string) (hadFeed bool, err error) {
	if name == "" {
		return false, errInput{"A display name is required."}
	}
	feed, hadFeed, err := a.feedFor(ctx, source.Alias)
	if err != nil {
		return false, fmt.Errorf("%w. Nothing was renamed", err)
	}
	if err := a.store.RenameSource(ctx, source.Alias, name); err != nil {
		return false, fmt.Errorf("could not rename the Source in D1: %w. Nothing was renamed", err)
	}
	if !hadFeed {
		return false, nil
	}
	updateErr := a.miniflux.UpdateFeed(ctx, feed.ID, miniflux.FeedUpdate{Title: name})
	if updateErr == nil {
		return true, nil
	}
	// Roll back on a fresh context: the request's may be what just expired.
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackTimeout)
	defer cancel()
	if err := a.store.RenameSource(rollbackCtx, source.Alias, source.DisplayName); err != nil {
		return true, fmt.Errorf("Miniflux could not retitle the feed (%v), and putting the old name back in D1 failed too (%v); D1 says %q, Miniflux %q. Rename again to line them up", updateErr, err, name, feed.Title)
	}
	return true, fmt.Errorf("Miniflux could not retitle the feed: %v. Nothing was renamed", updateErr)
}

const rollbackTimeout = 30 * time.Second

func (a *App) reconnectSource(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	source, ok := a.pathSource(w, r)
	if !ok {
		return
	}
	resubscribed, err := a.reconnect(ctx, source.Alias)
	if err != nil {
		slog.Error("reconnect newsletter source failed", "component", "bridge-admin", "alias", source.Alias, "err", err)
		a.renderEdit(w, http.StatusBadGateway, editPage{Source: source, Error: err.Error()})
		return
	}
	slog.Info("newsletter source reconnected", "component", "bridge-admin", "alias", source.Alias, "resubscribed", resubscribed)
	done := "reconnected"
	if resubscribed {
		done = "resubscribed"
	}
	http.Redirect(w, r, "/sources/"+source.Alias+"/edit?done="+done, http.StatusSeeOther)
}

// reconnect rotates a Source's feed password: a new one is generated, its
// hash replaces the old in D1, and Miniflux gets the plaintext, either as the
// existing feed's credentials (then a fetch to clear any auth error) or, if
// the feed is gone (matched by feed path, see feedsOf), as a fresh subscription in the
// default category. The plaintext lives only in this function.
//
// Everything that can fail without side effects (reading Miniflux, picking
// the category, generating the password) runs before D1 is written, so those
// failures leave the old password working. Once D1 has the new hash the old
// password is dead; if Miniflux then misses the new one, the feed fails until
// the owner reconnects again, which is always safe to repeat.
func (a *App) reconnect(ctx context.Context, alias string) (resubscribed bool, err error) {
	const untouched = "The old password still works"
	feed, hasFeed, err := a.feedFor(ctx, alias)
	if err != nil {
		return false, fmt.Errorf("%w. %s", err, untouched)
	}
	var category int64
	if !hasFeed {
		if category, err = a.resolveCategory(ctx, ""); err != nil {
			return false, fmt.Errorf("%w. %s", err, untouched)
		}
	}
	secret, err := a.newSecret()
	if err != nil {
		return false, fmt.Errorf("could not generate a feed password: %w. %s", err, untouched)
	}
	if err := a.store.SetSecretHash(ctx, alias, HashSecret(secret)); err != nil {
		return false, fmt.Errorf("could not save the new password hash in D1: %w. %s", err, untouched)
	}

	const retry = "D1 already has the new password, so the feed fails until it gets it: Reconnect again"
	if !hasFeed {
		_, err := a.miniflux.CreateFeed(ctx, miniflux.NewFeed{
			FeedURL:    a.feedURL(alias),
			CategoryID: category,
			Username:   alias, // see add: Miniflux sends Basic auth only with a username
			Password:   secret,
		})
		if err != nil {
			return true, fmt.Errorf("Miniflux could not subscribe: %v. %s", err, retry)
		}
		return true, nil
	}
	if err := a.miniflux.UpdateFeed(ctx, feed.ID, miniflux.FeedUpdate{Username: alias, Password: secret}); err != nil {
		return false, fmt.Errorf("Miniflux could not take the new password: %v. %s", err, retry)
	}
	if err := a.miniflux.RefreshFeed(ctx, feed.ID); err != nil {
		return false, fmt.Errorf("Miniflux has the new password, but fetching the feed with it failed: %v", err)
	}
	return false, nil
}
