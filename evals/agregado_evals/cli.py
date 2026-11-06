"""make eval: run one prompt label against a Langfuse dataset as a Langfuse
experiment, capped so it can't spend the day's neurons."""

from __future__ import annotations

import argparse
import functools
import os
import sys
from datetime import datetime, timezone

from .contract import ContractError, check_item_input, check_prompt
from .evaluators import rates
from .harness import make_task, select_items
from .operations import OPERATIONS

DEFAULT_MODEL = "@cf/google/gemma-4-26b-a4b-it"  # internal/config AI_MODEL default
REQUIRED_ENV = (
    "LANGFUSE_PUBLIC_KEY",
    "LANGFUSE_SECRET_KEY",
    "LANGFUSE_BASE_URL",
    "CLOUDFLARE_ACCOUNT_ID",
    "CLOUDFLARE_API_TOKEN",
)


def _positive_int(value: str) -> int:
    try:
        n = int(value)
    except ValueError:
        raise argparse.ArgumentTypeError(f"must be a whole number of items, got {value!r}")
    if n < 1:
        raise argparse.ArgumentTypeError(f"must be at least 1, got {n}")
    return n


def _label(value: str) -> str:
    if not value.strip():
        raise argparse.ArgumentTypeError("must name a prompt label, e.g. staging")
    return value.strip()


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="agregado-evals",
        description="Run a Langfuse prompt label against a Langfuse dataset on Workers AI.",
    )
    parser.add_argument("--operation", choices=sorted(OPERATIONS), default="preference_score")
    parser.add_argument("--dataset", default="score-golden")
    parser.add_argument("--label", type=_label, required=True, help="prompt label to run, e.g. staging")
    parser.add_argument(
        "--max-items",
        type=_positive_int,
        required=True,
        help="required cap on dataset items (each costs neurons from the 10k/day budget)",
    )
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    missing = [name for name in REQUIRED_ENV if not os.environ.get(name)]
    if missing:
        print(f"missing environment: {', '.join(missing)}", file=sys.stderr)
        return 2

    # Imported here so --help and argument errors need no SDK start-up.
    from langfuse import get_client
    from openai import OpenAI

    operation = OPERATIONS[args.operation]
    model = os.environ.get("AI_MODEL") or DEFAULT_MODEL
    langfuse = get_client()

    prompt = langfuse.get_prompt(operation.prompt_name, label=args.label, type="chat", cache_ttl_seconds=0)
    try:
        check_prompt(prompt.prompt, operation.variables)
    except ContractError as e:
        print(f"{operation.prompt_name} label {args.label} (v{prompt.version}): {e}", file=sys.stderr)
        return 1

    dataset = langfuse.get_dataset(args.dataset)
    items = select_items(dataset.items, args.max_items)
    if not items:
        print(f"dataset {args.dataset} has no active items", file=sys.stderr)
        return 1
    bad = []
    for item in items:
        try:
            check_item_input(item.input, operation.variables)
        except ContractError as e:
            bad.append(f"  {item.id}: {e}")
    if bad:
        print("items Agregado couldn't have produced:\n" + "\n".join(bad), file=sys.stderr)
        return 1

    client = OpenAI(
        api_key=os.environ["CLOUDFLARE_API_TOKEN"],
        base_url=f"https://api.cloudflare.com/client/v4/accounts/{os.environ['CLOUDFLARE_ACCOUNT_ID']}/ai/v1",
        max_retries=0,  # a retry spends neurons twice; a failed item is just dropped
    )
    task = make_task(
        operation=operation,
        prompt=prompt,
        client=client,
        model=model,
        start_generation=functools.partial(langfuse.start_as_current_observation, as_type="generation"),
    )

    stamp = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
    result = langfuse.run_experiment(
        name=f"{operation.name} {args.label}",
        run_name=f"{operation.name}-{args.label}-v{prompt.version}-{stamp}",
        description=f"{operation.prompt_name} label {args.label} (v{prompt.version}) on {len(items)} items of {args.dataset}",
        data=items,
        task=task,
        evaluators=list(operation.evaluators),
        run_evaluators=[rates],
        max_concurrency=1,  # Workers AI free tier: no bursts
        metadata={
            "prompt_label": args.label,
            "prompt_version": str(prompt.version),
            "model": model,
            "max_items": str(args.max_items),
        },
    )
    print(result.format())
    if result.experiment_url:
        print(result.experiment_url)
    langfuse.flush()
    failed = len(items) - len(result.item_results)
    if failed:
        print(f"{failed} of {len(items)} items failed (see the log above); they have no scores", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
