# ADR 0010: The preference profile lives in Agregado's database

**Status:** Accepted

**Date:** 2026-10-29

**Issue:** [#191](https://github.com/ffrt-labs/agregado/issues/191)

**Amends:** ADR-0005 (point 3), which had Agregado read `PREFERENCES.md` from a
`PREFERENCES_PATH` file kept current by n8n.

## Context

ADR-0005 left `PREFERENCES.md` as a file on the homelab host, bind-mounted into
the container and written by n8n from the authoritative Google Drive copy. In
practice that file was owned by root, so every write needed sudo. n8n would also
have had to reach the host filesystem, which assumes it runs on the same host as
Agregado. A personal-data file also drifted outside both the database backup and
any repository, with nothing reporting a bad write. Meanwhile, nothing ever
consumed the reader's votes (#81 shipped only the signal export).

## Decision

**Agregado stores the accepted profile in Postgres and receives it over its
private API. No file or mount is involved.**

- The **Google Doc `PREFERENCES`** remains the authoritative copy. The reader
  edits it, including from the phone, and its version history is the rollback.
- `preference_profiles` keeps every accepted version. The newest row is the
  profile in force. A version identical to the newest one is not stored again.
- `PUT /api/private/preferences` takes the Doc's Markdown export, validates it
  and stores it. `GET` returns it. Both use `X-Enrichment-Secret`.
- `POST /api/private/preferences/regenerate` rewrites the profile from the
  Reader's evidence: current votes as strong evidence, first Opens as weak
  evidence. It uses one model call inside Agregado, which already holds both the
  evidence and the model. It validates the output, stores it and returns it.
  With no evidence it changes nothing. Output that fails validation is rejected,
  and the accepted profile stays in place.
- n8n runs nightly. It exports the Doc, `PUT`s it, then calls `regenerate`. When
  the profile changed, it writes the result back to the Doc and reports what
  changed. Agregado never talks to Google.
- **Shape:** an H1 `PREFERENCES`, an optional intro paragraph, and exactly four
  H2 sections holding only bullet points: *Topics I want*, *Topics I skip*,
  *Sources I trust*, *Kinds of articles I skip*. The size limit is 8 KiB.
  Bullets starting with `(example:` are placeholders and never reach a model.
- The regeneration replaces the profile automatically after validation. There
  is no human-review gate, which supersedes #81's review requirement.

## Consequences

`PREFERENCES_PATH`, its bind mount and the host directory go away. Before an
Agregado image built from this change runs Enrichment, the profile must be
handed over once (the n8n workflow's first run, or a manual `PUT`). Until then,
Enrichment fails with "no preference profile has been stored yet", exactly as a
missing file did before.

`GET /api/private/preferences/signals` stays available for inspection, but the
regeneration no longer depends on it.
