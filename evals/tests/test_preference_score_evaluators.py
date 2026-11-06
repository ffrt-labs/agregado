"""The preference_score evaluators score one item's output the way Agregado
reads it in production (internal/ai parseScore), then against the owner's
annotation on the dataset item."""

import pytest

from agregado_evals.preference_score import score_agrees, score_parses


def output(text, finish_reason="stop"):
    return {"text": text, "finish_reason": finish_reason}


@pytest.mark.parametrize(
    "text, expected",
    [
        ("4", 1),
        ("  5\n", 1),
        ("1", 1),
        ("0", 0),
        ("6", 0),
        ("4.5", 0),
        ("Score: 4", 0),
        ("1_0", 0),
        ("", 0),
    ],
)
def test_score_parses_accepts_only_an_integer_from_1_to_5(text, expected):
    evaluation = score_parses(input={}, output=output(text), expected_output=None)

    assert evaluation.name == "score_parses"
    assert evaluation.data_type == "BOOLEAN"
    assert evaluation.value == expected


def test_truncated_answer_does_not_parse_even_if_its_text_would():
    # Production treats finish_reason "length" as a failed call: no Score.
    evaluation = score_parses(input={}, output=output("4", "length"), expected_output=None)

    assert evaluation.value == 0


@pytest.mark.parametrize("label", [4, "4", " 4 ", {"score": 4}, {"score": "4"}])
def test_score_agrees_when_the_answer_matches_the_annotation(label):
    evaluation = score_agrees(input={}, output=output("4"), expected_output=label)

    assert evaluation.name == "score_agrees"
    assert evaluation.data_type == "BOOLEAN"
    assert evaluation.value == 1


def test_score_disagrees_when_the_answer_differs_from_the_annotation():
    evaluation = score_agrees(input={}, output=output("2"), expected_output=4)

    assert evaluation.value == 0
    assert evaluation.comment == "answered 2, annotated 4"


def test_an_unparsable_answer_disagrees_with_the_annotation():
    evaluation = score_agrees(input={}, output=output("four"), expected_output=4)

    assert evaluation.value == 0


@pytest.mark.parametrize("label", [None, {}, "", "n/a", 9, True, {"summary": "x"}])
def test_an_item_without_a_usable_annotation_gets_no_agreement_score(label):
    assert score_agrees(input={}, output=output("4"), expected_output=label) == []
