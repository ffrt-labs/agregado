# Karakeep (formerly Hoarder) — what it's made of, and what it weighs

**Research ticket:** [#50](https://github.com/ffrt-labs/agregado/issues/50)
**Date:** 2026-08-22
**Sources:** primary only — `github.com/karakeep-app/karakeep` and `docs.karakeep.app`. No blog posts, no forums.

**Versions this applies to:**
- Latest published server release: **v0.33.2** (2026-08-11).
- Source inspected at `main` commit `7ca5ee236b96cd659096b1530c5be3fdcd8e4470` (2026-08-22), which is post-v0.33.2. Where `main` and v0.33.2 could differ, this is flagged.
- Satellite artefacts version separately: CLI `@karakeep/cli` **0.33.1** (`apps/cli/package.json`), MCP server `@karakeep/mcp` **0.33.0** (`apps/mcp/package.json`), browser extension **1.2.11** (`apps/browser-extension/manifest.json`), mobile apps tagged `ios/v1.10.0-1` and `android/v1.10.0-1`.
- Docs site is versioned; current docs correspond to v0.33.0 (`docs/versioned_docs/version-v0.33.0/`).

---

## 1. Data model

### Storage engines — there is exactly one database, and it's SQLite

Karakeep does **not** use Postgres. The Drizzle schema uses `sqliteTable` for every single table (`packages/db/schema.ts`, lines 45–1060 — 35 tables, all `sqliteTable`). The database file is `${DATA_DIR}/db.db` (`packages/db/drizzle.config.ts:7-9`).

There are four distinct places state lives:

| Store | What lives there | Evidence |
|---|---|---|
| SQLite `${DATA_DIR}/db.db` | All bookmark metadata, tags, lists, users, rules, webhooks, highlights, chat sessions | `packages/db/drizzle.config.ts:7-9`, `packages/db/schema.ts` |
| SQLite `${DATA_DIR}/queue.db` | The job queue itself (liteque) | `packages/plugins/queue-liteque/src/index.ts:66` — `path.join(serverConfig.dataDir, "queue.db")` |
| Filesystem `${DATA_DIR}/assets` **or** S3 | Binary assets: screenshots, PDFs, videos, full-page archives, uploads | `packages/shared/config.ts:456` (`assetsDir: ASSETS_DIR ?? path.join(DATA_DIR, "assets")`); S3 path via `ASSET_STORE_S3_*` vars at `packages/shared/config.ts:202-207`, resolved at `:489-498`; `packages/shared/assetdb.ts` implements both backends |
| Meilisearch | Full-text search index **and** (since v0.33.x) the vector store for semantic search | `docker/docker-compose.yml`, `packages/plugins/search-meilisearch/`, `packages/plugins/vectorstore-meilisearch/` |

Note the asset store is pluggable to S3-compatible object storage but defaults to plain filesystem. Assets are **not** in the database.

### What a bookmark is

The `bookmarks` table (`packages/db/schema.ts:205-276`) is a thin common header. Its columns: `id`, `dbCreatedAt`/`createdAt` (stored as `lastSavedAt`) / `modifiedAt`, `title`, `archived`, `favourited`, `userId`, `taggingStatus`, `summarizationStatus`, `embeddingStatus`, `summary`, `note`, `type`, `source`.

`source` is an enum of capture surfaces: `api | web | extension | cli | mobile | singlefile | rss | import` (`schema.ts:245-256`). Useful: the rule engine can branch on it (`bookmarkSourceIs`).

`type` has only **three** values (`schema.ts:241-244`, `BookmarkTypes`):

1. **`link`** → detail row in `bookmarkLinks` (`schema.ts:281-320`): `url`, plus crawler-extracted `title`, `description`, `author`, `publisher`, `datePublished`, `dateModified`, `imageUrl`, `favicon`, `htmlContent`, `contentAssetId`, reader-view assessment fields, `crawledAt`, `crawlStatus`, `crawlStatusCode`.
2. **`text`** → `bookmarkTexts` (`schema.ts:437-445`): just `text` + `sourceUrl`. This is the note type.
3. **`asset`** → `bookmarkAssets` (`schema.ts:447-459`): `assetType` enum is **`"image" | "pdf"` only**, plus `assetId`, `content` (extracted text/OCR), `metadata`, `fileName`, `sourceUrl`.

**So the first-class content types are: link, plain text/note, image, PDF. That's it.**

Video and audio are *not* first-class bookmark types. Video exists only as an *attachment to a link* bookmark: `AssetTypes.LINK_VIDEO` (`schema.ts:322-336`), downloaded by `apps/workers/workers/videoWorker.ts` via yt-dlp, and gated behind `CRAWLER_VIDEO_DOWNLOAD` which **defaults to `false`** with a 50 MB cap (`packages/shared/config.ts` — `CRAWLER_VIDEO_DOWNLOAD: stringBool("false")`, `CRAWLER_VIDEO_DOWNLOAD_MAX_SIZE: default(50)`). Video files can also be *uploaded* (`VIDEO_ASSET_TYPES` is in `SUPPORTED_UPLOAD_ASSET_TYPES`, `packages/shared/assetdb.ts:50-56`) but `SUPPORTED_BOOKMARK_ASSET_TYPES` is narrower. **Audio is nowhere** — there is no audio MIME type in `ASSET_TYPES` (`packages/shared/assetdb.ts:23-34`: gif, jpeg, png, webp, pdf, zip, html, mp4, webm, mkv).

### Everything hanging off a bookmark

`assets` (13 asset types: `linkBannerImage`, `linkScreenshot`, `linkPdf`, `assetScreenshot`, `linkFullPageArchive`, `linkPrecrawledArchive`, `linkVideo`, `linkHtmlContent`, `bookmarkAsset`, `userUploaded`, `avatar`, `backup`, `unknown` — `schema.ts:322-336`), `bookmarkTags` + `tagsOnBookmarks` (with `attachedBy` human/AI provenance), `bookmarkLists` + `bookmarksInLists` (manual and smart/query lists, nestable via `parentId`, shareable via `listCollaborators`/`listInvitations`), `highlights` (offset-anchored, 4 colours, with notes), `userReadingProgress`, `customPrompts`, `chatSessions`/`chatMessages`, `rssFeedsTable`, `webhooksTable`, `ruleEngineRulesTable`/`ruleEngineActionsTable`, `backupsTable`, `importSessions`/`importSessionBookmarks`/`importStagingBookmarks`, `invites`, `subscriptions` (Stripe).

That last one is a tell: `subscriptions`, `STRIPE_*` env vars, `FREE_QUOTA_*`/`PAID_QUOTA_*` limits (`packages/shared/config.ts`) — this codebase carries a **hosted-SaaS multi-tenant billing layer** that a single-user install pays for in schema surface but never uses.

---

## 2. Operational weight

### The official compose file: three containers

`docker/docker-compose.yml` (verbatim structure):

| Service | Image | Job |
|---|---|---|
| `web` | `ghcr.io/karakeep-app/karakeep:${KARAKEEP_VERSION:-release}` | Everything: Next.js web UI, REST API, tRPC, **and all background workers**, in one container |
| `chrome` | `ghcr.io/karakeep-app/karakeep-chrome:release` | Headless Chrome over CDP on port 9222, for crawling and screenshots |
| `meilisearch` | `getmeili/meilisearch:v1.41.0` | Full-text search index + vector store |

Two named volumes: `data` (SQLite + assets) and `meilisearch`. One published port: 3000.

There is **no Redis and no separate worker container** in the modern setup. Both were removed in v0.16 (`docs/docs/06-administration/07-legacy-container-upgrade.md`): "Karakeep's 0.16 release consolidated the web and worker containers into a single container and also dropped the need for the redis container." Redis is now optional and only used for distributed rate limiting (`REDIS_URL`, `packages/plugins/ratelimit-redis/`); the default rate limiter is in-memory and `RATE_LIMITING_ENABLED` defaults to `false`.

### The workers inside the one container

`apps/workers/workers/` contains 11 workers: `crawlerWorker`, `inference` (tagging/summarization), `embeddingsWorker`, `searchWorker`, `videoWorker`, `assetPreprocessingWorker`, `feedWorker` (RSS), `ruleEngineWorker`, `webhookWorker`, `importWorker`, `backupWorker`, plus `adminMaintenanceWorker`. Each is individually gateable via `WORKERS_ENABLED_WORKERS` / `WORKERS_DISABLED_WORKERS` (`packages/shared/config.ts:38-55`) and each has its own `*_NUM_WORKERS` concurrency knob, all defaulting to `1`.

The queue is **liteque**, a SQLite-backed queue (`packages/plugins/queue-liteque/src/index.ts`). A Restate-backed queue plugin also exists (`packages/plugins/queue-restate/`) for distributed deployments — not used by the default compose.

### Minimal install: one container

`docs/docs/02-installation/07-minimal-install.md` documents a single-container `docker run` with no Meilisearch, no Chrome, no AI provider. The stated cost:

> "If you run without meilisearch, the search functionality will be completely disabled. If you run without chrome, crawling will still work, but you'll lose ability to take screenshots of websites and websites with javascript content won't get crawled correctly. If you don't setup OpenAI/Ollama, AI tagging will be disabled."

Note "search functionality will be **completely** disabled" — there is no SQLite FTS fallback. Meilisearch is required for search, confirmed by `MEILI_ADDR` docs: "If not set, Search will be disabled" (`docs/docs/03-configuration/01-environment-variables.md:17`).

### What's baked into the main image

`docker/Dockerfile` builds a fat runtime: Node 24 slim base, plus `graphicsmagick`, `ghostscript`, `ffmpeg` (lines 118-127), a `yt-dlp` binary downloaded at build time (lines 130-137), and a **Rust-compiled `monolith` binary** built in a separate `rust:1-bookworm` builder stage (lines 2-9, copied at 147). Tesseract OCR is also wired in (`OCR_LANGS`, `OCR_CONFIDENCE_THRESHOLD`, `OCR_USE_LLM` config). This is a heavy image by construction.

### Resource footprint per the docs

**Finding: the docs do not publish RAM/CPU requirements anywhere.** Grepping `docs/docs/02-installation/*.md` and `docs/docs/06-administration/*.md` for memory/RAM/hardware turns up only "Requirements: Docker, Docker Compose" (`docs/docs/02-installation/01-docker.md:3-6`). The only memory number in the entire configuration surface is `CRAWLER_PARSER_MEM_LIMIT_MB` (default **512**), a per-parse cap inside the crawler (`packages/shared/config.ts`). So any RAM claim about Karakeep is a vibe, including a pessimistic one — but note the components involved (a Next.js server + Node workers + a headless Chrome + a Meilisearch instance holding an inverted index *and* float vectors) are individually well-characterised and none of them are small.

### Upgrades

Migrations run **automatically on container start**, as an s6 oneshot that both `svc-web` and `svc-workers` depend on: `docker/root/etc/s6-overlay/s6-rc.d/init-db-migration/run` runs `cd /db_migrations; exec node index.js`, and `svc-web/dependencies.d/init-db-migration` + `svc-workers/dependencies.d/init-db-migration` block the services until it completes. There are **94 migration files** in `packages/db/drizzle/` (`0000_*` through `0093_reader_view_assessment.sql`). No manual migration step for the normal path.

Breaking-change history (from GitHub release notes, primary):

- **v0.16** — web+workers containers merged; Redis container dropped. Requires editing your compose file (`docs/docs/06-administration/07-legacy-container-upgrade.md`).
- **Hoarder → Karakeep rename** — image paths changed from `ghcr.io/hoarder-app/hoarder-web` to `ghcr.io/karakeep-app/karakeep`; a whole migration doc exists (`docs/docs/06-administration/08-hoarder-to-karakeep-migration.md`).
- **v0.30.0** (2026-01-01) — "Breaking: In bookmark APIs `includeContent` now defaults to `false`." Announced months prior. This is an API-consumer-visible break.
- **v0.32.0** (2026-05-08) — Meilisearch version bump requiring users to follow Meilisearch's own upgrade guide.
- **v0.33.1** (2026-08-01) — embeddings use Meilisearch as the default vector store; older Meilisearch versions must be upgraded.
- **v0.33.2** (2026-08-11) — Chrome image swapped from the abandoned `gcr.io/zenika-hub/alpine-chrome:124` to `ghcr.io/karakeep-app/karakeep-chrome`; **requires a compose edit** — remote-debugging args must be removed or the container breaks (`docs/docs/06-administration/09-chrome-image-migration.md`).

Read: roughly one compose-file-editing upgrade per year, plus dependency-version coupling to Meilisearch. Not "docker compose pull and forget", but not churn either.

---

## 3. API surface

The API is a first-class, documented, OpenAPI-3.0-specified REST surface, bearer-token authenticated. Spec: `packages/open-api/karakeep-openapi-spec.json` (`info.version: 1.0.0`); rendered reference at <https://docs.karakeep.app/api/karakeep-api>. Route implementations: `packages/api/routes/`.

Full path list from the spec:

```
/bookmarks                                  GET, POST
/bookmarks/search                           GET
/bookmarks/check-url                        GET
/bookmarks/{bookmarkId}                     GET, PATCH, DELETE
/bookmarks/{bookmarkId}/content             GET
/bookmarks/{bookmarkId}/summarize           POST
/bookmarks/{bookmarkId}/tags                POST, DELETE
/bookmarks/{bookmarkId}/lists               GET
/bookmarks/{bookmarkId}/highlights          GET
/bookmarks/{bookmarkId}/assets              POST
/bookmarks/{bookmarkId}/assets/{assetId}    PUT, DELETE
/lists                                      GET, POST
/lists/{listId}                             GET, PATCH, DELETE
/lists/{listId}/bookmarks                   GET
/lists/{listId}/bookmarks/{bookmarkId}      PUT, DELETE
/tags                                       GET, POST
/tags/{tagId}                               GET, PATCH, DELETE
/tags/{tagId}/bookmarks                     GET
/highlights                                 GET, POST
/highlights/{highlightId}                   GET, PATCH, DELETE
/users/me                                   GET
/users/me/stats                             GET
/assets                                     POST
/assets/{assetId}                           GET
/assets/{assetId}/signed-url                GET
/admin/users/{userId}                       PUT
/admin/jobs/trigger/recrawl                 POST
/admin/jobs/trigger/reindex                 POST
/admin/jobs/trigger/inference               POST
/backups                                    GET, POST
/backups/{backupId}                         GET, DELETE
/backups/{backupId}/download                GET
/feeds                                      GET, POST
/feeds/{feedId}                             GET, PATCH, DELETE
/feeds/{feedId}/fetch                       POST
```

**Create / read / search / tag: all four are covered.** `POST /bookmarks` creates; `GET /bookmarks/{id}` reads; `GET /bookmarks/search` searches (using the full query language documented at `docs/docs/04-using-karakeep/search-query-language.md` — `is:fav`, `is:archived`, `is:link|text|media`, `is:broken`, `url:`, `title:`, tag/list qualifiers, boolean `and`/`or`, negation with `-`/`!`, parenthesised grouping); `POST|DELETE /bookmarks/{id}/tags` tags and untags.

Notably good for a downstream consumer: **`GET /bookmarks/{bookmarkId}/content` returns the article rendered as Markdown.** From the spec: `format` is an enum of `["markdown", "text"]`, defaulting to markdown, with `maxChars` (default 12000, max 50000) and an opaque `cursor` for continuation. The operation is explicitly described as returning "an agent-readable bookmark representation."

Gaps worth knowing about: **the webhook CRUD endpoints are not in the OpenAPI spec** even though `packages/api/routes/webhooks.ts` exists — webhooks are managed through the web UI / tRPC, not the documented REST API. Same for rule-engine rules (`packages/trpc/routers/rules.ts`, no REST path). Anything the UI does that isn't in the list above is tRPC-only (`packages/trpc/routers/`), which is a private, unversioned surface.

**Verdict on "complete enough to be someone else's backend": yes for bookmarks/tags/lists/highlights/assets/search/content-as-markdown; no for administering webhooks and rules programmatically.**

---

## 4. Capture surfaces shipped

| Surface | Shipped? | Evidence |
|---|---|---|
| Web UI | Yes | `apps/web` |
| REST API | Yes, documented + OpenAPI | `packages/open-api/karakeep-openapi-spec.json` |
| Browser extension (Chrome + Firefox) | Yes, first-party, v1.2.11, MV3 | `apps/browser-extension/manifest.json`; store links in `docs/docs/04-using-karakeep/quick-sharing.md` — [Chrome Web Store](https://chromewebstore.google.com/detail/karakeep/kgcjekpmcjjogibpjebkhaanilehneje), [Firefox AMO](https://addons.mozilla.org/en-US/firefox/addon/karakeep/) |
| iOS app | Yes, first-party | [App Store id6479258022](https://apps.apple.com/us/app/karakeep-app/id6479258022), `apps/mobile`, tag `ios/v1.10.0-1` |
| Android app | Yes, first-party | [Play Store `app.hoarder.hoardermobile`](https://play.google.com/store/apps/details?id=app.hoarder.hoardermobile), tag `android/v1.10.0-1` |
| CLI | Yes, first-party, npm `@karakeep/cli` 0.33.1 | `apps/cli/src/commands/` — `bookmarks`, `lists`, `tags`, `highlights`, `assets`, `admin`, `dump`, `migrate`, `wipe`, `whoami`, `auth`. Docs: `docs/docs/05-integrations/02-command-line.md` |
| MCP server | Yes, first-party, `@karakeep/mcp` 0.33.0 | `apps/mcp/`, docs `docs/docs/05-integrations/03-mcp.md`. Tools: `search-bookmarks`, `get-bookmark`, `get-bookmark-content`, `create-bookmark`, `update-bookmark`, `delete-bookmark`, plus full list and tag CRUD |
| RSS ingestion | Yes | `apps/workers/workers/feedWorker.ts`, `rssFeedsTable`, `/feeds` API, `docs/docs/05-integrations/06-rss-feeds.md` |
| SingleFile import | Yes | `docs/docs/05-integrations/05-singlefile.md`, `karakeep bookmarks import-singlefile` |
| **Telegram** | **No — nothing first-party** | Exhaustive `grep -rin telegram` across all `.ts/.tsx/.md/.mdx/.json` in the repo returns hits in exactly one file, repeated across doc versions: `docs/docs/07-community/01-community-projects.md:33-41`, which links to a **third-party** bot, [`Madh93/karakeepbot`](https://github.com/Madh93/karakeepbot), under a page-level warning: "This list comes with no guarantees about security, performance, reliability, or accuracy. Use at your own risk." |

**The Telegram answer is unambiguous: Karakeep ships zero Telegram code. A Telegram capture path means either adopting an unmaintained-by-upstream community bot or writing your own against the REST API.** The API is more than adequate for the latter (`POST /bookmarks` + `POST /bookmarks/{id}/tags`).

Other community surfaces listed on the same page (all third-party, same warning): Raycast extension, Alfred workflow, an **Obsidian plugin that syncs bookmarks into Markdown notes** ([`jhofker/obsidian-hoarder`](https://github.com/jhofker/obsidian-hoarder)), Hoarder's Pipette (search-result injection), and a Python API client.

---

## 5. AI features

### What exists

- **Auto-tagging** — `INFERENCE_ENABLE_AUTO_TAGGING`, **default `true`** (`packages/shared/config.ts`). Tags carry `attachedBy` provenance so AI tags are distinguishable from human ones (`schema.ts`, `tagsOnBookmarks`).
- **Auto-summarization** — `INFERENCE_ENABLE_AUTO_SUMMARIZATION`, **default `false`**. Also manually triggerable per bookmark via `POST /bookmarks/{bookmarkId}/summarize`.
- **Embeddings / semantic + hybrid search** — `SEMANTIC_SEARCH_ENABLED` default `true`, but gated behind `EMBEDDING_ENABLE_AUTO_INDEXING` (unset by default). Docs mark it **Experimental**. Vector store is Meilisearch.
- **Chat over your bookmarks** — `CHAT_ENABLED`, default `false`; `chatSessions`/`chatMessages` tables.
- **LLM-based OCR** — `OCR_USE_LLM`, default `false`; falls back to Tesseract.
- **Custom prompts** — `customPrompts` table, user-editable tagging/summarization prompts.

### Which provider, and can it be pointed elsewhere

Two client implementations, chosen by env var precedence (`packages/shared/inference.ts:208-219`):

```ts
static build(): InferenceClient | null {
  if (serverConfig.inference.openAIApiKey) return OpenAIInferenceClient.fromConfig();
  if (serverConfig.inference.ollamaBaseUrl) return OllamaInferenceClient.fromConfig();
  return null;
}
```

**`OPENAI_BASE_URL` points the OpenAI client at any OpenAI-compatible endpoint.** `docs/docs/03-configuration/02-different-ai-providers.md` gives worked configs for OpenAI, **Ollama** (both its `/v1` OpenAI-compatible endpoint and its native API via `OLLAMA_BASE_URL`), **Gemini**, **OpenRouter**, **Perplexity**, and **Azure OpenAI**. Embeddings can be pointed at a *different* endpoint again via `EMBEDDING_OPENAI_BASE_URL` / `EMBEDDING_OPENAI_API_KEY`. There's also `OPENAI_PROXY_URL`, `INFERENCE_OUTPUT_SCHEMA` (`structured|json|plain`, for models without structured output), `INFERENCE_LANG`, and `INFERENCE_CONTEXT_LENGTH`.

### Can it be turned off

**Fully, in three independent ways:**

1. **Configure nothing.** `InferenceClientFactory.build()` returns `null` when neither `OPENAI_API_KEY` nor `OLLAMA_BASE_URL` is set. Docs confirm: "Either `OPENAI_API_KEY` or `OLLAMA_BASE_URL` need to be set for automatic tagging to be enabled. Otherwise, automatic tagging will be skipped" (`docs/docs/03-configuration/01-environment-variables.md:87`). No key is required to run Karakeep.
2. **Feature flags.** `INFERENCE_ENABLE_AUTO_TAGGING=false`, `INFERENCE_ENABLE_AUTO_SUMMARIZATION=false`, `SEMANTIC_SEARCH_ENABLED=false`, `CHAT_ENABLED=false`.
3. **Kill the worker.** `WORKERS_DISABLED_WORKERS` can drop the inference and embedding workers entirely.

**No AI call is ever made without an explicitly configured provider.** There is no phone-home default. This is the cleanest part of the design.

---

## 6. Export

Three distinct export paths, with materially different fidelity. This is where the "markdown obligation" question gets an awkward answer.

### (a) Web UI export — `GET /api/bookmarks/export?format=json|netscape`

`apps/web/app/api/bookmarks/export/route.tsx`. Two formats:

- **`json`** — shape defined by `zExportSchema` in `packages/shared/import-export/exporters.ts:8-42`. Per bookmark: `createdAt`, `title`, `tags[]`, `lists[]`, `content` (a discriminated union of **only** `{type:"link", url}` or `{type:"text", text}`), `note`, `archived`. Plus a `lists[]` array.
- **`netscape`** — the standard `<!DOCTYPE NETSCAPE-Bookmark-file-1>` HTML that every browser imports (`toNetscapeFormat`, `exporters.ts:97+`).

**What the JSON export loses, per the source:** `toExportFormat` has a bare comment `// Exclude asset types for now` (`exporters.ts:64`) — image and PDF bookmarks produce `content: null` and are then **filtered out entirely** by `.filter((b) => b.content !== null)` in the route (`route.tsx:88`). It also drops: the crawled article body/`htmlContent`, AI `summary`, highlights, reading progress, all binary assets, rules, feeds, webhooks. It is a link-and-note list, not an archive.

### (b) Scheduled backups — zip of the same lossy JSON

`apps/workers/workers/backupWorker.ts`, exposed at `/backups` in the REST API, with daily/weekly frequency and a retention policy. It calls the **same** `toExportFormat`/`toExportListFormat` (`backupWorker.ts:330-390`), streams it to one JSON file, and `archiver`-zips **only that single file** (`createZipArchiveFromFile`, `:418-445` — `archive.file(jsonFilePath, ...)`, nothing else added). So the "backup" feature has exactly the fidelity of the JSON export: **no assets, no article content, no highlights.** Do not mistake it for a disaster-recovery backup.

### (c) `karakeep dump` — the actual archive

`apps/cli/src/commands/dump.ts`. Produces a `.tar.gz` with `format: "karakeep.dump", version: 1` manifest and separate trees for user settings, lists, tags, bookmarks, **binary assets**, AI prompts, rule-engine rules, RSS feeds, and webhooks — each independently excludable (`--exclude-assets`, `--exclude-bookmarks`, `--exclude-lists`, `--exclude-tags`, `--exclude-ai-prompts`, `--exclude-rules`, `--exclude-feeds`, `--exclude-webhooks`, `--exclude-user-settings`, `--exclude-link-content`). **This is the only clean, complete archive Karakeep produces, and it is CLI-only.**

### Per-item Markdown

**Not as a bulk export — but yes, per item, over the API.**

- `GET /bookmarks/{bookmarkId}/content?format=markdown` returns the readable article as Markdown, paginated by `maxChars`/`cursor` (see §3).
- `karakeep bookmarks content --format markdown` does the same from the CLI (`apps/cli/src/commands/bookmarks.ts:412-438`, which validates `format must be one of: markdown, text`).

So emitting a per-item Markdown corpus is a **short script over a documented endpoint**, not a feature you get from a button. The third-party Obsidian plugin exists precisely because upstream doesn't ship this.

---

## 7. Extension points

| Mechanism | Present? | Detail |
|---|---|---|
| **Webhooks** | **Yes, real** | `webhooksTable` (`schema.ts:722-741`) with per-user URL + event list + secret. Dedicated worker: `apps/workers/workers/webhookWorker.ts`. Events (`packages/shared/types/webhooks.ts:7-11`): **`created`, `edited`, `crawled`, `ai tagged`, `deleted`**. Configurable delivery: `WEBHOOK_TIMEOUT_SEC` (5), `WEBHOOK_RETRY_TIMES` (3), `WEBHOOK_NUM_WORKERS` (1), `MAX_WEBHOOKS_PER_USER` (100). Deletion events deliver even when the bookmark row is gone (`canDeliverWebhookWithoutBookmark`). Managed via UI/tRPC, **not** the documented REST API. |
| **Rule engine (if-this-then-that)** | Yes | `ruleEngineRulesTable`/`ruleEngineActionsTable`, `apps/workers/workers/ruleEngineWorker.ts`, schema in `packages/shared/types/rules.ts`. **Events:** `bookmarkAdded`, `tagAdded`, `tagRemoved`, `addedToList`, `removedFromList`, `favourited`, `archived`. **Conditions:** `alwaysTrue`, `urlContains`/`urlDoesNotContain`, `titleContains`/`titleDoesNotContain`, `importedFromFeed`, `bookmarkTypeIs`, `bookmarkSourceIs`, `hasTag`, `isFavourited`, `isArchived`, composable with `and`/`or`. **Actions:** `addTag`, `removeTag`, `addToList`, `removeFromList`, `downloadFullPageArchive`, `favouriteBookmark`, `archiveBookmark`. Documented at `docs/docs/04-using-karakeep/advanced-workflows.md`. |
| **MCP server** | Yes | See §4 — an LLM-facing extension surface. |
| **"Plugins"** | **Not a user extension point** | `packages/plugins/` exists but contains six *internal, compile-time* providers: `queue-liteque`, `queue-restate`, `search-meilisearch`, `vectorstore-meilisearch`, `ratelimit-memory`, `ratelimit-redis`. They're registered by a hardcoded import list in `packages/shared-server/src/plugins.ts` (`loadAllPlugins()`). There is **no runtime plugin loading, no plugin directory, no third-party plugin API.** It's an internal abstraction for swapping infrastructure backends, nothing more. |
| **Hooks / scripts** | No | Nothing. |
| **Prometheus metrics** | Yes | `PROMETHEUS_AUTH_TOKEN`, `packages/api/routes/metrics.ts`, `apps/workers/metrics.ts`. |

Combined: **webhooks + rule engine + REST API + MCP is a genuinely respectable automation surface** — better than most self-hosted bookmarkers. The `created` and `crawled` webhook events in particular mean an external enricher can be wired in without polling.

---

## 8. Is this bloated for one user?

### Evidence that it is

1. **Three containers minimum for the advertised experience, and two of them are heavyweights.** A headless Chrome and a Meilisearch instance are running 24/7 to serve a workload that, for one person, is a handful of saves per day. Chrome is the single largest resident-memory item in the stack and it idles at cost.
2. **The default image is fat by construction.** ffmpeg, ghostscript, graphicsmagick, yt-dlp, Tesseract, and a Rust-compiled `monolith` binary are all baked in (`docker/Dockerfile`) regardless of whether you ever save a video or a PDF.
3. **Search is all-or-nothing.** There is no SQLite FTS fallback. Drop Meilisearch and you lose search *completely* (`docs/docs/02-installation/07-minimal-install.md`). For a personal archive, search is the point — so the "minimal install" isn't really an option, which means Meilisearch isn't really optional.
4. **You are carrying a multi-tenant SaaS.** `subscriptions`, `STRIPE_*`, `FREE_QUOTA_*`/`PAID_QUOTA_*`, `invites`, `listCollaborators`, `listInvitations`, `DEMO_MODE`, Turnstile, SMTP, OAuth/OIDC — all in the schema and config of every single-user install. 35 tables and 94 migrations for what one person needs.
5. **11 background workers, a job queue, a rule engine, a chat feature, an embeddings pipeline, and an import-session state machine** for a workload measured in tens of items a day.
6. **The docs publish no resource requirements at all.** You cannot size this before installing it; you have to run it and measure.
7. **Upgrades occasionally demand hand-editing your compose file** (v0.16 container consolidation, v0.33.2 Chrome image) and couple you to Meilisearch's own upgrade path (v0.32.0, v0.33.1).
8. **The out-of-the-box export is lossy in a way that matters.** Image and PDF bookmarks are silently dropped from the JSON export and from the scheduled "backup" (`// Exclude asset types for now`). The complete archive is CLI-only.

### Evidence that it is not

1. **The default *is* one process.** Web + API + all 11 workers live in a single container; the queue is a SQLite file. The worker sprawl is architectural, not operational — you run one image, not eleven.
2. **The heavy parts are genuinely optional and cleanly gated.** `docs/docs/02-installation/07-minimal-install.md` is an officially supported single-container `docker run` with no Chrome, no Meilisearch, no AI. Every expensive feature is off by default: video download `false`, full-page archive `false`, PDF capture `false`, summarization `false`, chat `false`, rate limiting `false`, auto-embedding-indexing unset.
3. **No AI unless you ask for it, and it's provider-agnostic.** `InferenceClientFactory.build()` returns `null` with no key. When you do want it, `OPENAI_BASE_URL` points at Ollama/Gemini/OpenRouter/Perplexity/Azure/anything OpenAI-compatible, with embeddings independently targetable. Zero lock-in, zero phone-home.
4. **They have actively removed weight over time.** v0.16 deleted the Redis container *and* the separate workers container. The trend line is toward fewer moving parts, not more.
5. **Migrations are automatic and gated on startup.** 94 migrations is a number you never have to think about; s6 blocks the services until they've applied.
6. **The API is real, specified, and complete enough to be a backend.** OpenAPI 3.0, bearer auth, full CRUD on bookmarks/tags/lists/highlights/assets, a proper search query language, and — the sleeper feature — `GET /bookmarks/{id}/content?format=markdown` designed explicitly for agent consumption.
7. **Extension points exist and are not toys.** Five webhook events with retries, plus a rule engine with 11 conditions and 7 actions. You can bolt on external enrichment without forking anything.
8. **Every capture surface you'd want is first-party and maintained** — Chrome + Firefox extensions, iOS + Android apps, CLI, MCP server, RSS. Building even one of these yourself is weeks.

### Verdict

**Karakeep is not bloated as software; it is over-provisioned as a deployment, and it is only over-provisioned in the parts you can turn off.**

The honest shape of the trade is this. The *code* carries obvious SaaS baggage — Stripe, quotas, collaborators, invites — but that baggage is inert: it costs you schema you never read, not RAM you can't reclaim. The *deployment* is where the weight is real, and it concentrates in exactly two containers: Chrome and Meilisearch. Chrome you can genuinely drop if you mostly save static articles (crawling still works, you just lose screenshots and JS-rendered pages). Meilisearch you effectively cannot, because dropping it deletes search entirely, and a bookmark archive you can't search is a folder.

So for one user the practical floor is **two containers** — app + Meilisearch — not three, and not one. Whether that's "bloated" depends on what you're comparing it to: against a Telegram bot writing Markdown files, it's enormous; against building a crawler, a reader-view extractor, a search index, a browser extension, two mobile apps, and an OpenAPI-specified backend yourself, it is very cheap.

**Two caveats that should weigh heavily on an adopt decision.** First, **Telegram is not a shipped surface** — there is precisely zero Telegram code in the repository, and the only option upstream offers is a third-party bot behind an explicit no-guarantees warning. If Telegram capture is load-bearing, that's yours to build (the REST API makes it easy, but it's still yours to build and maintain). Second, **the exit path is narrower than it looks**: the export button and the scheduled backup both silently drop image and PDF bookmarks and all article content, and the only faithful archive comes from the CLI's `dump` command. If per-item Markdown is an obligation, that's also a script you write against `/bookmarks/{id}/content?format=markdown` — a well-designed endpoint, but not a feature.

Neither caveat is disqualifying. Both are work that adopting Karakeep does *not* save you.
