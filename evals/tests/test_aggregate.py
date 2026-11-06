"""Run-level evaluators turn the per-item scores into one rate per score name,
attached to the experiment run."""

from langfuse import Evaluation
from langfuse.experiment import ExperimentItemResult

from agregado_evals.evaluators import rates


def item(*evaluations):
    return ExperimentItemResult(item={"input": {}}, output=None, evaluations=list(evaluations), trace_id=None)


def boolean(name, value):
    return Evaluation(name=name, value=value, data_type="BOOLEAN")


def test_rates_average_each_boolean_score_over_the_items_that_have_it():
    results = [
        item(boolean("score_parses", 1), boolean("score_agrees", 1)),
        item(boolean("score_parses", 1), boolean("score_agrees", 0)),
        item(boolean("score_parses", 0)),  # unannotated: no agreement score
        item(boolean("score_parses", 1), boolean("score_agrees", 1)),
    ]

    by_name = {e.name: e for e in rates(item_results=results)}

    assert by_name["score_parses_rate"].value == 0.75
    assert by_name["score_parses_rate"].comment == "3 of 4 items"
    assert by_name["score_agrees_rate"].value == 2 / 3
    assert by_name["score_agrees_rate"].comment == "2 of 3 items"


def test_no_items_means_no_rates():
    assert rates(item_results=[]) == []
