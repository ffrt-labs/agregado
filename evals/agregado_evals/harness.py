"""The experiment task: one dataset item through one prompt version on
Workers AI, the way Agregado would run it."""

from __future__ import annotations

from typing import Any, Callable

from .contract import check_item_input
from .operations import Operation


def select_items(items: list, cap: int) -> list:
    """The first cap active dataset items. Slicing dataset.items keeps each
    item's dataset link, so the run still lands on the dataset's runs."""
    return [i for i in items if getattr(i, "status", "ACTIVE") != "ARCHIVED"][:cap]


def make_task(
    *,
    operation: Operation,
    prompt: Any,
    client: Any,
    model: str,
    start_generation: Callable,
) -> Callable:
    """Build the run_experiment task.

    prompt is a Langfuse ChatPromptClient, client an OpenAI client pointed at
    Workers AI's /ai/v1 endpoint, and start_generation a context manager
    factory taking Langfuse generation fields (start_as_current_observation
    with as_type="generation" bound)."""
    controls = {
        "max_completion_tokens": operation.max_completion_tokens,
        # Same switch production sends (internal/ai completionRequest).
        "extra_body": {"chat_template_kwargs": {"enable_thinking": False}},
    }
    model_parameters = {
        "max_completion_tokens": operation.max_completion_tokens,
        "enable_thinking": False,
    }

    def task(*, item: Any, **kwargs: Any) -> dict:
        variables = item["input"] if isinstance(item, dict) else item.input
        check_item_input(variables, operation.variables)
        messages = prompt.compile(**variables)
        with start_generation(
            name=operation.name,
            model=model,
            input=variables,
            model_parameters=model_parameters,
            prompt=prompt,
        ) as generation:
            response = client.chat.completions.create(model=model, messages=messages, **controls)
            choice = response.choices[0]
            output = {"text": choice.message.content or "", "finish_reason": choice.finish_reason}
            generation.update(
                output=output,
                usage_details={
                    "input": response.usage.prompt_tokens,
                    "output": response.usage.completion_tokens,
                },
            )
        return output

    return task
