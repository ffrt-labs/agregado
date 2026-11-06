"""The cap counts the items that will actually cost neurons: active ones."""

from types import SimpleNamespace

from agregado_evals.harness import select_items


def item(id, status="ACTIVE"):
    return SimpleNamespace(id=id, status=status)


def test_takes_at_most_the_cap_skipping_archived_items():
    items = [item("a"), item("b", "ARCHIVED"), item("c"), item("d"), item("e")]

    assert [i.id for i in select_items(items, 3)] == ["a", "c", "d"]


def test_a_cap_above_the_dataset_size_takes_every_active_item():
    assert [i.id for i in select_items([item("a"), item("b")], 10)] == ["a", "b"]
