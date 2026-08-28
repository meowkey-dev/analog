"""SPEC §2.3 selectors and staleness, §3 annotation endpoints."""

from __future__ import annotations

import pytest

from tests.conftest import AGENT, HUMAN, assert_valid, make_space, one_card

pytestmark = pytest.mark.contract

POINT = {"type": "point", "x": 0.34, "y": 0.71}
RECT = {"type": "rect", "x": 0.1, "y": 0.2, "w": 0.3, "h": 0.25}


@pytest.fixture
def space(client):
    make_space(client, "demo")
    return client, one_card(client, "demo", title="Option B")


def create(client, card_id, **kw):
    r = client.post("/api/spaces/demo/annotations", params=HUMAN,
                    json={"card_id": card_id, **kw})
    assert r.status_code == 201, r.text
    return r.json()


@pytest.mark.parametrize("selector", [None, POINT, RECT], ids=["whole-card", "point", "rect"])
def test_all_v1_selectors_round_trip(space, selector):
    client, card = space
    ann = create(client, card["id"], body="look", selector=selector)
    assert_valid(ann, "Annotation")
    assert ann["selector"] == selector
    assert ann["id"].startswith("a_")


def test_selector_defaults_to_the_whole_card(space):
    client, card = space
    assert create(client, card["id"], body="b")["selector"] is None


def test_motivation_defaults_to_commenting(space):
    client, card = space
    assert create(client, card["id"], body="b")["motivation"] == "commenting"


@pytest.mark.parametrize("motivation", ["commenting", "assessing", "editing"])
def test_motivations(space, motivation):
    client, card = space
    assert create(client, card["id"], body="b", motivation=motivation)["motivation"] == motivation


def test_unknown_motivation_is_rejected(space):
    client, card = space
    r = client.post("/api/spaces/demo/annotations", params=HUMAN,
                    json={"card_id": card["id"], "body": "b", "motivation": "praising"})
    assert r.status_code == 400


def test_creation_records_the_creator_and_the_cards_rev(space):
    client, card = space
    client.patch(f"/api/spaces/demo/cards/{card['id']}", params=AGENT, json={"text": "v2"})
    ann = create(client, card["id"], body="b")
    assert (ann["creator"], ann["creator_kind"]) == ("human", "human")
    assert ann["card_rev"] == 2
    assert ann["stale"] is False
    assert ann["card_title"] == "Option B"
    assert ann["resolved"] is False and ann["resolved_reply"] is None


def test_agents_can_annotate_too(space):
    client, card = space
    r = client.post("/api/spaces/demo/annotations", params=AGENT,
                    json={"card_id": card["id"], "body": "note to self"})
    assert r.json()["creator_kind"] == "agent"
    assert r.json()["creator"] == "claude-code"


def test_annotating_an_unknown_card_is_404(space):
    client, _ = space
    r = client.post("/api/spaces/demo/annotations", params=HUMAN,
                    json={"card_id": "c_nope", "body": "b"})
    assert r.status_code == 404


# --- staleness (SPEC §2.3) ---------------------------------------------------

def test_an_edit_makes_the_annotation_stale(space):
    client, card = space
    ann = create(client, card["id"], body="b", selector=POINT)
    assert ann["stale"] is False
    client.patch(f"/api/spaces/demo/cards/{card['id']}", params=AGENT, json={"text": "rewritten"})
    assert client.get("/api/spaces/demo/annotations").json()[0]["stale"] is True


def test_a_move_does_not_make_the_annotation_stale(space):
    """schema.sql note 2: fractions survive a resize, so a move must not stale a pin."""
    client, card = space
    create(client, card["id"], body="b", selector=RECT)
    client.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                 json={"x": 900, "y": 900, "width": 640, "height": 480})
    assert client.get("/api/spaces/demo/annotations").json()[0]["stale"] is False


def test_stale_annotations_are_not_auto_resolved(space):
    """SPEC §10: only the human can tell 'fixed it' from 'rewrote around it'."""
    client, card = space
    create(client, card["id"], body="b")
    client.patch(f"/api/spaces/demo/cards/{card['id']}", params=AGENT, json={"text": "v2"})
    ann = client.get("/api/spaces/demo/annotations").json()[0]
    assert ann["stale"] is True and ann["resolved"] is False
    assert client.get("/api/spaces/demo").json()["counts"]["open_annotations"] == 1


# --- resolve -----------------------------------------------------------------

def test_resolve_with_a_reply(space):
    client, card = space
    ann = create(client, card["id"], body="y-axis starts at 40")
    r = client.patch(f"/api/spaces/demo/annotations/{ann['id']}", params=AGENT,
                     json={"resolved": True, "reply": "rebased axis at 0"})
    assert r.status_code == 200, r.text
    assert r.json()["resolved"] is True
    assert r.json()["resolved_reply"] == "rebased axis at 0"
    ev = client.get("/api/spaces/demo/events").json()["events"][-1]
    assert ev["type"] == "annotation.resolved"
    assert ev["payload"]["reply"] == "rebased axis at 0"


def test_resolved_defaults_to_true(space):
    """The CLI and MCP surfaces only ever resolve; `resolve` with a reply is the norm."""
    client, card = space
    ann = create(client, card["id"], body="b")
    r = client.patch(f"/api/spaces/demo/annotations/{ann['id']}", params=AGENT,
                     json={"reply": "done"})
    assert r.json()["resolved"] is True


def test_reopening_emits_no_resolve_event(space):
    client, card = space
    ann = create(client, card["id"], body="b")
    client.patch(f"/api/spaces/demo/annotations/{ann['id']}", params=AGENT, json={"resolved": True})
    before = len(client.get("/api/spaces/demo/events").json()["events"])
    r = client.patch(f"/api/spaces/demo/annotations/{ann['id']}", params=HUMAN,
                     json={"resolved": False})
    assert r.json()["resolved"] is False
    after = client.get("/api/spaces/demo/events").json()["events"]
    assert len(after) == before, "there is no annotation.reopened event type"


def test_resolving_an_unknown_annotation_is_404(space):
    client, _ = space
    assert client.patch("/api/spaces/demo/annotations/a_nope", params=AGENT,
                        json={"resolved": True}).status_code == 404


def test_list_order_is_creation_order(space):
    client, card = space
    ids = [create(client, card["id"], body=str(i))["id"] for i in range(3)]
    assert [a["id"] for a in client.get("/api/spaces/demo/annotations").json()] == ids
