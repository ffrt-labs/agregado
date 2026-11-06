# Prompt experiments (`evals/`)

A small Python project that runs one label of an Agregado prompt (say
`staging`) against a Langfuse dataset with the Langfuse Python SDK's
experiment runner. Each run becomes a dataset run you can compare with a
`production` run in Langfuse's experiment comparison view, with each item's
output and evaluator scores and an aggregate per run.

It is separate from the Go module and only runs when you start it by hand.
Nothing in Agregado imports it.

So far it covers one operation: `preference_score` on the `score-golden`
dataset.

## Setup

1. Install [uv](https://docs.astral.sh/uv/). It reads `pyproject.toml` and
   `uv.lock` (Python 3.11+, `langfuse==5.1.0`, `openai==3.46.0`) and creates
   `evals/.venv` on first run.
2. Put the credentials in the root `.env`. The Makefile loads it:

   | Variable | Use |
   |---|---|
   | `LANGFUSE_PUBLIC_KEY`, `LANGFUSE_SECRET_KEY` | the `agregado` project's key pair |
   | `LANGFUSE_BASE_URL` | Langfuse base URL |
   | `LANGFUSE_EVAL_BASE_URL` | optional override for this harness. Agregado's own `LANGFUSE_BASE_URL` is `http://langfuse-web:3000`, which only resolves on the `langfuse-ingest` Docker network. From a laptop, set this to the tailnet UI URL |
   | `CLOUDFLARE_ACCOUNT_ID`, `CLOUDFLARE_API_TOKEN` | Workers AI, the same as production |
   | `AI_MODEL` | optional, defaults to production's `@cf/google/gemma-4-26b-a4b-it` |

3. You need a dataset (below) and a prompt version carrying the label you
   want to test.

### The dataset

Building `score-golden` is annotation work you do yourself in the Langfuse UI.
The usual path: open a `preference_score` generation on an `article-enrichment`
trace and choose **Add to dataset**. The generation's input is already the
variables the harness needs (`preferences`, `title`, `content`), recorded after
Agregado cleaned and truncated the content. Then set the item's
**expected output** to your own Score for that Article:

```json
4
```

`"4"` and `{"score": 4}` also work. Items with no usable label (empty, outside
1-5, or some other shape) still run and get a `score_parses` score, but no
`score_agrees` score, so they don't affect the agreement rate. Archived items
are skipped.

The harness refuses to start if an item's input isn't exactly the operation's
variables, because Agregado could not have produced that item.

## Running

Run it after the 08:00 Digest:

```bash
make experiment LABEL=staging ITEMS=10
make experiment LABEL=production ITEMS=10        # the baseline to compare against
make experiment LABEL=staging ITEMS=10 DATASET=score-golden OPERATION=preference_score
make experiment-test                              # evaluator unit tests, no network
```

`ITEMS` is required, and there is no "whole dataset" default. Without it, the
harness exits before it reads any credentials.

Each item goes through these steps:

1. Fetch `agregado/<operation>` at the label, uncached.
2. Check that the version is exactly a system and a user message, with no
   placeholders, and uses exactly the operation's variables. This is what
   Agregado's Go compiler accepts. The Langfuse SDK quietly leaves an unknown
   `{{variable}}` in the text, so a version Go would reject could still score
   well here and then never run in production. The harness refuses such a
   version.
3. Compile the version with the item's input.
4. Call Workers AI's OpenAI-compatible endpoint (`/ai/v1/chat/completions`)
   with production's generation controls for the operation. For
   `preference_score` that is `max_completion_tokens: 32` and
   `chat_template_kwargs: {enable_thinking: false}`, from
   `internal/ai/cloudflare.go`. Calls run one at a time with no retries.
5. Record the call as a generation linked to the prompt version, with token
   usage, so Langfuse shows its cost in neurons.

### Evaluators

| Score | Level | Meaning |
|---|---|---|
| `score_parses` | item, boolean | The answer is an integer from 1 to 5, as `parseScore` in `internal/ai` reads it. A truncated answer (`finish_reason: length`) counts as 0, because production fails that call |
| `score_agrees` | item, boolean | The parsed answer equals the annotation. Only on annotated items |
| `score_parses_rate`, `score_agrees_rate` | run | The share of items scoring 1, over the items that have that score |

An item whose call fails (an HTTP error or a 429) is logged and dropped from
the run with no scores. The harness then says how many items were dropped.

## Neuron budget

Workers AI's free tier allows 10,000 neurons a day, and Agregado's own
enrichment and the 08:00 Digest need that budget. One `preference_score` item
costs up to about 45 neurons: at most about 4,700 input tokens (an 8 KiB
PREFERENCES.md plus 8,000 characters of content) at 9,091 neurons per million,
plus up to 32 output tokens. So `ITEMS=20` costs up to about 900 neurons, and
a staging-vs-production pair costs twice that. Keep runs small and run them
after the Digest, never before.

## Promoting a winning version

1. Compare the `staging` and `production` runs in the dataset's experiment
   comparison view: `score_agrees_rate` first, then `score_parses_rate`, then
   individual items where they differ.
2. In Langfuse, open `agregado/preference_score`, select the winning version
   and add the `production` label to it. Labels are unique, so this moves the
   label off the old version. The API equivalent is
   `PATCH /api/public/v2/prompts/agregado%2Fpreference_score/versions/<n>` with
   `{"newLabels": ["production"]}`.
3. Agregado picks it up within `LANGFUSE_PROMPT_CACHE_TTL` (60s), with no
   restart. To roll back, move `production` back to the previous version.

The promoted version must keep exactly the variables `preferences`, `title` and
`content`. If it doesn't, Agregado falls back to its in-code default and logs a
warning. The harness's contract check catches this before you promote.

## Layout

- `agregado_evals/cli.py`: arguments, credential check, prompt and dataset
  fetch, `run_experiment`
- `agregado_evals/harness.py`: item selection and the experiment task
- `agregado_evals/contract.py`: the prompt and item shape Go accepts
- `agregado_evals/operations.py`: each operation's variables, token cap and
  evaluators
- `agregado_evals/preference_score.py`, `evaluators.py`: evaluators
- `tests/`: unit tests. A fixture blocks every socket, so the tests can't
  reach the network
