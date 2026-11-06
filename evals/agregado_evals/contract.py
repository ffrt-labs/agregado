"""The prompt and dataset item shape Agregado's Go code accepts.

Go (internal/ai Prompt.Compile, internal/langfuse prompts.go) wants a chat
prompt of exactly [system, user], no placeholders, and every supplied variable
used with none undefined. The Langfuse SDK's compile is laxer: it leaves an
unknown {{name}} in the text. Running an experiment on a prompt Go would
reject would measure something production never runs, so the harness refuses.
"""

from __future__ import annotations

import re
from typing import Any, Iterable

# internal/ai/template.go variableRe: only identifiers are variables; other
# {{...}} text stays literal in Go and in the Langfuse SDK alike.
_VARIABLE = re.compile(r"\{\{\s*([A-Za-z_][A-Za-z0-9_]*)\s*\}\}")


class ContractError(ValueError):
    """The prompt or an item doesn't fit what Agregado runs in production."""


def check_prompt(messages: Iterable[dict], variables: frozenset[str]) -> None:
    messages = list(messages)
    shape = [(m.get("type"), m.get("role")) for m in messages]
    if shape != [("message", "system"), ("message", "user")]:
        raise ContractError(
            f"prompt must be exactly a system and a user message, got {shape}"
        )
    used = {
        name
        for m in messages
        for name in _VARIABLE.findall(m["content"])
    }
    problems = []
    if unknown := sorted(used - variables):
        problems.append("unknown: " + ", ".join(unknown))
    if unused := sorted(variables - used):
        problems.append("unused: " + ", ".join(unused))
    if problems:
        raise ContractError(
            f"prompt variables differ from {sorted(variables)} ({'; '.join(problems)})"
        )


def check_item_input(item_input: Any, variables: frozenset[str]) -> None:
    if not isinstance(item_input, dict):
        raise ContractError(
            "item input is not a variables object; add items from an Agregado generation, whose input is its variables"
        )
    keys = set(item_input)
    problems = []
    if missing := sorted(variables - keys):
        problems.append("missing: " + ", ".join(missing))
    if unexpected := sorted(keys - variables):
        problems.append("unexpected: " + ", ".join(unexpected))
    if problems:
        raise ContractError(f"item input variables differ ({'; '.join(problems)})")
