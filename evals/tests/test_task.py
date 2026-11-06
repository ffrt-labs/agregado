"""The experiment task compiles the chosen prompt version with the item's
recorded variables and calls Workers AI with production's generation controls.
The OpenAI-compatible client and the Langfuse generation are fakes: no network."""

from contextlib import contextmanager
from types import SimpleNamespace

import pytest
from langfuse.api import Prompt_Chat
from langfuse.model import ChatPromptClient

from agregado_evals.contract import ContractError
from agregado_evals.harness import make_task
from agregado_evals.operations import OPERATIONS

MODEL = "@cf/google/gemma-4-26b-a4b-it"
VARIABLES = {"preferences": "Likes Go.", "title": "Go 1.26", "content": "Released."}


def prompt():
    return ChatPromptClient(
        Prompt_Chat(
            name="agregado/preference_score",
            version=7,
            prompt=[
                {"role": "system", "content": "Score 1-5."},
                {"role": "user", "content": "PREFERENCES.md:\n{{preferences}}\n\nTitle: {{title}}\n\nContent: {{content}}"},
            ],
            config={},
            labels=["staging"],
            tags=[],
        )
    )


class FakeChat:
    def __init__(self, content="4", finish_reason="stop"):
        self.requests = []
        self._reply = SimpleNamespace(
            choices=[SimpleNamespace(message=SimpleNamespace(content=content), finish_reason=finish_reason)],
            usage=SimpleNamespace(prompt_tokens=900, completion_tokens=2),
        )
        self.chat = SimpleNamespace(completions=SimpleNamespace(create=self._create))

    def _create(self, **kwargs):
        self.requests.append(kwargs)
        return self._reply


class FakeGenerations:
    def __init__(self):
        self.started = []
        self.updates = []

    @contextmanager
    def __call__(self, **kwargs):
        self.started.append(kwargs)
        yield SimpleNamespace(update=lambda **u: self.updates.append(u))


def run(chat, generations, item_input=VARIABLES):
    task = make_task(
        operation=OPERATIONS["preference_score"],
        prompt=prompt(),
        client=chat,
        model=MODEL,
        start_generation=generations,
    )
    return task(item=SimpleNamespace(input=item_input))


def test_the_call_uses_production_generation_controls():
    chat = FakeChat()

    run(chat, FakeGenerations())

    (request,) = chat.requests
    assert request["model"] == MODEL
    assert request["max_completion_tokens"] == 32
    assert request["extra_body"] == {"chat_template_kwargs": {"enable_thinking": False}}


def test_the_messages_are_the_prompt_version_compiled_with_the_item_variables():
    chat = FakeChat()

    run(chat, FakeGenerations())

    assert chat.requests[0]["messages"] == [
        {"role": "system", "content": "Score 1-5."},
        {"role": "user", "content": "PREFERENCES.md:\nLikes Go.\n\nTitle: Go 1.26\n\nContent: Released."},
    ]


def test_the_output_is_the_answer_and_why_it_stopped():
    assert run(FakeChat("4"), FakeGenerations()) == {"text": "4", "finish_reason": "stop"}
    assert run(FakeChat("Let me", "length"), FakeGenerations()) == {"text": "Let me", "finish_reason": "length"}


def test_the_call_is_recorded_as_a_generation_linked_to_the_prompt_version():
    generations = FakeGenerations()

    run(FakeChat(), generations)

    (started,) = generations.started
    assert started["name"] == "preference_score"
    assert started["model"] == MODEL
    assert started["input"] == VARIABLES  # same shape as Agregado's generations
    assert started["prompt"].version == 7
    assert started["model_parameters"] == {"max_completion_tokens": 32, "enable_thinking": False}
    assert generations.updates[-1]["usage_details"] == {"input": 900, "output": 2}
    assert generations.updates[-1]["output"] == {"text": "4", "finish_reason": "stop"}


def test_an_item_without_the_operation_variables_fails_before_any_call():
    chat = FakeChat()

    with pytest.raises(ContractError):
        run(chat, FakeGenerations(), item_input={"title": "t"})
    assert chat.requests == []
