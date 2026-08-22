# Task-tiered LLM routing: what needs a frontier model, what doesn't, and what it costs

Research for issue [#51](https://github.com/ffrt-labs/agregado/issues/51).
Researched 2026-08-22. **Every price in this document was checked on 2026-08-22** and should be
re-checked before it is quoted anywhere load-bearing.

The settled position is that routing is *per task*, not per surface. This document establishes the
evidence for each task in the enrichment pipeline and ends with a proposed routing table.

---

## 0. What the pipeline actually does today

Read from the repo at commit time of writing:

| Step | Code | Model today |
|---|---|---|
| Content extraction | `internal/ingestion/fetch/fetch.go` — `codeberg.org/readeck/go-readability/v2` | none (heuristic) |
| Relevance score (1–5) | `internal/ai/cloudflare.go` `Score`, called per article in `internal/storage/enrich.go` | Workers AI |
| Reason ("why this matters", ≤20 words) | `Reason` | Workers AI |
| Tagging / categorization | `Categorize`, one slug from the live tag list | Workers AI |
| Per-group summary | `Summarize` (`internal/digest/generator.go`) | Workers AI |
| Digest intro | `Digest` | Workers AI |

Default model: `@cf/google/gemma-4-26b-a4b-it` (`internal/config/config.go`, `AI_MODEL`).
Article body fed to the model is capped at `AI_MAX_CONTENT_CHARS=8000` (~2,000 tokens).
The preference signal is a `topic_weights` table, not a free-text profile
(`docs/ai-relevance-feedback-plan.md`).

So the pipeline is *already* all-cheap-tier. The question this document answers is where that is
correct and where it is quietly costing quality.

---

## 1. Price references (all checked 2026-08-22)

### Cloudflare Workers AI

Source: [Workers AI pricing](https://developers.cloudflare.com/workers-ai/platform/pricing/).

- Free allocation: **10,000 Neurons per day at no charge**; usage above that is **$0.011 per 1,000 Neurons**.
- Relevant per-model rates (USD per 1M tokens, input/output — and the Neuron equivalents):

| Model | $/M in | $/M out | Neurons/M in | Neurons/M out |
|---|---|---|---|---|
| `@cf/google/gemma-4-26b-a4b-it` (current default) | $0.100 | $0.300 | 9,091 | 27,273 |
| `@cf/ibm-granite/granite-4.0-h-micro` | $0.017 | $0.112 | 1,542 | 10,158 |
| `@cf/meta/llama-3.2-3b-instruct` | $0.051 | $0.335 | 4,625 | 30,475 |
| `@cf/qwen/qwen3-30b-a3b-fp8` | $0.051 | $0.335 | 4,625 | 30,475 |
| `@cf/meta/llama-3.1-8b-instruct-fp8-fast` | $0.045 | $0.384 | 4,119 | 34,868 |
| `@cf/openai/gpt-oss-120b` | $0.350 | $0.750 | 31,818 | 68,182 |
| `@cf/meta/llama-3.3-70b-instruct-fp8-fast` | $0.293 | $2.253 | 26,668 | 204,805 |
| `@cf/mistralai/mistral-small-3.1-24b-instruct` | $0.351 | $0.555 | 31,876 | 50,488 |

`gemma-4-26b-a4b-it` is documented with a **256K context window**, **function calling: yes**, and
`response_format` support
([model page](https://developers.cloudflare.com/workers-ai/models/gemma-4-26b-a4b-it/)).

### Claude

Source: [Claude pricing](https://platform.claude.com/docs/en/about-claude/pricing), USD per 1M tokens.

| Model | Input | Output | Batch input | Batch output | Cache hit |
|---|---|---|---|---|---|
| Claude Haiku 4.5 (`claude-haiku-4-5`) | $1 | $5 | $0.50 | $2.50 | $0.10 |
| Claude Sonnet 5 (`claude-sonnet-5`) | $2 | $10 | $1 | $5 | $0.20 |
| Claude Opus 5 (`claude-opus-5`) | $5 | $25 | $2.50 | $12.50 | $0.50 |
| Claude Fable 5 (`claude-fable-5`) | $10 | $50 | $5 | $25 | $1 |

Notes that matter for the arithmetic:

- The **Batch API is a flat 50% discount on input and output**, and stacks with prompt caching
  ([pricing → batch processing](https://platform.claude.com/docs/en/about-claude/pricing#batch-processing)).
  A nightly digest is exactly the non-latency-sensitive workload it exists for.
- **Cache reads cost 0.1x base input**; minimum cacheable prefix is ~1024 tokens
  ([prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)).
  A stable preference profile + rubric in the system prompt is a textbook cache prefix.
- Claude Sonnet 5's $2/$10 rate is now the **standard** price, not introductory — the previously
  announced increase to $3/$15 on 2026-09-01 was cancelled
  ([pricing note](https://platform.claude.com/docs/en/about-claude/pricing#claude-sonnet-5-introductory-pricing)).
- **Thinking tokens are billed as output.** On Claude 4.6+ models thinking is adaptive; on Opus 5 it
  is on by default. Any Opus/Sonnet cost estimate that assumes a 200-token answer is wrong by
  several times unless effort is pinned low. Budget 5–10x the visible answer length, or set
  `output_config: {effort: "low"}`.
- Claude 4.7+ models use a newer tokenizer producing **~30% more tokens for the same text**
  ([pricing note](https://platform.claude.com/docs/en/about-claude/pricing)). Costs below use
  pre-4.7-tokenizer token counts; add ~30% to input volume for Opus 5 / Fable 5.

---

## 2. Task (a): content extraction from fetched HTML

### What the task demands

Structure recognition over DOM shape. No world knowledge, no judgement. It is a boilerplate-removal
problem with decades of prior art, and the output is *the input text verbatim* — an LLM here is
being asked to copy several thousand tokens, which is both the most expensive possible output and
the one place hallucination is unrecoverable (a fabricated sentence enters the corpus as fact).

### Evidence

Heuristic extractors are already at or near the ceiling of what's measurable:

- Trafilatura's own evaluation (dataset of 990 documents, 2,951 text and 2,966 boilerplate
  segments, run 2026-08-04): trafilatura 2.2.0 standard **F1 0.924 / accuracy 0.923**; magic-html
  0.889; jusText 0.862
  ([Trafilatura evaluation](https://trafilatura.readthedocs.io/en/latest/evaluation.html)).
- Scrapinghub's article-extraction benchmark (independent, now archived) puts
  **`go_readability_fork` at F1 0.947 ± 0.005**, essentially tied with `readability_js` (0.947) and
  within ~1 point of trafilatura (0.958) and its Rust port (0.970); the commercial AutoExtract
  service scored 0.970
  ([scrapinghub/article-extraction-benchmark](https://github.com/scrapinghub/article-extraction-benchmark)).
  This is the closest published proxy for the library this repo actually uses.
- A multi-type extraction benchmark (WCXB) reports rs-trafilatura at F1 0.859 across *diverse* page
  types at 44 ms/page ([arXiv:2605.21097](https://arxiv.org/pdf/2605.21097)) — accuracy drops on
  non-article pages, but so would an LLM's, and 44 ms/page is four orders of magnitude cheaper than
  a model call.

**Evidence gap, stated plainly:** I found no head-to-head benchmark of readability-class extractors
against a small LLM on the *same* labelled dataset. The case for heuristics here rests on
(i) heuristics already scoring 0.92–0.97 F1, leaving little headroom, and (ii) cost/latency being
~10,000x lower, not on a published LLM-loses result.

### Verdict

**No LLM.** Keep `go-readability`. The current design — persist raw HTML in `raw_html_repo` so the
extraction heuristic can be re-run (`internal/storage/raw_html_repo.go`, issue #2) — is the right
shape. If extraction quality becomes the bottleneck, the cheap win is **adding trafilatura-family
extraction as a fallback when `ErrThinContent` fires** (+1–2 F1 points, still free), not routing to
a model. An LLM is defensible only as a last-resort repair path on the small tail of pages where
extraction returns thin content — and even then, prefer "LLM picks the right DOM subtree" over
"LLM rewrites the article".

---

## 3. Task (b): summarization

### What the task demands

Faithfulness and compression. Modest world knowledge, no long context (input is already capped at
8,000 chars), and no comparative judgement. Errors are visible and low-stakes: a mediocre summary
in a personal digest costs a few seconds of the reader's time, and the reader can click through.

### Evidence

- Zhang et al., *Benchmarking Large Language Models for News Summarization* (TACL): across ten LLMs
  with reference summaries written by freelance writers, **"instruction tuning, and not model size,
  is the key to the LLM's zero-shot summarization capability"**, and LLM summaries were judged
  **on par with human-written summaries** ([arXiv:2301.13848](https://arxiv.org/abs/2301.13848)).
  This is the single strongest published result for pushing summarization down-tier.
- Meta's Llama 3.2 model card gives summarization (TLDR9+, 1-shot rougeL): **1B = 16.8, 3B = 19.0,
  Llama 3.1 8B = 17.2** — i.e. the 3B *beats* the 8B on summarization, while the same card shows the
  1B collapsing on instruction following (IFEval 59.5 vs 3B 77.4 vs 8B 80.4)
  ([meta-llama/Llama-3.2-3B-Instruct](https://huggingface.co/meta-llama/Llama-3.2-3B-Instruct)).
  Read together: summarization quality saturates early; *following the format instruction* is what
  scales with size.

### Verdict

**Cheap tier is correct.** The failure mode to watch is not summary quality but instruction
adherence (length, no preamble, no bullet points) — which is what the repo's prompts already lean on
("Return only that sentence — no preamble"). At ≥8B-equivalent (gemma-4-26b, qwen3-30b-a3b,
llama-3.3-70b) that is a solved problem; at 1B it is not.

Reserve frontier only for the **digest intro** if you want it to read well — it is one call a day
and therefore free at any tier (see §7).

---

## 4. Task (c): tagging / categorization

### What the task demands

Closed-set classification against a *live* slug list injected at call time. Needs light world
knowledge and strict output discipline (return exactly one slug, nothing else). No judgement, no
long context.

### Evidence

- BTZSC (38 models, 270M–12B, four families) finds **instruction-tuned LLMs at 4–12B competitive on
  topic classification specifically** — Mistral-Nemo-Instruct 0.69 macro F1, Qwen3-8B 0.65, against
  a reranker state-of-the-art of 0.72 overall and NLI cross-encoders plateauing at 0.50
  ([arXiv:2603.11991](https://arxiv.org/html/2603.11991)). Topic classification is called out as the
  task where small instruction-tuned LLMs do best.
- The counterweight: fine-tuned encoders still beat zero-shot LLMs by ~10–25 accuracy points on
  AG News / BANKING77, with the gap widening on *fine-grained* label sets
  ([ASRJ 2025 study](https://asrjetsjournal.org/American_Scientific_Journal/article/view/12048)).
  Translation for this project: a coarse 8–15 slug taxonomy is safe for a small model; a 60-slug
  taxonomy is not, and would be better served by an embedding classifier than by a bigger LLM.
- Open-source models for text annotation more broadly: performance is usable but sensitive to
  prompt and setup ([arXiv:2307.02179](https://arxiv.org/pdf/2307.02179)).

### Verdict

**Cheap tier, confidently** — as long as the taxonomy stays coarse. There is no world-knowledge
demand here that a 26B model lacks. Frontier spend on tagging buys nothing measurable.

---

## 5. Task (d): relevance scoring against a personal preference profile

### What the task demands

This is the one task with genuine **judgement** content. It is LLM-as-judge: apply a rubric, weigh
an item against a person's stated interests, and produce a calibrated ordinal. It also wants
**comparative** context — "is this in today's top 15?" is a ranking question, and asking for an
absolute 1–5 per item throws away the comparison. Add real world knowledge ("is this actually
globally significant, or does it just sound like it?") — which is precisely what the repo's design
plan relies on ("no external API needed. A well-prompted LLM already knows what's globally
significant", `docs/ai-relevance-feedback-plan.md`, decision 2).

### Evidence

- LLM relevance judgments *can* reach human quality: UMBRELA reproduces Bing's finding that "large
  language models can accurately perform the relevance assessment task and provide human-quality
  judgments", correlating highly with TREC Deep Learning 2019–2023 rankings
  ([arXiv:2406.06519](https://arxiv.org/abs/2406.06519)) — **using GPT-4o**, a frontier-tier model.
  The result is not evidence for the cheap tier.
- *Judge's Verdict* finds that judge tier matters: larger, more capable models achieve higher
  agreement with humans, and smaller judges show "superficial" alignment — agreeing with humans
  partly by coincidence rather than by sound reasoning
  ([arXiv:2510.09738](https://arxiv.org/pdf/2510.09738)). **The paper's headline numbers are
  task-and-domain dependent and I could not extract a clean per-tier agreement figure from it —
  treat this as directional, not quantitative.**
- The genuine counter-evidence: a weak judge (Qwen 2.5 7B) with *high-quality references* beats a
  strong judge (GPT-4o) with synthetic ones
  ([ACM TOIS, On the Use of LLMs for Relevance Labelling](https://dl.acm.org/doi/full/10.1145/3788872)),
  and a distilled small model matched or beat its teacher on 923 enterprise query–document pairs
  after fine-tuning
  ([arXiv:2601.03211](https://arxiv.org/html/2601.03211v1)). Prometheus-13B hit 0.897 Pearson with
  human evaluators *when given an explicit rubric*
  ([Awesome-LLMs-as-Judges](https://github.com/CSHaitao/Awesome-LLMs-as-Judges)).

The synthesis is not "small models can't judge". It is: **small models judge well when the rubric is
explicit and the comparison set is given; they judge badly when asked for an absolute calibrated
score from an implicit standard.** The current `Score` prompt ("1=spam/trivial, 3=worth reading,
5=essential global significance", one article at a time, no comparison set) is the second shape.

**Evidence gap:** nothing published evaluates relevance scoring against a *personal* preference
profile specifically. Personalization is the part where world knowledge and reading-between-the-lines
matter most and where the literature is thinnest. This is the highest-value place to run a local
eval against your own 👍/👎 feedback data, which the feedback loop already collects.

### Verdict — the one place to split the tier

Route this as **two stages**:

1. **Cheap-tier triage over the full feed.** Keep a per-item cheap score, but demote its job from
   "rank the day" to "reject the obvious floor". Its only requirement is high recall of anything
   plausibly interesting — a small model is fine at that.
2. **Frontier-tier shortlist ranking, one batched call.** Take the ~40–60 survivors, send titles +
   short excerpts + the preference profile in a single call, and ask for a ranked top-N with
   one-line reasons. This is where judgement, world knowledge and comparison live, and it is
   *cheaper than the per-item pass it replaces* (§7).

The `Reason` call should ride along in that same shortlist call rather than being a separate
per-item Workers AI call — it is the user-visible sentence, it is produced for ~15 items, and the
frontier model has already read the item.

---

## 6. Structured output at the cheap tier — Workers AI specifics

This is the operational risk of cheap-tier routing, and Workers AI's docs are not fully consistent.

**JSON mode.** [Workers AI JSON mode](https://developers.cloudflare.com/workers-ai/features/json-mode/)
documents `response_format` with `{"type": "json_schema", ...}` (OpenAI convention) on **nine**
models:

`@cf/meta/llama-3.1-8b-instruct-fast`, `@cf/meta/llama-3.1-70b-instruct`,
`@cf/meta/llama-3.3-70b-instruct-fp8-fast`, `@cf/meta/llama-3-8b-instruct`,
`@cf/meta/llama-3.1-8b-instruct`, `@cf/meta/llama-3.2-11b-vision-instruct`,
`@hf/nousresearch/hermes-2-pro-mistral-7b`, `@hf/thebloke/deepseek-coder-6.7b-instruct-awq`,
`@cf/deepseek-ai/deepseek-r1-distill-qwen-32b`.

Caveats, quoted: Cloudflare **"can't guarantee that the model responds according to the requested
JSON Schema"**; a failure returns a `"JSON Mode couldn't be met"` error; and **JSON mode does not
support streaming**.

**Discrepancy worth flagging:** the project's current default, `@cf/google/gemma-4-26b-a4b-it`, is
**not on that nine-model list**, yet its own model page advertises `response_format` support and
function calling ([model page](https://developers.cloudflare.com/workers-ai/models/gemma-4-26b-a4b-it/)).
The features page says the list "will continue to expand". **Do not trust either page — probe it
from a test.** Any code that depends on schema-constrained output from a Workers AI model needs a
parse-failure retry path regardless.

**Function calling.** [Function calling docs](https://developers.cloudflare.com/workers-ai/features/function-calling/)
name only `@hf/nousresearch/hermes-2-pro-mistral-7b` in prose but direct you to the catalogue
capability filter. Filtering the
[model catalogue on Function calling](https://developers.cloudflare.com/workers-ai/models/?capabilities=Function+calling)
returns 16 text models, including `gemma-4-26b-a4b-it`, `gpt-oss-20b`/`120b`, `granite-4.0-h-micro`,
`llama-3.3-70b-instruct-fp8-fast`, `llama-4-scout-17b-16e-instruct`, `mistral-small-3.1-24b-instruct`,
`qwen3-30b-a3b-fp8`, `glm-4.7-flash`, `kimi-k2.6`, `nemotron-3-120b-a12b`.

**How reliable is tool-call structure at small sizes?** Meta's own numbers, on the Berkeley Function
Calling Leaderboard v2: **Llama 3.2 1B = 25.7%, 3B = 67.0%**; Nexus 13.5% vs 34.3%
([Llama 3.2 3B model card](https://huggingface.co/meta-llama/Llama-3.2-3B-Instruct)). Sub-3B models
are not usable for tool/JSON-shaped output. That is the floor to stay above.

**Practical recommendation.** This pipeline's cheap-tier outputs are *tiny* — one integer, one slug,
one sentence. Constrained decoding is overkill and adds a failure mode. Keep the current
"return only the integer / only the slug" prompting plus strict server-side parsing and a
reject-and-retry, which is what `internal/ai/cloudflare.go` already does. Reserve JSON mode for the
one place a structured payload is genuinely needed — the batched shortlist ranking — and run that on
Claude, where structured outputs (`output_config.format`) and `strict: true` tools are
first-class ([tool use](https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview)).

---

## 7. Cost per day — the arithmetic, with assumptions

### Token assumptions

| Quantity | Value | Where it comes from |
|---|---|---|
| Feed volume | 100 / 200 / 300 entries per day | issue #51 |
| Shortlist | 15 entries per day | issue #51 (10–20) |
| Article body sent to model | 8,000 chars ≈ **2,000 tokens** | `AI_MAX_CONTENT_CHARS=8000`, ~4 chars/token |
| System prompt + title + weights | ~150 tokens (score/reason), ~200 (categorize, incl. slug list) | `internal/ai/prompts.go` |
| Score call | 2,150 in / 5 out | 1–5 integer |
| Categorize call | 2,200 in / 5 out | one slug |
| Reason call | 2,150 in / 30 out | ≤20 words |
| Group summary | 1,500 in / 90 out, ~8 groups | `Summarize` over a tag group |
| Digest intro | 800 in / 80 out, 1 call | `Digest` |
| Shortlist candidates surviving triage | 50 items | ~25% of a 200-entry day |
| Batched shortlist call | 50 × 300-token excerpt + 1,000-token profile/rubric ≈ **16,000 in**; 15 × ~60-token reasons ≈ **1,000 out** | one comparative call |

Rounding is deliberate: these are order-of-magnitude figures, and every one of them is dominated by
the 2,000-token body, so the honest sensitivity statement is *cost scales with
`entries × AI_MAX_CONTENT_CHARS`*. Halving the body cap halves the bill.

### Option A — status quo: everything on Workers AI `gemma-4-26b-a4b-it`

At **200 entries/day** (score + categorize on all, reason on shortlist only):

| Call | Input tokens | Output tokens | Cost |
|---|---|---|---|
| Score × 200 | 430,000 | 1,000 | $0.0433 |
| Categorize × 200 | 440,000 | 1,000 | $0.0443 |
| Reason × 15 | 32,250 | 450 | $0.0034 |
| Summaries × 8 | 12,000 | 720 | $0.0014 |
| Digest × 1 | 800 | 80 | $0.0001 |
| **Total** | **915,050** | **3,250** | **$0.092 / day** |

≈ **$2.77 / month** at list price. But convert to Neurons before believing that:
0.915M in × 9,091 + 0.00325M out × 27,273 ≈ **8,410 Neurons/day** — **under Cloudflare's free
allocation of 10,000 Neurons/day**
([pricing](https://developers.cloudflare.com/workers-ai/platform/pricing/)).

| Feed volume | Neurons/day | Billable | Cost/day |
|---|---|---|---|
| 100 entries | ~4,300 | 0 | **$0.00** |
| 200 entries | ~8,410 | 0 | **$0.00** |
| 300 entries | ~12,500 | 2,500 | **$0.028** (~$0.83/mo) |

**The status quo is free or near-free at this volume.** Any proposal to spend money has to justify
itself against a $0 baseline.

### Option B — full feed on Claude Haiku 4.5 (for comparison)

200 entries/day, same token counts, Haiku 4.5 at $1/$5:

- Score: 430,000 × $1/M = $0.430; output 1,000 × $5/M = $0.005
- Categorize: 440,000 × $1/M = $0.440; output $0.005
- Reason ×15 + summaries + digest: ≈ $0.047
- **Total ≈ $0.93 / day ≈ $28 / month.** With the Batch API (50%): **≈ $0.46/day ≈ $14/month.**

That is ~10x the Workers AI list price and ∞x the effective ($0) price, to buy quality on two tasks
(§3, §4) where the published evidence says the small model is already adequate. **Don't.**

### Option C — recommended split: cheap triage + one frontier shortlist call

Workers AI keeps score + categorize + group summaries (≈ 8,400 Neurons/day at 200 entries → **$0**,
or $0.028/day at 300 entries). Add **one** batched shortlist-ranking call per day:
16,000 input / 1,000 output.

Frontier output is dominated by thinking tokens, which bill as output
([pricing](https://platform.claude.com/docs/en/about-claude/pricing)). Two output scenarios:
`effort: "low"` (~1,000 output) and default adaptive thinking (~5,000 output).

| Model | Standard, low effort | Standard, adaptive thinking | Batch API (50%), low effort |
|---|---|---|---|
| Haiku 4.5 ($1/$5) | $0.021 | $0.041 | **$0.011** |
| Sonnet 5 ($2/$10) | $0.042 | $0.082 | **$0.021** |
| Opus 5 ($5/$25)¹ | $0.129 | $0.229 | **$0.065** |
| Fable 5 ($10/$50)¹ | $0.258 | $0.458 | **$0.129** |

¹ Opus 5 / Fable 5 use the newer tokenizer (~30% more tokens); input inflated to 20,800 accordingly.

**One Opus 5 call per day costs about 13 cents — roughly $3.90/month — and that is the entire
frontier bill.** With the Batch API (a nightly digest is not latency-sensitive) it is ~$2/month.
Prompt-caching the profile + rubric prefix (1h write 2x, reads 0.1x) shaves further once the prefix
exceeds 1,024 tokens, though at one call/day a 1-hour cache never gets a second read — **caching is
not worth it for a once-daily call**; it becomes worth it only if the shortlist call is re-run
interactively.

### Totals, dated 2026-08-22

| Configuration | 100/day | 200/day | 300/day |
|---|---|---|---|
| A. All Workers AI (status quo) | $0.00 | $0.00 | $0.028 |
| C. Workers AI triage + 1 Opus 5 shortlist call, batched | $0.065 | $0.065 | $0.093 |
| C′. Same, with Sonnet 5 instead | $0.021 | $0.021 | $0.049 |
| B. All Claude Haiku 4.5, batched | $0.23 | $0.46 | $0.70 |
| All Claude Opus 5 per-item (the naive frontier build) | ~$1.2 | ~$2.4 | ~$3.6 |

**Recommendation C costs about $2/month.** It is not a cost decision — at this volume nothing here
is a cost decision. It is a quality decision that happens to be affordable.

---

## 8. Batched vs per-item

| Task | Shape | Why |
|---|---|---|
| Extraction | Per-item, no LLM | Deterministic function of one document. |
| Cheap triage score | **Per-item** | Input is a 2,000-token body; batching 10 of those is a 20K-token prompt where small models lose track of item boundaries and drift toward the middle of the batch. Per-item also gives failure isolation (one bad HTML page fails one score, not ten) and lets ingestion parallelise. Since each item is scored once and cached in `relevance_score`, there is no repeated-prefix cost to amortise. |
| Tagging | **Batchable (10–20 items) if inputs are truncated** | Output is one slug; the slug list is a fixed prefix worth amortising. Only safe if you send title + first ~200 words rather than the full body, and require the model to echo the item ID with each slug — index-misalignment is the classic batched-classification failure. Given the task is already free, batching here is an optimisation with a correctness risk and **no payoff**: keep it per-item until volume changes. |
| Reason / one-line "why it matters" | **Batched into the shortlist call** | Only needed for the ~15 shortlisted items, and the frontier model has already read them in that call. A separate per-item call re-pays the input cost for no gain. |
| Shortlist relevance ranking | **Must be batched — one call** | This is the whole point: ranking is comparative. Per-item absolute scores are exactly the shape the LLM-as-judge literature says small models fake well and calibrate badly (§5). One call also means one place to put the preference profile. |
| Per-group summary | **Inherently batched** | Already takes a group of articles. |
| Digest intro | Single call over summaries | Already batched by construction. |

Cross-cutting: Cloudflare's [Batch API](https://developers.cloudflare.com/workers-ai/features/batch-api/)
is *async request batching* (queue many inference requests, poll for results; payload < 10 MB), not a
price discount, and the docs state no pricing difference. It's a throughput/rate-limit tool for the
ingestion pass, not a cost lever. Anthropic's Batch API **is** a 50% price lever and applies cleanly
to the nightly shortlist call.

---

## 9. Proposed routing table

Costs are at **200 entries/day, 15 shortlisted, priced 2026-08-22**.

| Task | Recommended tier / model | Est. daily cost | Confidence |
|---|---|---|---|
| **(a) Content extraction** | **No LLM** — `go-readability` (current); add a trafilatura-family fallback on `ErrThinContent` | **$0.00** | **High** — heuristics at F1 0.947 (go_readability_fork) / 0.924–0.958 (trafilatura) leave little headroom; LLM copying full article text is the worst cost/risk trade in the pipeline. *Caveat: no published LLM-vs-readability head-to-head exists.* |
| **(b) Summarization (per-group + per-item)** | **Cheap** — Workers AI `@cf/google/gemma-4-26b-a4b-it` (current). Cheaper alternates: `qwen3-30b-a3b-fp8` / `llama-3.1-8b-instruct-fp8-fast` at ~half the input rate | **$0.0015** (free-tier absorbed) | **High** — instruction tuning, not size, drives zero-shot summarization; LLM summaries rated on par with human ones (TACL). Stay ≥3B: 1B-class models fail instruction following (IFEval 59.5). |
| **(c) Tagging / categorization** | **Cheap** — same Workers AI model, per-item, plain-text slug + strict parse (not JSON mode) | **$0.044** (free-tier absorbed) | **High** for a coarse ≤15-slug taxonomy (4–12B LLMs competitive on topic classification, BTZSC). **Medium** if the taxonomy grows fine-grained — zero-shot LLMs trail fine-tuned encoders by 10–25 points there; switch to an embedding classifier, not a bigger LLM. |
| **(d1) Full-feed relevance triage** | **Cheap** — Workers AI, per-item, 1–5 integer, tuned for **recall not ranking** (job: drop the floor, pass ~25% up) | **$0.043** (free-tier absorbed) | **Medium** — small judges show "superficial" human alignment (Judge's Verdict), but a high-recall floor filter is a much weaker ask than calibrated ranking. Validate against the existing 👍/👎 feedback data. |
| **(d2) Shortlist ranking vs preference profile + "why it matters"** | **Frontier** — `claude-opus-5`, **one batched call/day** over ~50 candidates, `effort: "low"`, structured output, via the Batch API | **$0.065** | **Medium-High** that frontier is the right tier (UMBRELA's human-quality relevance judgments were GPT-4o-class; judge capability tracks tier); **Low-Medium** on the exact model — Sonnet 5 at $0.021/day may well be indistinguishable here, and nothing published evaluates *personal-profile* relevance scoring. **Run an A/B against your own feedback data before settling.** |
| **(e) Digest intro** | Either. Cheap by default; fold into the (d2) frontier call if you want it to read better — it's one call/day, so cost is noise | **~$0.0001** | **High** |
| **Total** | | **≈ $0.065 / day actual spend (~$2 / month)** — the single frontier call is the whole bill; the Workers AI rows list at ~$0.09/day but fall inside the 10,000 Neurons/day free allocation up to ~250 entries/day | |

### The two changes this implies

1. **Stop treating per-item cheap scores as the ranking.** Demote them to a recall filter and add one
   batched frontier ranking call. This is the whole quality delta, and it costs ~$2/month.
2. **Fold `Reason` into that call.** It is user-visible prose for 15 items produced by a model that
   has already read them; a separate cheap per-item call pays input cost twice for a worse sentence.

### What would change this table

- Feed volume above ~250 entries/day pushes Workers AI past the free allocation (still cents).
- Raising `AI_MAX_CONTENT_CHARS` scales the cheap-tier bill linearly — it is the dominant term.
- A fine-grained taxonomy moves (c) off LLMs entirely and onto embeddings/rerankers
  (`bge-m3` at $0.012/M tokens, `bge-reranker-base` at $0.003/M).
- Accumulated 👍/👎 data makes a distilled/fine-tuned small scorer viable for (d2) — the enterprise
  search result (923 pairs) suggests a distilled SLM can match its teacher on a narrow relevance task.

---

## Sources

Prices and capability claims checked **2026-08-22**.

- [Cloudflare Workers AI — pricing](https://developers.cloudflare.com/workers-ai/platform/pricing/)
- [Cloudflare Workers AI — model catalogue, function-calling filter](https://developers.cloudflare.com/workers-ai/models/?capabilities=Function+calling)
- [Cloudflare Workers AI — JSON mode](https://developers.cloudflare.com/workers-ai/features/json-mode/)
- [Cloudflare Workers AI — function calling](https://developers.cloudflare.com/workers-ai/features/function-calling/)
- [Cloudflare Workers AI — Batch API](https://developers.cloudflare.com/workers-ai/features/batch-api/)
- [Cloudflare Workers AI — gemma-4-26b-a4b-it model page](https://developers.cloudflare.com/workers-ai/models/gemma-4-26b-a4b-it/)
- [Claude — pricing](https://platform.claude.com/docs/en/about-claude/pricing)
- [Claude — prompt caching](https://platform.claude.com/docs/en/build-with-claude/prompt-caching)
- [Claude — tool use overview](https://platform.claude.com/docs/en/agents-and-tools/tool-use/overview)
- [Trafilatura — evaluation](https://trafilatura.readthedocs.io/en/latest/evaluation.html)
- [scrapinghub/article-extraction-benchmark](https://github.com/scrapinghub/article-extraction-benchmark)
- [WCXB: A Multi-Type Web Content Extraction Benchmark (arXiv:2605.21097)](https://arxiv.org/pdf/2605.21097)
- [Zhang et al., Benchmarking LLMs for News Summarization (arXiv:2301.13848)](https://arxiv.org/abs/2301.13848)
- [Meta Llama 3.2 3B Instruct model card](https://huggingface.co/meta-llama/Llama-3.2-3B-Instruct)
- [BTZSC zero-shot text classification benchmark (arXiv:2603.11991)](https://arxiv.org/html/2603.11991)
- [Bridging Zero-Shot and Fine-Tuned Performance in Text Classification](https://asrjetsjournal.org/American_Scientific_Journal/article/view/12048)
- [Open-Source LLMs for Text Annotation (arXiv:2307.02179)](https://arxiv.org/pdf/2307.02179)
- [UMBRELA (arXiv:2406.06519)](https://arxiv.org/abs/2406.06519)
- [Judge's Verdict (arXiv:2510.09738)](https://arxiv.org/pdf/2510.09738)
- [On the Use of LLMs for Relevance Labelling (ACM TOIS)](https://dl.acm.org/doi/full/10.1145/3788872)
- [Fine-tuning SLMs as Enterprise Search Relevance Labelers (arXiv:2601.03211)](https://arxiv.org/html/2601.03211v1)
- [Awesome-LLMs-as-Judges (Prometheus correlation figure)](https://github.com/CSHaitao/Awesome-LLMs-as-Judges)
