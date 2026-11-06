"""The Agregado operations the harness can run, each with production's
variables, generation controls and evaluators (internal/ai)."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Callable

from . import preference_score


@dataclass(frozen=True)
class Operation:
    name: str
    # The variable names Go supplies (internal/ai/template.go
    # defaultUserTemplates); a prompt version must use exactly these.
    variables: frozenset[str]
    # Production's cap for this operation (internal/ai/cloudflare.go
    # shortAnswerOptions etc.). Reasoning is always disabled.
    max_completion_tokens: int
    evaluators: tuple[Callable, ...]

    @property
    def prompt_name(self) -> str:
        return f"agregado/{self.name}"


OPERATIONS = {
    "preference_score": Operation(
        name="preference_score",
        variables=frozenset({"preferences", "title", "content"}),
        max_completion_tokens=32,
        evaluators=(preference_score.score_parses, preference_score.score_agrees),
    ),
}
