package bridgeadmin

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/felipeafreitas/agregado/internal/miniflux"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// DefaultCategory is the Miniflux category a new Source lands in unless
// another is picked. It is created on first use.
const DefaultCategory = "Newsletters"

const statsWindow = 30 * 24 * time.Hour

//go:embed templates/*.html
var templateFS embed.FS

var templates = template.Must(template.New("").Funcs(template.FuncMap{
	"date": func(t *time.Time) string { return t.UTC().Format("2006-01-02") },
	"title": func(status string) string {
		if status == "" {
			return ""
		}
		return strings.ToUpper(status[:1]) + status[1:]
	},
}).ParseFS(templateFS, "templates/*.html"))

// Config is what the app needs to know about the Bridge's addresses.
type Config struct {
	// ReceivingDomain is the domain of every Source's receiving address
	// (<alias>@<domain>), the Resend catch-all.
	ReceivingDomain string
	// BridgeOrigin is the Bridge Worker's scheme and host. New trims a
	// trailing slash; nothing else needs to.
	// A Source's feed is BridgeOrigin + "/feed/<alias>.atom", the URL
	// Miniflux subscribes to. Existing feeds are matched by path only
	// (feedsOf).
	BridgeOrigin string
}

type App struct {
	store     SourceStore
	miniflux  Miniflux
	cfg       Config
	now       func() time.Time
	newSecret func() (string, error)
	mail      ReceivedMail
	objects   ObjectStore
}

// New wires the app to its four systems. mail (Resend) may be nil: Confirm
// and Unclaimed mail then say mail is unavailable. objects (R2) may be nil:
// Delete then fails at the step that removes stored issues.
func New(store SourceStore, mf Miniflux, mail ReceivedMail, objects ObjectStore, cfg Config) *App {
	cfg.BridgeOrigin = strings.TrimRight(cfg.BridgeOrigin, "/")
	return &App{store: store, miniflux: mf, mail: mail, objects: objects, cfg: cfg, now: time.Now, newSecret: NewSecret}
}

func (a *App) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})
	r.Get("/", a.list)
	r.Get("/sources/new", a.newSource)
	r.Post("/sources", a.addSource)
	a.removalRoutes(r)
	a.renameReconnectRoutes(r)
	r.Get("/sources/{alias}/confirm", a.confirm)
	r.Post("/sources/{alias}/activate", a.activate)
	return r
}

func (a *App) feedURL(alias string) string {
	return a.cfg.BridgeOrigin + "/feed/" + alias + ".atom"
}

// feedsOf picks alias's Bridge feeds out of Miniflux's. A feed is matched by
// its URL path, /feed/<alias>.atom, ignoring case, scheme and origin, so a
// feed subscribed under an older Bridge host still counts. List, Rename,
// Reconnect and Delete all match feeds this way.
func feedsOf(feeds []miniflux.Feed, alias string) []miniflux.Feed {
	want := "/feed/" + alias + ".atom"
	var out []miniflux.Feed
	for _, f := range feeds {
		u, err := url.Parse(strings.TrimSpace(f.FeedURL))
		if err == nil && strings.EqualFold(u.Path, want) {
			out = append(out, f)
		}
	}
	return out
}

func (a *App) address(alias string) string {
	return alias + "@" + a.cfg.ReceivingDomain
}

// Fetch health of a Source's Miniflux feed.
const (
	healthOK      = "ok"
	healthFailing = "failing"
	healthPending = "unchecked"
	healthNoFeed  = "missing"
	healthUnknown = "unknown"
)

type sourceRow struct {
	Source
	Address     string
	LastIssueAt *time.Time
	Last30Days  int
	Health      string
	HealthError string
	CheckedAt   *time.Time
}

type listPage struct {
	Rows          []sourceRow
	Added         string
	MinifluxError string
	Activated     string
	Unclaimed     []unclaimedAlias
	MailError     string
}

func (a *App) list(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sources, err := a.store.Sources(ctx)
	if err != nil {
		a.fail(w, "Could not read the Newsletter Sources from D1", err)
		return
	}
	stats, err := a.store.IssueStats(ctx, a.now().Add(-statsWindow))
	if err != nil {
		a.fail(w, "Could not read issue stats from D1", err)
		return
	}

	page := listPage{}
	feeds, feedsErr := a.miniflux.Feeds(ctx)
	if feedsErr != nil {
		slog.Warn("miniflux feeds unavailable", "component", "bridge-admin", "err", feedsErr)
		page.MinifluxError = feedsErr.Error()
	}
	for _, s := range sources {
		row := sourceRow{Source: s, Address: a.address(s.Alias)}
		if st, ok := stats[s.Alias]; ok {
			row.LastIssueAt, row.Last30Days = st.LastIssueAt, st.Last30Days
		}
		if s.Alias == r.URL.Query().Get("added") {
			page.Added = s.DisplayName
		}
		if s.Alias == r.URL.Query().Get("activated") {
			page.Activated = s.DisplayName
		}
		matched := feedsOf(feeds, s.Alias)
		switch {
		case feedsErr != nil:
			row.Health = healthUnknown
		case len(matched) == 0:
			row.Health = healthNoFeed
		case matched[0].ParsingErrorCount > 0:
			row.Health, row.HealthError = healthFailing, matched[0].ParsingErrorMessage
			row.CheckedAt = &matched[0].CheckedAt
		case matched[0].CheckedAt.IsZero():
			row.Health = healthPending
		default:
			row.Health = healthOK
			row.CheckedAt = &matched[0].CheckedAt
		}
		page.Rows = append(page.Rows, row)
	}
	sort.SliceStable(page.Rows, func(i, j int) bool {
		return strings.ToLower(page.Rows[i].DisplayName) < strings.ToLower(page.Rows[j].DisplayName)
	})
	page.Unclaimed, page.MailError = a.unclaimed(ctx, sources)
	a.render(w, http.StatusOK, "list.html", page)
}

type categoryOption struct {
	ID       string
	Title    string
	Selected bool
}

type addPage struct {
	DisplayName string
	Alias       string
	Domain      string
	Categories  []categoryOption
	Error       string
}

func (a *App) newSource(w http.ResponseWriter, r *http.Request) {
	page := addPage{Alias: r.URL.Query().Get("alias")}
	a.renderForm(w, r.Context(), http.StatusOK, page, "")
}

// renderForm fills in the category picker and renders the Add form. selected
// is the category id the owner picked; empty means the default.
func (a *App) renderForm(w http.ResponseWriter, ctx context.Context, status int, page addPage, selected string) {
	page.Domain = a.cfg.ReceivingDomain
	categories, err := a.miniflux.Categories(ctx)
	if err != nil && page.Error == "" {
		page.Error = "Miniflux is unreachable, so no category can be picked: " + err.Error()
	}
	defaultID := ""
	if c, ok := findCategory(categories, DefaultCategory); ok {
		defaultID = strconv.FormatInt(c.ID, 10)
	}
	if selected == "" {
		selected = defaultID
	}
	if defaultID == "" {
		page.Categories = append(page.Categories, categoryOption{Title: DefaultCategory + " (created on save)", Selected: selected == ""})
	}
	for _, c := range categories {
		id := strconv.FormatInt(c.ID, 10)
		page.Categories = append(page.Categories, categoryOption{ID: id, Title: c.Title, Selected: id == selected})
	}
	a.render(w, status, "add.html", page)
}

func findCategory(categories []miniflux.Category, title string) (miniflux.Category, bool) {
	for _, c := range categories {
		if strings.EqualFold(strings.TrimSpace(c.Title), title) {
			return c, true
		}
	}
	return miniflux.Category{}, false
}

// errInput is a problem with what the owner typed: 422 and the form again.
type errInput struct{ msg string }

func (e errInput) Error() string { return e.msg }

func (a *App) addSource(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	page := addPage{
		DisplayName: strings.TrimSpace(r.PostFormValue("display_name")),
		Alias:       strings.TrimSpace(r.PostFormValue("alias")),
	}
	selected := strings.TrimSpace(r.PostFormValue("category_id"))

	source, err := a.add(r.Context(), page.DisplayName, page.Alias, selected)
	if err != nil {
		status := http.StatusBadGateway
		var input errInput
		if errors.As(err, &input) {
			status = http.StatusUnprocessableEntity
		} else {
			slog.Error("add newsletter source failed", "component", "bridge-admin", "alias", page.Alias, "err", err)
		}
		page.Error = err.Error()
		a.renderForm(w, r.Context(), status, page, selected)
		return
	}
	slog.Info("newsletter source added", "component", "bridge-admin", "alias", source.Alias)
	http.Redirect(w, r, "/?added="+source.Alias, http.StatusSeeOther)
}

// add registers a Pending Source and subscribes Miniflux to its feed.
//
// Order matters: the D1 row goes first because Miniflux fetches the feed while
// subscribing, and the Worker 404s an unknown alias. If the subscription then
// fails, the Pending row is removed again, so no Source is left with a feed no
// one can read; the owner fixes the cause and adds it afresh. The password
// lives only in this function: D1 gets its hash, Miniflux the plaintext.
func (a *App) add(ctx context.Context, displayName, alias, categoryID string) (Source, error) {
	if displayName == "" {
		return Source{}, errInput{"A display name is required."}
	}
	if alias == "" {
		alias = SuggestAlias(displayName)
	}
	if err := ValidateAlias(alias); err != nil {
		return Source{}, errInput{"Alias " + strconv.Quote(alias) + ": " + err.Error() + "."}
	}
	taken, err := a.store.SourceExists(ctx, alias)
	if err != nil {
		return Source{}, fmt.Errorf("could not check the alias in D1: %w", err)
	}
	if taken {
		return Source{}, errInput{alias + " is already a Newsletter Source. Pick another alias."}
	}

	category, err := a.resolveCategory(ctx, categoryID)
	if err != nil {
		return Source{}, err
	}

	secret, err := a.newSecret()
	if err != nil {
		return Source{}, fmt.Errorf("could not generate a feed password: %w", err)
	}
	source := Source{Alias: alias, DisplayName: displayName, Status: StatusPending}
	if err := a.store.InsertPending(ctx, source, HashSecret(secret)); err != nil {
		return Source{}, fmt.Errorf("could not save the Source in D1: %w", err)
	}

	_, subErr := a.miniflux.CreateFeed(ctx, miniflux.NewFeed{
		FeedURL:    a.feedURL(alias),
		CategoryID: category,
		// The Worker ignores the username, but Miniflux sends Basic auth only
		// when it is non-empty.
		Username: alias,
		Password: secret,
	})
	if subErr == nil {
		return source, nil
	}
	// Roll back on a fresh context: the request's may be what just expired.
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := a.store.RemovePending(rollbackCtx, alias); err != nil {
		return Source{}, fmt.Errorf("Miniflux could not subscribe (%v), and removing the Pending Source from D1 failed too (%v); remove %s by hand before retrying", subErr, err, alias)
	}
	return Source{}, fmt.Errorf("Miniflux could not subscribe: %v. Nothing was saved", subErr)
}

// resolveCategory turns the picked category id into one Miniflux knows; an
// empty pick means DefaultCategory, found by title or created.
func (a *App) resolveCategory(ctx context.Context, picked string) (int64, error) {
	if picked != "" {
		id, err := strconv.ParseInt(picked, 10, 64)
		if err != nil || id <= 0 {
			return 0, errInput{"Unknown category."}
		}
		return id, nil
	}
	categories, err := a.miniflux.Categories(ctx)
	if err != nil {
		return 0, fmt.Errorf("could not read Miniflux categories: %w", err)
	}
	if c, ok := findCategory(categories, DefaultCategory); ok {
		return c.ID, nil
	}
	created, err := a.miniflux.CreateCategory(ctx, DefaultCategory)
	if err != nil {
		return 0, fmt.Errorf("could not create the %s category in Miniflux: %w", DefaultCategory, err)
	}
	return created.ID, nil
}

var errNoSource = errors.New("no such Newsletter Source")

// aliasParam is the {alias} in the path, or "" if it cannot be one.
func aliasParam(r *http.Request) string {
	alias := chi.URLParam(r, "alias")
	if ValidateAlias(alias) != nil {
		return ""
	}
	return alias
}

// findSource is the Source with this alias, if there is one; the table is
// small, so it is found among all of them.
func (a *App) findSource(ctx context.Context, alias string) (Source, bool, error) {
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

// pathSource is the Source the {alias} in the path names. When there is
// none, or D1 cannot say, it has already answered (404 or 502) and ok is
// false.
func (a *App) pathSource(w http.ResponseWriter, r *http.Request) (source Source, ok bool) {
	alias := aliasParam(r)
	if alias == "" {
		a.notFound(w, chi.URLParam(r, "alias"))
		return Source{}, false
	}
	source, ok, err := a.findSource(r.Context(), alias)
	if err != nil {
		a.fail(w, "Could not read the Newsletter Sources from D1", err)
		return Source{}, false
	}
	if !ok {
		a.notFound(w, alias)
	}
	return source, ok
}

func (a *App) notFound(w http.ResponseWriter, alias string) {
	a.render(w, http.StatusNotFound, "error.html", errorPage{Message: "Not found", Detail: errNoSource.Error() + " " + strconv.Quote(alias)})
}

type errorPage struct {
	Message string
	Detail  string
}

func (a *App) fail(w http.ResponseWriter, message string, err error) {
	slog.Error(message, "component", "bridge-admin", "err", err)
	a.render(w, http.StatusBadGateway, "error.html", errorPage{Message: message, Detail: err.Error()})
}

func (a *App) render(w http.ResponseWriter, status int, name string, data any) {
	var buf strings.Builder
	if err := templates.ExecuteTemplate(&buf, name, data); err != nil {
		slog.Error("render failed", "component", "bridge-admin", "template", name, "err", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	w.Write([]byte(buf.String()))
}
