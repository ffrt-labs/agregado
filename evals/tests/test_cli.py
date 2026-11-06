"""The command line refuses to start without an item cap, so an experiment
can't run the whole dataset and use up the day's 10k neurons."""

import pytest

from agregado_evals.cli import main, parse_args


def test_label_and_item_cap_are_enough_with_preference_score_and_score_golden_as_defaults():
    args = parse_args(["--label", "staging", "--max-items", "5"])

    assert (args.operation, args.dataset, args.label, args.max_items) == (
        "preference_score",
        "score-golden",
        "staging",
        5,
    )


@pytest.mark.parametrize(
    "argv",
    [
        ["--label", "staging"],
        ["--label", "staging", "--max-items="],  # make eval with ITEMS unset
        ["--label", "staging", "--max-items", "0"],
        ["--label", "staging", "--max-items", "-3"],
        ["--label", "staging", "--max-items", "all"],
        ["--max-items", "5"],
        ["--label", "", "--max-items", "5"],  # make eval with LABEL unset
        ["--label", "staging", "--max-items", "5", "--operation", "digest_select"],
    ],
)
def test_it_refuses_to_start_without_a_valid_cap_label_and_operation(argv, capsys):
    with pytest.raises(SystemExit) as exit:
        parse_args(argv)

    assert exit.value.code == 2


def test_missing_cap_is_refused_before_credentials_are_read(monkeypatch, capsys):
    for name in ("LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY", "LANGFUSE_BASE_URL"):
        monkeypatch.delenv(name, raising=False)

    with pytest.raises(SystemExit):
        main(["--label", "staging"])

    assert "--max-items" in capsys.readouterr().err


class FakeLangfuse:
    """Langfuse client stand-in: serves one prompt and one dataset, records
    the experiment it is asked to run."""

    def __init__(self, user_template, items):
        from types import SimpleNamespace

        from langfuse.api import Prompt_Chat
        from langfuse.model import ChatPromptClient

        self.prompt = ChatPromptClient(
            Prompt_Chat(
                name="agregado/preference_score", version=4,
                prompt=[{"role": "system", "content": "s"}, {"role": "user", "content": user_template}],
                config={}, labels=["staging"], tags=[],
            )
        )
        self.dataset = SimpleNamespace(items=items)
        self.experiments = []

    def get_prompt(self, name, label, **kwargs):
        assert (name, label) == ("agregado/preference_score", "staging")
        return self.prompt

    def get_dataset(self, name):
        assert name == "score-golden"
        return self.dataset

    def start_as_current_observation(self, **kwargs):
        raise AssertionError("no call expected in this test")

    def run_experiment(self, **kwargs):
        from types import SimpleNamespace

        self.experiments.append(kwargs)
        return SimpleNamespace(format=lambda: "", experiment_url=None, item_results=kwargs["data"])

    def flush(self):
        pass


def dataset_item(id, **variables):
    from types import SimpleNamespace

    return SimpleNamespace(id=id, status="ACTIVE", input=variables or {"preferences": "p", "title": "t", "content": "c"})


@pytest.fixture
def credentials(monkeypatch):
    for name in ("LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY", "LANGFUSE_BASE_URL", "CLOUDFLARE_ACCOUNT_ID", "CLOUDFLARE_API_TOKEN"):
        monkeypatch.setenv(name, "x")


def use(monkeypatch, fake):
    import langfuse

    monkeypatch.setattr(langfuse, "get_client", lambda: fake)


def test_the_experiment_runs_the_capped_items_with_both_evaluators_and_the_rates(monkeypatch, credentials):
    fake = FakeLangfuse("{{preferences}} {{title}} {{content}}", [dataset_item(str(i)) for i in range(10)])
    use(monkeypatch, fake)

    assert main(["--label", "staging", "--max-items", "3"]) == 0

    (experiment,) = fake.experiments
    assert [i.id for i in experiment["data"]] == ["0", "1", "2"]
    assert [e.__name__ for e in experiment["evaluators"]] == ["score_parses", "score_agrees"]
    assert [e.__name__ for e in experiment["run_evaluators"]] == ["rates"]
    assert experiment["metadata"]["prompt_version"] == "4"


def test_a_prompt_version_go_would_reject_runs_nothing(monkeypatch, credentials, capsys):
    fake = FakeLangfuse("{{preferences}} {{title}} {{content}} {{author}}", [dataset_item("a")])
    use(monkeypatch, fake)

    assert main(["--label", "staging", "--max-items", "3"]) == 1

    assert fake.experiments == []
    assert "unknown: author" in capsys.readouterr().err


def test_items_that_are_not_recorded_variables_run_nothing(monkeypatch, credentials, capsys):
    fake = FakeLangfuse("{{preferences}} {{title}} {{content}}", [dataset_item("a"), dataset_item("b", title="t")])
    use(monkeypatch, fake)

    assert main(["--label", "staging", "--max-items", "3"]) == 1

    assert fake.experiments == []
    assert "b: " in capsys.readouterr().err


def test_missing_credentials_are_named_before_anything_is_fetched(monkeypatch, capsys):
    for name in ("LANGFUSE_PUBLIC_KEY", "LANGFUSE_SECRET_KEY", "LANGFUSE_BASE_URL", "CLOUDFLARE_ACCOUNT_ID", "CLOUDFLARE_API_TOKEN"):
        monkeypatch.delenv(name, raising=False)
    monkeypatch.setenv("LANGFUSE_PUBLIC_KEY", "pk-lf-test")

    assert main(["--label", "staging", "--max-items", "3"]) == 2

    err = capsys.readouterr().err
    assert "LANGFUSE_SECRET_KEY" in err and "CLOUDFLARE_API_TOKEN" in err
    assert "LANGFUSE_PUBLIC_KEY" not in err
