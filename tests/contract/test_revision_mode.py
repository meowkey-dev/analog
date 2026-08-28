"""SPEC §2.4 — replace and branch.

Note the event count in branch mode. A branching PATCH is not one mutation: it
creates a card and an auto-link, so it emits card.created + link.created. What it
must NOT do is write the superseded card's content again — schema.sql note 4 says
its rev freezes, which is the whole reason branch mode never produces stale pins.
"""

from __future__ import annotations

import pytest

from tests.conftest import AGENT, HUMAN, make_space, one_card

pytestmark = pytest.mark.contract


def canvas(client, slug="demo", **params):
    return client.get(f"/api/spaces/{slug}/canvas", params=params).json()


def node(client, slug, node_id, **params):
    return next(n for n in canvas(client, slug, **params)["nodes"] if n["id"] == node_id)


# --- replace (default) -------------------------------------------------------

def test_replace_mutates_in_place(client):
    make_space(client, "demo")
    card = one_card(client, "demo", content="v1")

    updated = client.patch(f"/api/spaces/demo/cards/{card['id']}", params=AGENT,
                           json={"text": "v2"}).json()
    assert updated["id"] == card["id"]
    assert updated["text"] == "v2"
    assert updated["sp_rev"] == 2
    assert "sp_superseded_by" not in updated
    assert len(canvas(client)["nodes"]) == 1
    assert canvas(client)["edges"] == []


def test_replace_keeps_the_old_content_only_in_the_event_log(client):
    make_space(client, "demo")
    card = one_card(client, "demo", content="v1")
    client.patch(f"/api/spaces/demo/cards/{card['id']}", params=AGENT, json={"text": "v2"})
    ev = client.get("/api/spaces/demo/events").json()["events"][-1]
    assert ev["type"] == "card.updated"
    assert ev["payload"]["changed"] == ["text"]


# --- branch ------------------------------------------------------------------

@pytest.fixture
def branched(client):
    """A branch-mode space with one card, revised once."""
    make_space(client, "demo", revision_mode="branch")
    old = one_card(client, "demo", title="Chart", content="v1", kind="svg")
    new = client.patch(f"/api/spaces/demo/cards/{old['id']}", params=AGENT,
                       json={"text": "v2"})
    assert new.status_code == 200, new.text
    return client, old, new.json()


def test_branch_returns_the_new_card(branched):
    client, old, new = branched
    assert new["id"] != old["id"]
    assert new["text"] == "v2"
    assert new["sp_rev"] == 1, "a fresh card starts at rev 1"
    assert new["sp_title"] == "Chart", "inherited from the card it revises"
    assert new["sp_kind"] == "svg"
    assert "sp_superseded_by" not in new


def test_branch_marks_the_old_card_and_freezes_it(branched):
    client, old, new = branched
    stub = node(client, "demo", old["id"])
    assert stub["sp_superseded_by"] == new["id"]
    assert stub["text"] == "v1", "the old content stays readable"
    assert stub["sp_rev"] == old["sp_rev"], "the supersede pointer must not bump rev"


def test_branch_keeps_both_cards_visible(branched):
    client, old, new = branched
    assert {n["id"] for n in canvas(client)["nodes"]} == {old["id"], new["id"]}


def test_branch_does_not_stack_the_new_card_on_the_old_one(branched):
    client, old, new = branched
    assert (new["x"], new["y"]) != (old["x"], old["y"])


def test_branch_auto_links_old_to_new_with_label_revised(branched):
    client, old, new = branched
    edges = canvas(client)["edges"]
    assert len(edges) == 1
    assert edges[0]["fromNode"] == old["id"]
    assert edges[0]["toNode"] == new["id"]
    assert edges[0]["label"] == "revised"


def test_branch_emits_a_create_and_a_link_and_nothing_else(branched):
    client, old, new = branched
    types = [e["type"] for e in client.get("/api/spaces/demo/events").json()["events"]]
    assert types == ["card.created", "card.created", "link.created"]
    assert "card.updated" not in types, "the superseded card is never written again"


def test_branch_reports_as_a_new_card_not_an_edit(branched):
    """A different agent sees a creation, so it will not think its card was rewritten."""
    client, old, new = branched
    fb = client.get("/api/spaces/demo/feedback", params={"actor": "codex"}).json()
    assert fb["cards_edited"] == []
    assert [l["label"] for l in fb["links_added"]] == ["revised"]


# --- annotations across a branch (SPEC §2.4) --------------------------------

def test_annotations_stay_on_the_card_they_were_made_on(client):
    make_space(client, "demo", revision_mode="branch")
    old = one_card(client, "demo", title="Chart", content="v1")
    ann = client.post("/api/spaces/demo/annotations", params=HUMAN, json={
        "card_id": old["id"], "body": "y-axis starts at 40",
        "selector": {"type": "point", "x": 0.3, "y": 0.6}}).json()
    new = client.patch(f"/api/spaces/demo/cards/{old['id']}", params=AGENT,
                       json={"text": "v2"}).json()

    kept = client.get("/api/spaces/demo/annotations").json()
    assert [a["card_id"] for a in kept] == [old["id"]], "not copied forward"
    assert kept[0]["id"] == ann["id"]


def test_branch_mode_never_produces_a_stale_annotation(client):
    make_space(client, "demo", revision_mode="branch")
    old = one_card(client, "demo", content="v1")
    client.post("/api/spaces/demo/annotations", params=HUMAN,
                json={"card_id": old["id"], "body": "b"})
    for i in range(3):
        old = client.patch(f"/api/spaces/demo/cards/{old['id']}", params=AGENT,
                           json={"text": f"v{i + 2}"}).json()
    assert all(a["stale"] is False for a in client.get("/api/spaces/demo/annotations").json())


def test_feedback_still_delivers_annotations_on_superseded_cards(client):
    make_space(client, "demo", revision_mode="branch")
    old = one_card(client, "demo", title="Chart", content="v1")
    client.post("/api/spaces/demo/annotations", params=HUMAN,
                json={"card_id": old["id"], "body": "fix the axis"})
    client.patch(f"/api/spaces/demo/cards/{old['id']}", params=AGENT, json={"text": "v2"})

    fb = client.get("/api/spaces/demo/feedback", params={"actor": "claude-code"}).json()
    assert [a["body"] for a in fb["annotations"]] == ["fix the axis"]
    assert fb["annotations"][0]["stale"] is False


# --- per-call override -------------------------------------------------------

def test_mode_query_overrides_a_replace_space(client):
    make_space(client, "demo")
    old = one_card(client, "demo", content="v1")
    new = client.patch(f"/api/spaces/demo/cards/{old['id']}", params={**AGENT, "mode": "branch"},
                       json={"text": "v2"}).json()
    assert new["id"] != old["id"]
    assert node(client, "demo", old["id"])["sp_superseded_by"] == new["id"]


def test_mode_query_overrides_a_branch_space(client):
    make_space(client, "demo", revision_mode="branch")
    old = one_card(client, "demo", content="v1")
    same = client.patch(f"/api/spaces/demo/cards/{old['id']}",
                        params={**AGENT, "mode": "replace"}, json={"text": "v2"}).json()
    assert same["id"] == old["id"]
    assert same["sp_rev"] == 2
    assert len(canvas(client)["nodes"]) == 1


def test_an_unknown_mode_is_rejected(client):
    make_space(client, "demo")
    card = one_card(client, "demo")
    r = client.patch(f"/api/spaces/demo/cards/{card['id']}", params={**AGENT, "mode": "merge"},
                     json={"text": "v2"})
    assert r.status_code == 400


def test_a_move_never_branches(client):
    """Dragging a card is not a content revision, whatever the space's mode is."""
    make_space(client, "demo", revision_mode="branch")
    card = one_card(client, "demo")
    moved = client.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                         json={"x": 400, "y": 50}).json()
    assert moved["id"] == card["id"]
    assert len(canvas(client)["nodes"]) == 1
    assert client.get("/api/spaces/demo/events").json()["events"][-1]["type"] == "card.moved"


def test_if_match_still_applies_in_branch_mode(client):
    make_space(client, "demo", revision_mode="branch")
    card = one_card(client, "demo")
    r = client.patch(f"/api/spaces/demo/cards/{card['id']}", params=AGENT,
                     headers={"If-Match": "7"}, json={"text": "v2"})
    assert r.status_code == 409


def test_branching_a_superseded_card_is_rejected(client):
    """The chain has one head; revising a frozen card would fork it."""
    make_space(client, "demo", revision_mode="branch")
    old = one_card(client, "demo", content="v1")
    client.patch(f"/api/spaces/demo/cards/{old['id']}", params=AGENT, json={"text": "v2"})
    r = client.patch(f"/api/spaces/demo/cards/{old['id']}", params=AGENT, json={"text": "v3"})
    assert r.status_code == 409
    assert r.json()["error"] == "conflict"
