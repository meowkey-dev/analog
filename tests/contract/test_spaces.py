"""SPEC §3 space endpoints."""

from __future__ import annotations

import pytest

from tests.conftest import AGENT, HUMAN, assert_valid, make_space, one_card

pytestmark = pytest.mark.contract


def test_create_returns_201_and_a_valid_space(client):
    space = make_space(client, "redesign", "Nav redesign")
    assert_valid(space, "Space")
    assert space["slug"] == "redesign"
    assert space["title"] == "Nav redesign"
    assert space["revision_mode"] == "replace"     # SPEC §2.4 default
    assert space["seq"] == 1, "seq 1 is the space.created event"
    assert space["id"].startswith("s_")


def test_create_accepts_branch_mode(client):
    assert make_space(client, "b", "B", revision_mode="branch")["revision_mode"] == "branch"


def test_duplicate_slug_is_409(client):
    make_space(client, "dup")
    r = client.post("/api/spaces", params=HUMAN, json={"slug": "dup", "title": "again"})
    assert r.status_code == 409
    assert r.json()["error"] == "conflict"


@pytest.mark.parametrize("slug", ["Has-Caps", "has space", "has_underscore", "", "a" * 65, "café"])
def test_invalid_slugs_are_rejected(client, slug):
    r = client.post("/api/spaces", params=HUMAN, json={"slug": slug, "title": "T"})
    assert r.status_code == 400, f"{slug!r} was accepted"
    assert r.json()["error"] == "validation_failed"


def test_get_unknown_space_is_404(client):
    r = client.get("/api/spaces/nope")
    assert r.status_code == 404
    assert r.json()["error"] == "not_found"


def test_counts_track_live_rows_only(client):
    make_space(client, "counts")
    a, b = client.post("/api/spaces/counts/cards", params=AGENT, json={"cards": [
        {"title": "A", "content": "a"}, {"title": "B", "content": "b"}]}).json()
    client.post("/api/spaces/counts/links", params=AGENT, json={
        "edges": [{"fromNode": a["id"], "toNode": b["id"], "label": "x"}]})
    ann = client.post("/api/spaces/counts/annotations", params=HUMAN, json={
        "card_id": a["id"], "body": "look"}).json()

    assert client.get("/api/spaces/counts").json()["counts"] == {
        "cards": 2, "links": 1, "open_annotations": 1}

    client.delete(f"/api/spaces/counts/cards/{b['id']}", params=HUMAN)
    client.patch(f"/api/spaces/counts/annotations/{ann['id']}", params=AGENT,
                 json={"resolved": True})
    assert client.get("/api/spaces/counts").json()["counts"] == {
        "cards": 1, "links": 1, "open_annotations": 0}


def test_patch_updates_title_and_revision_mode(client):
    make_space(client, "p")
    r = client.patch("/api/spaces/p", params=HUMAN,
                     json={"title": "New", "revision_mode": "branch"})
    assert r.status_code == 200
    assert r.json()["title"] == "New"
    assert client.get("/api/spaces/p").json()["revision_mode"] == "branch"


def test_space_created_names_the_space(client):
    """SPEC §3 / AMENDMENTS #5: a space's log opens with its own creation."""
    make_space(client, "born", "Born")
    event = client.get("/api/spaces/born/events").json()["events"][0]
    assert (event["seq"], event["type"]) == (1, "space.created")
    assert event["payload"] == {"slug": "born", "title": "Born"}
    assert (event["actor"], event["actor_kind"]) == ("human", "human")


def test_delete_removes_the_space_and_its_contents(client):
    make_space(client, "gone")
    one_card(client, "gone")
    assert client.delete("/api/spaces/gone", params=HUMAN).status_code == 204
    assert client.get("/api/spaces/gone").status_code == 404
    assert client.get("/api/spaces").json() == []


def test_space_seq_is_the_event_counter(client):
    make_space(client, "seq")
    assert client.get("/api/spaces/seq").json()["seq"] == 1     # space.created
    one_card(client, "seq")
    assert client.get("/api/spaces/seq").json()["seq"] == 2
    one_card(client, "seq")
    assert client.get("/api/spaces/seq").json()["seq"] == 3


def test_seq_is_per_space(client):
    make_space(client, "one")
    make_space(client, "two")
    one_card(client, "one")
    one_card(client, "one")
    one_card(client, "two")
    assert client.get("/api/spaces/one").json()["seq"] == 3     # created + 2 cards
    assert client.get("/api/spaces/two").json()["seq"] == 2     # created + 1 card
    log = client.get("/api/spaces/two/events").json()["events"]
    assert [(e["seq"], e["type"]) for e in log] == [
        (1, "space.created"), (2, "card.created")]
