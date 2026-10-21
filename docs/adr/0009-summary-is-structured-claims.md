# ADR 0009 — The Summary is structured claims on the cheap model

**Status:** Accepted
**Date:** 2026-10-19 (amended same day: lead extract → structured)
**Issue:** [#86](https://github.com/ffrt-labs/agregado/issues/86)

## Context

A **Summary** is machine input: it stands in for an Article's content when a
model judges it (Digest selection, `PREFERENCES.md` regeneration). It was never
shown to the Reader when this was written; since #183 the Digest shows up to two
claims per item (CONTEXT.md → Summary), so how it reads can now be judged too.
It is still judged mainly downstream, by whether the Digests built from it are good.

Production ran `OpSummarize` on every candidate: a model call over a 400-character
excerpt. #86 asked whether that spend buys anything.

## Bake-off

The same frozen candidate set went through the real `OpDigestSelect` prompt once
per arm, with the selection model held fixed (`@cf/openai/gpt-oss-120b`). The
Reader ranked the resulting Digests blind. Corpus: 2026-10-01 and 2026-10-02
(10 candidates each; 2 partial on the 2nd). Prototype: branch
`prototype/summary-bakeoff` (never merged).

| Arm | Rank 10-01 | Rank 10-02 | Clearly worse? | Neurons/day (% of 10k free) |
|---|---|---|---|---|
| lead (title + first 150 words, no model) | 2 | 3 | never | 168 / 173 (1.7%) |
| current (`OpSummarize`, 400-char excerpt) | 4 | 4 | both days | 233 / 270 (2.3–2.7%) |
| structured (claims/entities/kind, cheap model) | 1 | 2 | never | 516 / 528 (5.2%) |
| long (first 800 words, no Summary model) | 3 | 1 | never | 418 / 518 (4.2–5.2%) |

"Clearly worse" = misses an Article the Reader wanted, or a "why" is wrong or
misleading. The current arm was flagged on 10-01 (superficial summaries, as if
the article was not opened) and on 10-02 (missed an Article rated highly important).

## Decision

**Structured claims on the cheap model.** The Reader chose this over the
pre-set rule's winner.

The rule set before the run was "the cheapest arm that is never clearly worse;
ties go to the lead extract". By that rule the lead extract wins: it was never
flagged and costs about 1.7% of the free daily allowance. **The Reader overrode
it.** Structured had the best average rank (1.5 vs 2.5 for lead) and was the
clearest winner in the Reader's judgement. Cost is not a constraint: Agregado
is the only consumer of the Workers AI allowance, and structured uses about 5.2%
of it. The rule is kept as the record of what the test would have said.

1. **Job:** machine input for Digest selection and `PREFERENCES.md` regeneration.
2. **Input / granularity:** per Article; the title plus the Article content,
   cleaned and capped at `AI_MAX_CONTENT_CHARS` (8000 in the bake-off).
3. **Output contract:** JSON `{kind, claims, entities}`: `kind` is one of
   news / analysis / tutorial / opinion / announcement / newsletter / other;
   `claims` is the main claim or finding, one sentence each, at most 3;
   `entities` is people, companies, products and places, at most 6. The prompt
   asks for factual output only and for a stub or truncated Article to be said so
   in `claims`. Prototype prompt: `structuredPrompt` in
   `cmd/summary-bakeoff-prototype/main.go` on branch `prototype/summary-bakeoff`.
4. **Fallback:** when content cannot be fetched, use Miniflux's feed content,
   marked `[content: partial]`. If the model call fails or returns invalid JSON,
   fall back to the lead extract (title + first 150 words) so selection is never
   starved of input.
5. **Model tier:** the cheap tier (`@cf/google/gemma-4-26b-a4b-it` in the
   bake-off). Selection stays on its own model.
6. **Non-Digest Articles:** need no Summary.

## Consequences

- Roughly 3x the Neurons of the lead extract (about 516–528 a day for 10
  candidates, 5.2% of the free 10k). Still well inside the allowance.
- The current `OpSummarize` arm is dropped from the selection path: it ranked
  last and was flagged clearly worse on both days.
- The lead-extract fallback in point 4 is a design addition, not something the
  bake-off tested.
- Two days is a small sample. Revisit if a Digest misses a wanted Article or
  the extra cost stops being irrelevant. `long` is the other arm worth trying.
- `PREFERENCES.md` regeneration inherits this choice; it was not tested separately.
- Whether `OpSummarize` survives for the 2–3 sentence Digest-level overview is a
  separate concern and is untouched here.
- Implementation is a follow-up issue; the prototype branch is not merged.
