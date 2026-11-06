"""Before spending neurons, the harness checks that the fetched prompt and the
dataset items fit what Agregado's Go compiler accepts (internal/ai Compile and
the Langfuse prompt client): exactly a system and a user message, no
placeholders, and the operation's variables, all used and none extra. The
Langfuse SDK would leave an unknown {{variable}} in the text and say nothing,
so a result from a prompt Go would reject must not be trusted."""

import pytest

from agregado_evals.contract import ContractError, check_item_input, check_prompt

VARIABLES = frozenset({"preferences", "title", "content"})


def message(role, content):
    return {"type": "message", "role": role, "content": content}


GOOD = [
    message("system", "You give scores."),
    message("user", "PREFERENCES.md:\n{{preferences}}\n\nTitle: {{ title }}\n\nContent: {{content}}"),
]


def test_a_prompt_go_would_compile_passes():
    check_prompt(GOOD, VARIABLES)


@pytest.mark.parametrize(
    "messages, reason",
    [
        ([message("user", "{{preferences}} {{title}} {{content}}")], "system and a user message"),
        ([GOOD[1], GOOD[0]], "system and a user message"),
        ([GOOD[0], GOOD[1], message("assistant", "ok")], "system and a user message"),
        ([GOOD[0], {"type": "placeholder", "name": "history"}], "system and a user message"),
        ([GOOD[0], message("user", "{{preferences}} {{title}}")], "unused: content"),
        ([GOOD[0], message("user", "{{preferences}} {{title}} {{content}} {{author}}")], "unknown: author"),
    ],
)
def test_a_prompt_go_would_reject_is_refused(messages, reason):
    with pytest.raises(ContractError, match=reason):
        check_prompt(messages, VARIABLES)


def test_a_variable_used_in_the_system_message_counts():
    messages = [message("system", "Profile: {{preferences}}"), message("user", "{{title}} {{content}}")]

    check_prompt(messages, VARIABLES)


def test_braces_around_a_non_identifier_are_literal_text_as_in_go():
    messages = [message("system", "Answer like {{ this one }}."), GOOD[1]]

    check_prompt(messages, VARIABLES)


def test_an_item_recorded_with_the_operation_variables_passes():
    check_item_input({"preferences": "p", "title": "t", "content": "c"}, VARIABLES)


@pytest.mark.parametrize(
    "item_input, reason",
    [
        ({"preferences": "p", "title": "t"}, "missing: content"),
        ({"preferences": "p", "title": "t", "content": "c", "slugs": "x"}, "unexpected: slugs"),
        ([{"role": "user", "content": "hi"}], "not a variables object"),
        ("hello", "not a variables object"),
    ],
)
def test_an_item_that_is_not_the_recorded_variables_is_refused(item_input, reason):
    with pytest.raises(ContractError, match=reason):
        check_item_input(item_input, VARIABLES)
