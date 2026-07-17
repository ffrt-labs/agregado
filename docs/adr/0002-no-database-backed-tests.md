# ADR 0002 — No database-backed tests; persistence is verified live

**Status:** Accepted
**Date:** 2026-07-17 (documenting a practice in place since Phase 1)

## Context

Every test in this repo is either a pure function test or uses a hand-written
fake. There is no testcontainers, no live Postgres in CI, no `sql.Open` in a
test. `article_repo_test.go` — the test file named after the repository layer —
covers only `sortClause`, a pure string function.

Instead, every phase since 15 has been **live-verified** against the real local
stack: real RSS redirects, real backfills, real `/admin/logs` rows, real feedback
votes producing real `topic_weights`.

This was never written down. It was noticed while planning Phase 20, when a spec
nearly proposed tests the project has no infrastructure to run.

## Decision

**Do not introduce database-backed tests. Verify persistence live instead.**

Tests cover logic at the highest available seam using fakes:

- **Pure functions** — `textutil`, `domain`, `sortClause`, `resolveSender`.
  Table-driven.
- **HTTP handlers** — `httptest` + hand-written fakes (`fakeArticleRepo` in
  `internal/api/articles_test.go` is the reference implementation; it satisfies
  several interfaces at once so one fake backs both the handler and its
  collaborators).
- **Ingestion** — `Parse(payload)` with header maps and HTML strings in,
  assertions on the returned entity out.
- **Anything needing a network call** — an interface with a fake substituted
  (`storage.Fetcher` exists for exactly this reason).

Persistence, migrations, and constraint behavior are verified by exercising the
real stack and observing the database.

## Consequences

**Good:**

- Tests run in milliseconds with no Docker, no fixtures, no teardown, no
  flake.
- Forces logic to be extractable from I/O to be testable at all — which is why
  `resolveContent`, `Distill`, and `resolveSender` are pure and why `Fetcher` is
  an interface.
- Live verification catches an entire class of defect that DB tests would miss.
  Phases 16–18 were all bugs where every unit was individually correct and the
  *wiring* was broken. A repo test asserting `SetTags` writes a row would have
  passed throughout the period when `article_tags` had zero rows in production,
  because nothing called it.

**Bad / accepted:**

- **Persistence correctness is only ever proven manually.** A regression in a SQL
  query is caught by a human looking, or not at all.
- Live verification is not reproducible in CI and leaves real rows behind (see
  the Phase 17/18 housekeeping notes — a backfill and a real feedback vote were
  kept rather than reverted, as genuine proof the mechanism works).
- `SELECT *` at six call sites in the article repo is untested and has already
  caused one silent bug (a bare `SELECT *` plus a `db:"-"` tag meant `GetById`
  never loaded tags — Phase 18).
- Schema/migration changes carry more risk than they would with a test harness,
  which is why migration-touching work (e.g. the Phase 21 spec) specifies
  explicit live-verification steps including the down migration.

## Alternatives considered

- **testcontainers-go.** Rejected for now: introduces Docker into the test loop
  and an entire testing infrastructure, for a single-user project where the
  highest-value verification (does the whole pipeline actually work end to end?)
  is already being done live and is the thing that has actually caught bugs.
- **A shared test database.** Rejected: shared mutable state across tests, and it
  would not have caught any of the Phases 16–18 defects.

## Notes

The tradeoff worth being honest about: this project's bugs have overwhelmingly
been in **config and wiring — the one part of the system with no test surface at
all**. Four of the five defects found in Phases 16–19 lived in `envDefault`
values, deploy workflow env blocks, compose service definitions, and tunnel
ingress rules. None of those are reachable from a Go test with or without a
database.

So the argument for this ADR is not "DB tests have no value." It is that DB tests
would address the area where this project has had the fewest problems, at
meaningful cost, while the actual problem area stays structurally untestable and
needs compensating controls instead (see ADR 0001 and the Phase 19 banner guard).

**Revisit if** the project ever gains a second developer, CI-gated merges, or a
schema complex enough that live verification stops being tractable.
