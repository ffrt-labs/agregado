# ADR 0005 — Enrichment is a library, and scoring is not in it

**Status:** Accepted
**Date:** 2026-08-23
**Issue:** [#57](https://github.com/ffrt-labs/agregado/issues/57)

## Context

The pivot ([#48](https://github.com/ffrt-labs/agregado/issues/48)) leaves two
tools that both want AI-derived data about content: the Reader enriches
Articles, the Bookmarker enriches Bookmarks. ADR-scale question: do they share
an enrichment component, or does each call an LLM in twenty lines and stay
dumber?

The pull toward a shared component is the same reasoning that produced the
over-engineering this pivot exists to escape, so it was charted as a question
rather than assumed.

Two facts constrain the answer:

- **The interfaces are not the same shape.** The Reader wants
  extract → summarize → tag → **score**. The Bookmarker wants
  extract → summarize → tag, and nothing else. A Bookmark is a survivor: it
  cleared the filter as an Article, or it was Saved deliberately by hand. Either
  way it has already passed the judgement scoring exists to make. The
  Bookmarker's purpose is summarizing, tagging and organising what is kept — not
  deciding whether to keep it.
- **Enrichment belongs to the tool that owns the store** (CONTEXT.md, settled by
  [#55](https://github.com/ffrt-labs/agregado/issues/55)). No Enrichment crosses
  between the tools. Whatever is shared is shared as *code*, never as data.

## Decision

**A Go library. Not a service, not a CLI, and it does not score.**

1. **Form: a library**, in the same language as its callers (Go). No new
   container, no network hop, no process spawn. The tools stay separate
   deployables that happen not to reinvent the same hundred lines.

2. **Surface: fetch → extract → summarize → tag**, plus the task-tiered model
   routing table from [#51](https://github.com/ffrt-labs/agregado/issues/51)
   (each task routed to the cheapest model that can do it). Extraction needs no
   LLM at all. This surface is opinion-free: it is the part that is genuinely
   duplicated and genuinely uninteresting.

3. **Scoring stays in Agregado**, outside the library, and `PREFERENCES.md` is
   an explicit runtime input to it. Google Drive holds the authoritative personal
   document; n8n validates and synchronizes it to the absolute `PREFERENCES_PATH`
   cache on the homelab, which Agregado reads. The profile is personal editable data,
   not application source. The library never loads, parses, or knows about the
   preference profile.

4. **Conditional on [#56](https://github.com/ffrt-labs/agregado/issues/56).**
   The library is *shared* only if the Bookmarker is built in Go. If Karakeep is
   adopted, the library has exactly one consumer and stays an Agregado-internal
   package — good code organisation, not an architectural seam. In that case
   Karakeep's own AI is expected to be **turned off** rather than wired to this
   routing table: no Enrichment crosses the seam anyway, and Karakeep's tagging
   would be a second, unrelated opinion about the same content.

5. **Divergence policy: copy out, do not extend.** The library has no
   configuration hooks beyond the routing table. When one caller needs something
   the other does not, that caller keeps its own copy of the part it needed. The
   library is allowed to shrink, or to die. **Duplication is cheaper than the
   wrong abstraction.**

## Rationale / alternatives rejected

- **A sidecar HTTP service (rejected).** The cleanest seam on paper, and the
  most expensive one here: a new container to run, monitor and keep alive, on a
  single box serving a single user, to wrap a handful of pure functions and some
  API calls to Cloudflare Workers AI. This is precisely the operational weight
  the map interrogates third-party tools for — self-inflicted.

- **A CLI both tools shell out to (rejected).** Language-agnostic and easy to
  test standalone, but it buys independence from a problem that does not exist
  (both tools are Go, by construction — see decision 4) and pays for it with
  process-spawn overhead and awkward batching. #51's shape needs one batched
  frontier call per day; a CLI is the wrong grain for that.

- **Scoring inside the library (rejected).** The test the seam must pass is that
  the core knows nothing about Articles or Bookmarks. Scoring fails it. Relevance
  is personal fit against a profile that learns from reading behaviour — the one
  place the map's yardstick says the opinions are the dev's own. A library that
  holds `PREFERENCES.md` is not a generic enrichment utility; it is Agregado
  wearing a coat of paint, and the Bookmarker importing it would import
  Agregado's entire reason to exist.

- **Designing the library for two consumers now (rejected).** Speculative
  generality for a second consumer that #56 may never produce. Decision 4 makes
  the library a *consequence* of the Bookmarker decision rather than a constraint
  on it.

## Consequences

- **`PREFERENCES.md` has an unambiguous consumer: Agregado.** Its storage location
  is intentionally independent of the application checkout. This is an input to
  [#59](https://github.com/ffrt-labs/agregado/issues/59), not a decision it gets
  to revisit.
- **The Bookmarker never scores.** If ranking the Pile ever becomes a want, it is
  a new decision against the "no affordance that needs grooming" through-line,
  not an extension of this one.
- **Where the library physically lives** — its own Go module, or a package
  Agregado exports — is deliberately left to
  [#62](https://github.com/ffrt-labs/agregado/issues/62).
- The library is small enough that if it never gains a second consumer, nothing
  was lost: it is the code Agregado would have written anyway.
