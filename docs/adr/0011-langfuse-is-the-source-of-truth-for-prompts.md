# ADR 0011: Langfuse is the source of truth for prompts, traces and scores

**Status:** Accepted

**Date:** 2026-11-05

**Issue:** [#201](https://github.com/ffrt-labs/agregado/issues/201) (spec [#198](https://github.com/ffrt-labs/agregado/issues/198))

**Amends:** the `ai_prompts` table and admin prompt editor (migration 000011) as
the place prompts are changed.

## Context

Agregado's model prompts lived in Go (`ai.DefaultPrompts`), with an override row
per operation in `ai_prompts` that the admin page edits. A changed prompt had no
version and no history. Nothing tied a Score to the prompt that produced it, so
comparing two prompts on real Articles was impossible. Spec #198 brings in a
self-hosted Langfuse for tracing, prompt management and evals.

## Decision

**Langfuse is the source of truth for the prompts it manages, and the
destination for traces and scores. It is optional at runtime and never on the
failure path.**

- A managed prompt is a Langfuse **chat** prompt named `agregado/<operation>`:
  one system and one user message with `{{name}}` variables. Agregado serves
  the version at label `production`. Promoting a version, or rolling one back,
  means moving that label. There is no deploy.
- Agregado caches each prompt in memory for `LANGFUSE_PROMPT_CACHE_TTL`
  (default 60s) and refreshes it in the background, serving the stale copy in
  the meantime. A failed fetch keeps the last good copy. A model call never
  waits on a fetch: with nothing cached yet it uses the in-code default while
  one background fetch runs, and startup warms every managed prompt.
- Each operation keeps an **in-code default** (version 0): today's system
  prompt (the `ai_prompts` row if present, otherwise `ai.DefaultPrompts`) and
  today's Go-built user text as a template. The default is used when Langfuse
  is unconfigured, has never answered, or serves a version that fails to
  compile. A version that references an undefined variable, or leaves one
  unused, fails to compile and is logged as a warning.
- Every generation compiled from a managed version is linked to its name and
  version, and records its variables as input, so a trace can become a dataset
  item. The compiled messages go in the generation's `messages` metadata.
- Version 1 is seeded from the prompt in force by `cmd/seed-langfuse-prompts`.
  It never overwrites a prompt that Langfuse already has.
- Migration happens one operation at a time. `preference_score` is first
  (#201); `article_summary`, `categorize` and `digest_select` follow (#203).
  The legacy operations (score, summarize, digest, reason) are not migrated
  and keep their current path. Categorize's live tag slugs are the
  `{{allowed_slugs}}` variable; its default template appends
  ` Allowed slugs: {{allowed_slugs}}.` to the `ai_prompts` row, as Go did.

## Consequences

- Langfuse being down, slow or misconfigured never fails an enrichment or a
  Digest, and with it unconfigured Agregado behaves exactly as before. The cost
  is that a bad production label degrades quietly to the default, so the
  warning log is the alarm.
- Once an operation is seeded, edits in the admin prompt editor change only the
  default. Retiring the editor and `ai_prompts` is #207.
- Prompts are authored as data in Langfuse. The variables each operation
  supplies are a contract in code (`ai.Prompt.Compile` rejects mismatches), and
  the eval harness compiles dataset inputs against a candidate version with the
  same variables.

## Deviation from the spec

Spec #198 said to widen `ai.PromptStore` to serve templates. The code adds a
separate `ai.TemplateStore` (implemented by `internal/langfuse`) instead, and
deliberately so: `PromptStore` is the `ai_prompts` row lookup (a system prompt
string per operation), which the legacy operations and the in-code defaults
still read, while a `TemplateStore` returns a versioned system-and-user
`ai.Prompt`. Widening one interface would have made the Postgres repo
implement Langfuse's shape, or Langfuse the row lookup. `PromptStore` stays
for the legacy operations until #207 removes it.

## Update: retirement (#207)

After two clean weeks of tracing, migration 000024 dropped `ai_logs` and
`ai_prompts` and deleted the `ai_logging_enabled` setting. The admin log view,
the admin prompt editor, the prompt repository and `ai.PromptStore` /
`ai.AILogSink` are gone, with their routes. Every operation now takes its prompt
from Langfuse, or from `ai.DefaultPrompts` / `ai.DefaultTemplate`; legacy
operations use the in-code defaults only, and `cmd/seed-langfuse-prompts` seeds
from them. Mentions above of the `ai_prompts` row and the editor describe the
state before this change. The `ai: usage` log line stays, so
homelab-observability can alert on the neuron budget when Langfuse is down. The
down migration recreates both tables empty.
