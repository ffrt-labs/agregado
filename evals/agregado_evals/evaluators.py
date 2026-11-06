"""Evaluators shared by every operation."""

from __future__ import annotations

from collections import defaultdict
from typing import Any

from langfuse import Evaluation


def rates(*, item_results: list, **kwargs: Any) -> list[Evaluation]:
    """Run-level aggregate: for each BOOLEAN item score, the share of items
    that scored 1, over the items that carry that score."""
    values: dict[str, list[float]] = defaultdict(list)
    for result in item_results:
        for evaluation in result.evaluations:
            if evaluation.data_type == "BOOLEAN":
                values[evaluation.name].append(float(evaluation.value))
    return [
        Evaluation(
            name=f"{name}_rate",
            value=sum(vs) / len(vs),
            comment=f"{int(sum(vs))} of {len(vs)} items",
        )
        for name, vs in values.items()
    ]
