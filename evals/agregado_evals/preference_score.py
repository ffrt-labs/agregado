"""preference_score: the operation that turns PREFERENCES.md plus an Article
into a Score from 1 to 5. Mirrors internal/ai (ScoreWithPreferences,
parseScore, shortAnswerOptions)."""

from __future__ import annotations

import re
from typing import Any

from langfuse import Evaluation

# Go's strconv.Atoi grammar: optional sign, ASCII digits only.
_ATOI = re.compile(r"[+-]?[0-9]+")


def parse_score(output: Any) -> int | None:
    """The Score Agregado would store for this task output, or None where
    production would fail the call (truncated, not an integer, out of range)."""
    if not isinstance(output, dict) or output.get("finish_reason") == "length":
        return None
    text = str(output.get("text") or "").strip()
    if not _ATOI.fullmatch(text):
        return None
    score = int(text)
    return score if 1 <= score <= 5 else None


def score_parses(*, output: Any, **kwargs: Any) -> Evaluation:
    """Code check: the answer parses as an integer Score from 1 to 5."""
    ok = parse_score(output) is not None
    return Evaluation(name="score_parses", value=1 if ok else 0, data_type="BOOLEAN")


def annotated_score(expected_output: Any) -> int | None:
    """The owner's Score label on a dataset item: an integer 1-5, given bare,
    as a string, or as {"score": ...}. None when the item isn't annotated."""
    if isinstance(expected_output, dict):
        expected_output = expected_output.get("score")
    if isinstance(expected_output, bool):
        return None
    if isinstance(expected_output, int):
        label = expected_output
    elif isinstance(expected_output, str) and _ATOI.fullmatch(expected_output.strip()):
        label = int(expected_output.strip())
    else:
        return None
    return label if 1 <= label <= 5 else None


def score_agrees(*, output: Any, expected_output: Any = None, **kwargs: Any) -> Evaluation | list:
    """Agreement with the annotation: the parsed answer equals the label.
    An unannotated item gets no score, so it can't dilute the agreement rate."""
    label = annotated_score(expected_output)
    if label is None:
        return []
    score = parse_score(output)
    return Evaluation(
        name="score_agrees",
        value=1 if score == label else 0,
        data_type="BOOLEAN",
        comment=f"answered {score if score is not None else 'no Score'}, annotated {label}",
    )
