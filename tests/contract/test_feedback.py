"""SPEC §4.1 — the get_feedback contract, and §10's non-arbitrary decisions.

Delta computation lives in the server (contracts/README.md), so this is where the
rules are pinned. tests/contract/test_fixtures_roundtrip.py checks the same rules
against the frozen fixture; this file checks the edges the fixture cannot reach.
"""

from __future__ import annotations

import pytest

from tests.conftest import AGENT, HUMAN, add_cards, assert_valid, make_space, one_card

pytestmark = pytest.mark.contract

CODEX = {"actor": "codex", "actor_kind": "agent"}


@pytest.fixture
def space(client):
    make_space(client, "demo")
    return client


def feedback(client, actor="claude-code", **params):
    r = client.get("/api/spaces/demo/feedback", params={"actor": actor, **params})
    assert r.status_code == 200, r.text
    body = r.json()
    assert_valid(body, "Feedback")
    return body


# --- own-event filtering (SPEC §10) -----------------------------------------

def test_an_agent_never_reads_its_own_writes_back(space):
    card = one_card(space, "demo")
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=AGENT, json={"text": "v2"})

    fb = feedback(space, "claude-code")
    assert fb["cards_edited"] == [] and fb["cards_moved"] == []
    assert fb["summary"] == ""


def test_another_agents_writes_are_feedback(space):
    card = one_card(space, "demo", title="Option B")
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=CODEX, json={"text": "v2"})

    fb = feedback(space, "claude-code")
    assert fb["cards_edited"] == [
        {"id": card["id"], "title": "Option B", "changed": ["text"], "actor": "codex"}]


def test_cursors_are_independent_per_actor(space):
    card = one_card(space, "demo")
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"text": "v2"})

    assert len(feedback(space, "claude-code")["cards_edited"]) == 1   # consumes
    assert feedback(space, "claude-code")["cards_edited"] == []
    assert len(feedback(space, "codex")["cards_edited"]) == 1, "codex has its own cursor"


# --- annotations are cursor-independent (SPEC §10) --------------------------

def test_unresolved_annotations_come_back_every_call(space):
    card = one_card(space, "demo")
    space.post("/api/spaces/demo/annotations", params=HUMAN,
               json={"card_id": card["id"], "body": "fix the axis"})
    for _ in range(3):
        fb = feedback(space, "claude-code")
        assert [a["body"] for a in fb["annotations"]] == ["fix the axis"]


def test_resolved_annotations_disappear(space):
    card = one_card(space, "demo")
    ann = space.post("/api/spaces/demo/annotations", params=HUMAN,
                     json={"card_id": card["id"], "body": "b"}).json()
    space.patch(f"/api/spaces/demo/annotations/{ann['id']}", params=AGENT,
                json={"resolved": True, "reply": "done"})
    assert feedback(space, "claude-code")["annotations"] == []


def test_an_agent_sees_its_own_annotations_too(space):
    """Own-event filtering governs deltas, not the annotation list."""
    card = one_card(space, "demo")
    space.post("/api/spaces/demo/annotations", params=AGENT,
               json={"card_id": card["id"], "body": "self note"})
    assert [a["body"] for a in feedback(space, "claude-code")["annotations"]] == ["self note"]


def test_annotations_carry_card_title_and_staleness(space):
    card = one_card(space, "demo", title="Render time")
    space.post("/api/spaces/demo/annotations", params=HUMAN, json={
        "card_id": card["id"], "body": "b", "selector": {"type": "point", "x": .3, "y": .6}})
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=CODEX, json={"text": "v2"})

    ann = feedback(space, "claude-code")["annotations"][0]
    assert ann["card_title"] == "Render time"
    assert ann["stale"] is True
    assert ann["selector"] == {"type": "point", "x": 0.3, "y": 0.6}


def test_annotations_on_deleted_cards_still_surface(space):
    card = one_card(space, "demo", title="Option D")
    space.post("/api/spaces/demo/annotations", params=HUMAN,
               json={"card_id": card["id"], "body": "why?"})
    space.delete(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN)
    fb = feedback(space, "claude-code")
    assert [a["body"] for a in fb["annotations"]] == ["why?"]


# --- cursor mechanics --------------------------------------------------------

def test_a_fresh_actor_starts_at_zero(space):
    one_card(space, "demo", title="A")
    assert len(feedback(space, "codex")["cards_edited"]) == 0
    fb = feedback(space, "codex")
    assert fb["cursor"] == 1


def test_advance_false_does_not_consume(space):
    card = one_card(space, "demo")
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"text": "v2"})
    for _ in range(3):
        assert len(feedback(space, "claude-code", advance=False)["cards_edited"]) == 1
    assert len(feedback(space, "claude-code")["cards_edited"]) == 1
    assert feedback(space, "claude-code")["cards_edited"] == []


def test_explicit_since_overrides_the_stored_cursor(space):
    card = one_card(space, "demo")
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"text": "v2"})
    feedback(space, "claude-code")                       # consume
    assert len(feedback(space, "claude-code", since=0, advance=False)["cards_edited"]) == 1


def test_cursor_is_always_the_spaces_current_seq(space):
    one_card(space, "demo")
    one_card(space, "demo")
    assert feedback(space, "claude-code", advance=False)["cursor"] == 2
    assert feedback(space, "claude-code", since=0, advance=False)["cursor"] == 2


def test_feedback_on_an_unknown_space_is_404(client):
    assert client.get("/api/spaces/nope/feedback", params={"actor": "x"}).status_code == 404


# --- bucketing ---------------------------------------------------------------

def test_moves_are_bucketed_away_from_edits(space):
    """SPEC §4.1: 'the human dragging things around is usually noise'."""
    card = one_card(space, "demo", title="Option A")
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"x": 40, "y": 90})
    fb = feedback(space, "claude-code")
    assert fb["cards_moved"] == [{"id": card["id"], "title": "Option A", "actor": "human"}]
    assert fb["cards_edited"] == []


def test_repeated_events_on_one_card_collapse_to_one_row(space):
    card = one_card(space, "demo")
    for i in range(4):
        space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"x": i})
    assert len(feedback(space, "claude-code")["cards_moved"]) == 1


def test_changed_keys_are_unioned_across_edits(space):
    card = one_card(space, "demo")
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"text": "v2"})
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"sp_title": "New"})
    assert feedback(space, "claude-code")["cards_edited"][0]["changed"] == ["sp_title", "text"]


def test_a_deletion_supersedes_an_edit_or_a_move(space):
    """Telling an agent a card was edited and then deleted is noise; it is gone."""
    card = one_card(space, "demo", title="Option D")
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"text": "v2"})
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"x": 10})
    space.delete(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN)

    fb = feedback(space, "claude-code")
    assert [c["id"] for c in fb["cards_deleted"]] == [card["id"]]
    assert fb["cards_edited"] == [] and fb["cards_moved"] == []


def test_an_edit_supersedes_a_move(space):
    card = one_card(space, "demo")
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"x": 10})
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"text": "v2"})
    fb = feedback(space, "claude-code")
    assert len(fb["cards_edited"]) == 1
    assert fb["cards_moved"] == []


def test_a_link_created_and_removed_in_the_window_reports_as_neither(space):
    a, b = add_cards(space, "demo", [{"title": "A", "content": "a"},
                                     {"title": "B", "content": "b"}])
    link = space.post("/api/spaces/demo/links", params=HUMAN, json={
        "edges": [{"fromNode": a["id"], "toNode": b["id"], "label": "x"}]}).json()[0]
    space.delete(f"/api/spaces/demo/links/{link['id']}", params=HUMAN)
    fb = feedback(space, "claude-code")
    assert fb["links_added"] == [] and fb["links_removed"] == []


def test_links_report_endpoints_and_label(space):
    a, b = add_cards(space, "demo", [{"title": "A", "content": "a"},
                                     {"title": "B", "content": "b"}])
    link = space.post("/api/spaces/demo/links", params=HUMAN, json={
        "edges": [{"fromNode": a["id"], "toNode": b["id"], "label": "depends on"}]}).json()[0]
    assert feedback(space, "claude-code")["links_added"] == [
        {"id": link["id"], "from": a["id"], "to": b["id"], "label": "depends on",
         "actor": "human"}]


def test_link_removal_is_reported(space):
    a, b = add_cards(space, "demo", [{"title": "A", "content": "a"},
                                     {"title": "B", "content": "b"}])
    link = space.post("/api/spaces/demo/links", params=AGENT, json={
        "edges": [{"fromNode": a["id"], "toNode": b["id"]}]}).json()[0]
    feedback(space, "claude-code")                       # consume its own writes
    space.delete(f"/api/spaces/demo/links/{link['id']}", params=HUMAN)
    assert feedback(space, "claude-code")["links_removed"] == [
        {"id": link["id"], "actor": "human"}]


# --- summary -----------------------------------------------------------------
#
# Pinned by contracts/fixtures/feedback.claude-code.since-12.json:
#
#   "2 open comments (1 stale), 1 card edited, 1 deleted, 1 moved, 1 new link."
#
# Parts, in this order, omitting any that are zero, joined with ", " and closed
# with a full stop:
#   {n} open comment[s][ ({k} stale)] · {n} card[s] edited · {n} deleted
#   · {n} moved · {n} new link[s] · {n} link[s] removed
# Empty string when every bucket is empty.

def test_summary_is_empty_when_nothing_changed(space):
    assert feedback(space, "claude-code")["summary"] == ""


def test_summary_singular_and_plural(space):
    card = one_card(space, "demo")
    space.post("/api/spaces/demo/annotations", params=HUMAN,
               json={"card_id": card["id"], "body": "one"})
    assert feedback(space, "claude-code", advance=False)["summary"] == "1 open comment."

    space.post("/api/spaces/demo/annotations", params=HUMAN,
               json={"card_id": card["id"], "body": "two"})
    assert feedback(space, "claude-code", advance=False)["summary"] == "2 open comments."


def test_summary_counts_stale(space):
    card = one_card(space, "demo")
    space.post("/api/spaces/demo/annotations", params=HUMAN,
               json={"card_id": card["id"], "body": "one"})
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=CODEX, json={"text": "v2"})
    assert feedback(space, "claude-code", advance=False)["summary"] == (
        "1 open comment (1 stale), 1 card edited.")


def test_summary_reproduces_the_fixture_grammar(space):
    a, b, c, d = add_cards(space, "demo", [{"title": t, "content": t} for t in "ABCD"])
    feedback(space, "claude-code")                       # consume own creations

    space.post("/api/spaces/demo/annotations", params=HUMAN, json={"card_id": a["id"], "body": "1"})
    space.post("/api/spaces/demo/annotations", params=HUMAN, json={"card_id": b["id"], "body": "2"})
    space.patch(f"/api/spaces/demo/cards/{b['id']}", params=HUMAN, json={"text": "v2"})
    space.delete(f"/api/spaces/demo/cards/{d['id']}", params=HUMAN)
    space.patch(f"/api/spaces/demo/cards/{a['id']}", params=HUMAN, json={"x": 1})
    space.post("/api/spaces/demo/links", params=HUMAN, json={
        "edges": [{"fromNode": a["id"], "toNode": c["id"], "label": "depends on"}]})

    assert feedback(space, "claude-code")["summary"] == (
        "2 open comments (1 stale), 1 card edited, 1 deleted, 1 moved, 1 new link.")


def test_summary_reports_removed_links(space):
    a, b = add_cards(space, "demo", [{"title": "A", "content": "a"},
                                     {"title": "B", "content": "b"}])
    link = space.post("/api/spaces/demo/links", params=AGENT,
                      json={"edges": [{"fromNode": a["id"], "toNode": b["id"]}]}).json()[0]
    feedback(space, "claude-code")
    space.delete(f"/api/spaces/demo/links/{link['id']}", params=HUMAN)
    assert feedback(space, "claude-code")["summary"] == "1 link removed."
