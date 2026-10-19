# ADR 0009 — The Summary is a lead extract, not a model call

**Status:** Accepted
**Date:** 2026-10-19
**Issue:** [#86](https://github.com/ffrt-labs/agregado/issues/86)

## Context

A **Summary** is machine input: it stands in for an Article's content when a
model judges it (Digest selection, `PREFERENCES.md` regeneration). It is never
shown to the Reader (CONTEXT.md → Summary). It is therefore judged downstream,
by whether the Digests built from it are good, not by how it reads.

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

**Winning rule (set before the run): the cheapest arm that is never clearly worse;
ties go to the lead extract.** That is the lead extract.

1. **Job:** machine input for Digest selection and `PREFERENCES.md` regeneration.
2. **Input / granularity:** per Article; the title plus the first 150 words of its
   content.
3. **Output contract:** plain extracted text. No model call, no generated claims.
4. **Fallback:** when content cannot be fetched, use Miniflux's feed content,
   marked `[content: partial]`.
5. **Model tier:** none. `OpSummarize` is removed from the per-Article path.
6. **Non-Digest Articles:** need no Summary.

## Consequences

- Removes the per-candidate Summary call, which was both the most expensive part
  of the control arm and the arm Reader found clearly worse.
- Cheapest of the four at about 1.7% of the free daily allowance.
- **Not a quality claim.** Structured ranked best on average (1.5 vs 2.5) at about
  3x the Neurons. The rule trades that margin for cost because the margin was
  never large enough to be "clearly worse". Two days is a small sample; revisit if
  a Digest misses a wanted Article that a longer input would have caught
  (`long` and `structured` are the first things to try).
- `PREFERENCES.md` regeneration inherits this choice; it was not tested separately.
- Whether `OpSummarize` survives for the 2–3 sentence Digest-level summary is a
  separate concern and is untouched here.
- Implementation is a follow-up issue; the prototype branch is not merged.
