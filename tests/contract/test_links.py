"""SPEC §3 link endpoints."""

from __future__ import annotations

import pytest

from tests.conftest import AGENT, HUMAN, add_cards, assert_valid, make_space

pytestmark = pytest.mark.contract


@pytest.fixture
def space(client):
    make_space(client, "demo")
    a, b = add_cards(client, "demo", [{"title": "A", "content": "a"},
                                      {"title": "B", "content": "b"}])
    return client, a["id"], b["id"]


def test_create_returns_json_canvas_edges(space):
    client, a, b = space
    r = client.post("/api/spaces/demo/links", params=AGENT, json={"edges": [
        {"fromNode": a, "toNode": b, "fromSide": "right", "toSide": "left",
         "label": "contradicts"}]})
    assert r.status_code == 201, r.text
    edge = r.json()[0]
    assert_valid(edge, "Edge")
    assert edge["id"].startswith("l_")
    assert (edge["fromNode"], edge["toNode"]) == (a, b)
    assert edge["label"] == "contradicts"
    assert edge["sp_created_by"] == "claude-code"


def test_bulk_create(space):
    client, a, b = space
    edges = client.post("/api/spaces/demo/links", params=AGENT, json={"edges": [
        {"fromNode": a, "toNode": b, "label": "one"},
        {"fromNode": b, "toNode": a, "label": "two"}]}).json()
    assert [e["label"] for e in edges] == ["one", "two"]
    assert len({e["id"] for e in edges}) == 2


def test_links_appear_on_the_canvas(space):
    client, a, b = space
    edge = client.post("/api/spaces/demo/links", params=AGENT,
                       json={"edges": [{"fromNode": a, "toNode": b}]}).json()[0]
    assert client.get("/api/spaces/demo/canvas").json()["edges"] == [edge]


@pytest.mark.parametrize("bad", ["from", "to"])
def test_dangling_endpoints_are_404(space, bad):
    client, a, b = space
    edge = {"fromNode": "c_nope" if bad == "from" else a,
            "toNode": "c_nope" if bad == "to" else b}
    r = client.post("/api/spaces/demo/links", params=AGENT, json={"edges": [edge]})
    assert r.status_code == 404
    assert r.json()["error"] == "not_found"


def test_a_link_to_a_deleted_card_is_404(space):
    client, a, b = space
    client.delete(f"/api/spaces/demo/cards/{b}", params=HUMAN)
    r = client.post("/api/spaces/demo/links", params=AGENT,
                    json={"edges": [{"fromNode": a, "toNode": b}]})
    assert r.status_code == 404


def test_a_rejected_batch_creates_nothing(space):
    client, a, b = space
    client.post("/api/spaces/demo/links", params=AGENT, json={"edges": [
        {"fromNode": a, "toNode": b, "label": "good"},
        {"fromNode": a, "toNode": "c_nope", "label": "bad"}]})
    assert client.get("/api/spaces/demo/canvas").json()["edges"] == []
    assert not [e for e in client.get("/api/spaces/demo/events").json()["events"]
                if e["type"] == "link.created"]


def test_delete_removes_the_link_from_the_canvas(space):
    client, a, b = space
    edge = client.post("/api/spaces/demo/links", params=AGENT,
                       json={"edges": [{"fromNode": a, "toNode": b}]}).json()[0]
    assert client.delete(f"/api/spaces/demo/links/{edge['id']}", params=HUMAN).status_code == 204
    assert client.get("/api/spaces/demo/canvas").json()["edges"] == []
    assert client.get("/api/spaces/demo/events").json()["events"][-1]["type"] == "link.deleted"


def test_deleting_twice_is_404(space):
    client, a, b = space
    edge = client.post("/api/spaces/demo/links", params=AGENT,
                       json={"edges": [{"fromNode": a, "toNode": b}]}).json()[0]
    client.delete(f"/api/spaces/demo/links/{edge['id']}", params=HUMAN)
    assert client.delete(f"/api/spaces/demo/links/{edge['id']}", params=HUMAN).status_code == 404


def test_deleting_a_card_leaves_its_links_alone(space):
    """Soft delete means the edge still has both endpoints on the deleted canvas."""
    client, a, b = space
    edge = client.post("/api/spaces/demo/links", params=AGENT,
                       json={"edges": [{"fromNode": a, "toNode": b}]}).json()[0]
    client.delete(f"/api/spaces/demo/cards/{a}", params=HUMAN)
    full = client.get("/api/spaces/demo/canvas", params={"include_deleted": True}).json()
    assert edge["id"] in {e["id"] for e in full["edges"]}


def test_deleted_links_are_absent_even_with_include_deleted(space):
    """include_deleted is a card-tombstone flag (node-only sp_deleted_at). A
    deleted link has no wire shape marking it, so returning one would give the
    client an edge indistinguishable from a live one."""
    client, a, b = space
    edge = client.post("/api/spaces/demo/links", params=AGENT,
                       json={"edges": [{"fromNode": a, "toNode": b}]}).json()[0]
    assert client.delete(f"/api/spaces/demo/links/{edge['id']}", params=HUMAN).status_code == 204
    full = client.get("/api/spaces/demo/canvas", params={"include_deleted": True}).json()
    assert edge["id"] not in {e["id"] for e in full["edges"]}
