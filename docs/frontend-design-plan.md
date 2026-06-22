# Agregado Frontend — "Signal Room" Design Implementation

## Context

Agregado is a personal newsletter/RSS aggregator. Its three web/email surfaces
(`/articles`, `/sources`, the digest email) are currently unstyled structural
HTML — no CSS, no static-asset route, no visual identity. The product's real
differentiator is **AI relevance scoring (1–5)** plus tag categorization, and
none of that data is expressed visually today.

This plan gives Agregado a distinctive, production-ready interface built around
a single signature: a **signal-strength meter** that turns each article's
relevance score into the page's most memorable element — "signal glows, noise
recedes." The direction is a calm dark *instrument console*, deliberately
avoiding the generic newspaper-broadsheet look that this product category
usually defaults to.

**Authorship:** For this frontend work, the agent implements the CSS, templates,
and HTMX wiring (the user's chosen split). Backend logic that does not yet exist
(blocklist persistence, digest history) is flagged per-page as a dependency, not
silently built.

---

## Design System — "Signal Room"

### Color (dark, single-accent)
CSS custom properties on `:root`. One accent family (amber/phosphor) carries all
"signal/live" meaning; categorical color comes only from DB tag hexes.

```
--ink:      #15171C   app background
--panel:    #1D2026   cards / raised surfaces
--panel-2:  #23272F   hover / nested rows
--line:     #2C313B   hairline borders (1px)
--text:     #E7E9ED   primary text
--muted:    #868D9B   metadata / secondary
--faint:    #565D6B   lowest-signal, disabled
--live:     #FFC56E   amber — now/live, high score, unread dot
--live-dim: #8A6A33   low-glow amber — mid score
--ok:       #6FD3A0   healthy feed
--danger:   #F2755C   feed errors, destructive actions
```
Ship dark as the canonical theme. `prefers-color-scheme: light` is an optional
later pass, not in scope now.

### Typography (3 roles, self-hosted woff2 in `static/fonts`, OFL)
- **Display + UI labels:** Space Grotesk (mechanical grotesque — console feel)
- **Reading body** (summaries, future article detail): Newsreader (warm serif)
- **Data / metadata:** JetBrains Mono (datelines, scores, feed health, wordmark)

Scale (base 16px): mono-caption `12px` uppercase +0.08em tracking · body
`17px` Newsreader · list-title `18px` Space Grotesk 500 · eyebrow `12px` mono
upper · H1 `30px` Space Grotesk 600. Line-height 1.6 body, 1.2 titles.

### Layout
- **Console rail** (left, ~220px desktop): wordmark + 5-bar brand glyph, nav
  (Home · Articles · Sources · Digest · Settings). Collapses to a top bar with a
  drawer toggle under 768px.
- **Status strip** (top of main): mono instrument readout —
  `feeds ●N online · ⚠N errors · M unread · last digest 07:02`.
- Content column max-width ~860px. Depth from `--panel` + 1px `--line`, not
  shadows. Radius: 4–6px on controls, square on full-bleed panels.

### Signature — the Signal Meter
Pure-CSS 5-segment bar (no images), reused everywhere a score appears (article
lists, dashboard, digest email). `filled = score`. Color by tier so noise
recedes: score ≥4 → `--live`; =3 → `--live-dim`; ≤2 → `--muted`; empty → `--line`.
Articles scored 1–2 (or blocklist-matched) render **dimmed** with a `── muted ──`
label. Unscored articles (null) show a neutral hollow meter — no penalty.
A 4px version of the glyph sits beside the wordmark as the brand mark.

### Motion (restrained, `prefers-reduced-motion` respected)
- Slow pulse on the "feeds online" dot.
- Meter segments rise/fill on list load and HTMX swap (staggered, ~250ms).
- HTMX requests: thin 2px top progress bar (`htmx-indicator`).

---

## Architecture changes

1. **Static serving** — add to `internal/api/server.go` router:
   `r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))`.
   New dirs: `static/css/app.css`, `static/fonts/*.woff2`, `static/js/app.js`
   (tiny — meter animation + drawer toggle; HTMX stays primary).
2. **Fonts** — self-host woff2 via `@font-face` (offline-safe, production-correct).
3. **Render helper** — `internal/api/render.go` `render()` already composes
   `layout.html` + one content file; reused as-is for full pages. Add the new
   page content templates under `templates/`. New routes need thin handlers
   (presentational glue) — see per-page deps below.

---

## Pages

### Restyle (no backend dependency — do first)

**Global shell — `templates/layout.html`**
Replace bare skeleton with: `<head>` linking `app.css` + fonts + HTMX; body =
console rail + status strip + `{{ template "content" . }}` main. Keep HTMX +
json-enc scripts.

**Articles — `templates/articles.html` + `article_list.html`**
- Each row: signal meter · title (links external, opens new tab) · mono metadata
  line (`tag · source · Nm read · relative time`) · tag chips (DB hex, tinted bg).
- Read-on-click preserved (`hx-post .../read`); add a read/unread visual state
  (dim + ✓) instead of no-op `hx-swap="none"` — swap the row to reflect state.
- Source filter dropdown → styled select in a toolbar; search input with `/`
  focus shortcut + `htmx-indicator`. Empty state: "No signal yet — add a source."
- Pagination → mono prev/next controls.
- `article_list.html` partial mirrors the row markup so HTMX search swaps match.

**Sources — `templates/sources.html`**
- Replace raw table with a card list (still a `<table>` underneath for a11y on
  desktop, card layout on mobile). Per source: name · type badge (rss/newsletter)
  · **health pill** (`--ok` fresh / `--live` stale / `--danger` + ErrorCount on
  error) · last-fetched relative time · refresh + delete actions.
- Add-source form → styled panel; type as a select (rss/newsletter), not free text.
- Delete: add an inline confirm (no native `confirm()` dialog — avoids blocking).

**Digest email — `internal/digest/templates/digest.html`**
- Email-safe **inline-styled** table layout (no external CSS in mail). Mirror the
  Signal Room palette as hardcoded hex. Masthead with date · per-tag sections
  with eyebrow + AI summary in serif · article rows with an inline signal meter
  (table cells or unicode `▇▇▅▁▁` fallback). Leaves room for the Phase 5.6
  👍/👎 feedback links already planned in TODO.

### New pages

**Dashboard / Home — new `GET /` route + handler + `templates/home.html`**
- Sections: status readout (large) · **Top signal today** (highest-scored
  unread, reusing the meter) · **Feed health** grid (reuses source pills) ·
  **Last digest** summary card.
- Data: reuse `ArticleRepository.List` + `SourceLister.List`. Score-ordering is
  best-effort — degrades gracefully where `relevance_score` is null (Phase 5.6
  not yet wired). No new backend required; thin handler only.

**Settings / Preferences — new `GET /settings` + `templates/settings.html`**
- Blocklist editor (add/remove terms, chip UI), digest min-score & cap, digest
  time. **Backend dependency:** `preferences` table exists but the blocklist
  endpoints (TODO 4.6: `GET/PUT /api/preferences/blocklist`) and a preferences
  repo do **not**. Recommendation: build the page against these endpoints; the
  endpoints themselves are Go/systems work — confirm whether the agent adds them
  or the user implements them (their learning domain). Page ships read-only/stub
  until the endpoints land if the user wants to write them.

**Digest archive + Admin/health — new `GET /admin` + `templates/admin.html`**
- Admin panel: live cards polling `/health`, `/health/rabbit`, `/health/db`
  (HTMX `hx-trigger="every 10s"`), plus a "send digest now" button
  (`POST /api/digest/send`) and a digest **preview** pane embedding
  `GET /api/digest/preview` (already returns HTML).
- Digest **history**: lists `digest_logs`. **Backend dependency:** digest-history
  logging (TODO 3.3) is deferred/not implemented, so history is empty until that
  lands — show an empty state. Health + preview + send-now work with existing
  endpoints, no new backend.

### Out of scope (recommended next)
Article **detail** page (TODO 4.3) — the Newsreader reading face is built for it;
worth doing right after this plan to use `Content`/`Summary` fully.

---

## Component library (in `app.css`)
`.meter` (signal bars, `data-score` driven) · `.chip` (tag, DB-hex tinted) ·
`.pill` (source health) · `.badge` (source type) · `.btn` / `.btn-danger` ·
`.toolbar` · `.empty` / `.loading` / `.error` states · `.rail` / `.statusbar`.
Built mobile-first; rail collapses, tables reflow to cards under 768px.

## Sequencing
1. Architecture: static route + fonts + `app.css` tokens & components.
2. Global shell (`layout.html`) + status strip + rail.
3. Restyle Articles (+ partial), Sources — pure FE, no deps.
4. Dashboard/Home (thin handler, existing data).
5. Admin/health + digest preview/send (existing endpoints).
6. Settings (pending blocklist-endpoint decision).
7. Digest email restyle.
8. Polish: empty/loading/error states, reduced-motion, mobile pass.

## Verification
- `docker-compose up` (Postgres + RabbitMQ), run `go run ./cmd/agregado`.
- Browser via claude-in-chrome MCP: load `/`, `/articles`, `/sources`,
  `/settings`, `/admin`; screenshot desktop + 375px mobile; confirm rail
  collapses, meter renders per score, HTMX search/read/refresh still work,
  keyboard focus rings visible, `prefers-reduced-motion` disables meter animation.
- Digest: hit `GET /api/digest/preview`, render the HTML in browser, verify
  email-safe inline styling and inline meter.
- `go build ./...` stays green (new routes/handlers compile).

## Open dependencies to confirm during build
- **Settings/blocklist + digest-history**: agent adds the thin Go endpoints, or
  user implements them as a learning exercise? (Affects whether Settings and
  digest-history ship live or stubbed.)
- **Fonts**: self-host woff2 (planned) vs Google Fonts `<link>` (simpler, CDN
  dependency). Plan assumes self-host.
