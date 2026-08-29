"""SPEC §3 / §10 — import is additive only. There is no whole-canvas replace."""

from __future__ import annotations

import pytest

from tests.conftest import AGENT, HUMAN, add_cards, fixture, make_space, one_card

pytestmark = pytest.mark.contract

CANVAS = fixture("canvas.json")


@pytest.fixture
def space(client):
    make_space(client, "demo")
    return client


def do_import(client, canvas, actor=None):
    r = client.post("/api/spaces/demo/import", params=actor or AGENT, json=canvas)
    assert r.status_code == 201, r.text
    return r.json()


def test_import_creates_cards_and_links(space):
    result = do_import(space, CANVAS)
    canvas = space.get("/api/spaces/demo/canvas").json()
    assert len(canvas["nodes"]) == 6
    assert len(canvas["edges"]) == 4
    assert set(result["id_map"]) == ({n["id"] for n in CANVAS["nodes"]}
                                     | {e["id"] for e in CANVAS["edges"]})


def test_ids_are_remapped(space):
    result = do_import(space, CANVAS)
    live = {n["id"] for n in space.get("/api/spaces/demo/canvas").json()["nodes"]}
    assert live.isdisjoint({n["id"] for n in CANVAS["nodes"]})
    assert live == {result["id_map"][n["id"]] for n in CANVAS["nodes"]}


def test_edges_are_rewired_to_the_new_ids(space):
    result = do_import(space, CANVAS)
    id_map = result["id_map"]
    edges = {e["id"]: e for e in space.get("/api/spaces/demo/canvas").json()["edges"]}
    for original in CANVAS["edges"]:
        edge = edges[id_map[original["id"]]]
        assert edge["fromNode"] == id_map[original["fromNode"]]
        assert edge["toNode"] == id_map[original["toNode"]]
        assert edge.get("label") == original.get("label")


def test_content_and_geometry_survive_the_round_trip(space):
    result = do_import(space, CANVAS)
    nodes = {n["id"]: n for n in space.get("/api/spaces/demo/canvas").json()["nodes"]}
    for original in CANVAS["nodes"]:
        got = nodes[result["id_map"][original["id"]]]
        for key in ("type", "x", "y", "width", "height", "text", "file",
                    "sp_kind", "sp_title"):
            if key in original:
                assert got.get(key) == original[key], f"{original['id']}.{key}"


def test_import_reattributes_and_resets_rev(space):
    result = do_import(space, CANVAS)
    for node in space.get("/api/spaces/demo/canvas").json()["nodes"]:
        assert node["sp_created_by"] == "claude-code"
        assert node["sp_rev"] == 1


def test_import_never_deletes(space):
    existing = one_card(space, "demo", title="Mine")
    do_import(space, CANVAS)
    live = {n["id"] for n in space.get("/api/spaces/demo/canvas").json()["nodes"]}
    assert existing["id"] in live
    assert len(live) == 7


def test_importing_twice_duplicates_rather_than_merging(space):
    do_import(space, CANVAS)
    do_import(space, CANVAS)
    assert len(space.get("/api/spaces/demo/canvas").json()["nodes"]) == 12


def test_import_emits_one_event_per_item(space):
    do_import(space, CANVAS)
    types = [e["type"] for e in space.get("/api/spaces/demo/events").json()["events"]]
    assert types.count("card.created") == 6
    assert types.count("link.created") == 4
    assert len(types) == 11, "plus the space's own creation"


def test_an_edge_to_an_unknown_node_is_rejected_atomically(space):
    bad = {"nodes": CANVAS["nodes"][:1],
           "edges": [{"id": "l_x", "fromNode": CANVAS["nodes"][0]["id"], "toNode": "c_absent"}]}
    r = space.post("/api/spaces/demo/import", params=AGENT, json=bad)
    assert r.status_code in (400, 404)
    assert space.get("/api/spaces/demo/canvas").json()["nodes"] == []


def test_an_edge_may_reference_a_card_already_in_the_space(space):
    a, b = add_cards(space, "demo", [{"title": "A", "content": "a"},
                                     {"title": "B", "content": "b"}])
    result = do_import(space, {"nodes": [], "edges": [
        {"id": "l_x", "fromNode": a["id"], "toNode": b["id"], "label": "joins"}]})
    edges = space.get("/api/spaces/demo/canvas").json()["edges"]
    assert len(edges) == 1
    assert edges[0]["id"] == result["id_map"]["l_x"]


def test_empty_import_is_a_no_op(space):
    before = space.get("/api/spaces/demo/events").json()["events"]
    result = do_import(space, {"nodes": [], "edges": []})
    assert result["id_map"] == {}
    assert space.get("/api/spaces/demo/events").json()["events"] == before


def test_export_import_round_trips_through_a_second_space(client):
    """`analog export | analog import` is the .canvas round trip in SPEC §4.2."""
    make_space(client, "demo")
    make_space(client, "copy")
    add_cards(client, "demo", [{"title": "A", "content": "a"}, {"title": "B", "content": "b"}])
    exported = client.get("/api/spaces/demo/canvas").json()

    r = client.post("/api/spaces/copy/import", params=AGENT, json=exported)
    assert r.status_code == 201
    copied = client.get("/api/spaces/copy/canvas").json()
    assert [n["sp_title"] for n in copied["nodes"]] == ["A", "B"]
    assert [n["text"] for n in copied["nodes"]] == ["a", "b"]
