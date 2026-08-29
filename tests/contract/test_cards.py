"""SPEC §3 card endpoints, §2.1 node shape, §5 auto-layout."""

from __future__ import annotations

import pytest

from tests.conftest import AGENT, HUMAN, add_cards, assert_valid, make_space, one_card

pytestmark = pytest.mark.contract

# SPEC §5, and DECISIONS.md on why a batch wraps at all. Stated here rather than
# imported: this suite describes the behaviour, so reading the constant out of the
# implementation would let the two drift together and still pass.
LAYOUT_MAX_COLUMN = 900


@pytest.fixture
def space(client):
    make_space(client, "demo")
    return client


# --- creation ----------------------------------------------------------------

def test_drafts_become_json_canvas_text_nodes(space):
    node = add_cards(space, "demo", [
        {"title": "Option E", "content": "## E", "kind": "md"}])[0]
    assert_valid(node, "Node")
    assert node["id"].startswith("c_")
    assert node["type"] == "text"
    assert node["text"] == "## E"
    assert node["sp_kind"] == "md"
    assert node["sp_title"] == "Option E"
    assert node["sp_created_by"] == "claude-code"
    assert node["sp_rev"] == 1
    assert "sp_deleted_at" not in node


def test_kind_defaults_to_md(space):
    assert add_cards(space, "demo", [{"title": "T", "content": "c"}])[0]["sp_kind"] == "md"


@pytest.mark.parametrize("kind", ["md", "html", "svg", "plain"])
def test_all_kinds_accepted(space, kind):
    assert add_cards(space, "demo", [{"title": kind, "content": "x", "kind": kind}])[0]["sp_kind"] == kind


def test_unknown_kind_is_rejected(space):
    r = space.post("/api/spaces/demo/cards", params=AGENT,
                   json={"cards": [{"title": "T", "content": "c", "kind": "pdf"}]})
    assert r.status_code == 400
    assert r.json()["error"] in ("unsupported_kind", "validation_failed")


def test_bulk_create_returns_nodes_in_request_order(space):
    nodes = add_cards(space, "demo", [{"title": t, "content": t} for t in "ABCD"])
    assert [n["sp_title"] for n in nodes] == list("ABCD")
    assert len({n["id"] for n in nodes}) == 4


def test_meta_is_stored_verbatim(space):
    meta = {"source": "run-17", "nested": {"n": [1, 2]}}
    assert add_cards(space, "demo", [
        {"title": "T", "content": "c", "meta": meta}])[0]["sp_meta"] == meta


def test_raw_nodes_are_accepted_and_reattributed(space):
    r = space.post("/api/spaces/demo/cards", params=AGENT, json={"nodes": [{
        "id": "c_client_chosen", "type": "text", "x": 5, "y": 6, "width": 100,
        "height": 50, "text": "raw", "sp_kind": "plain", "sp_title": "Raw",
        "sp_created_by": "someone-else", "sp_rev": 99}]})
    assert r.status_code == 201, r.text
    node = r.json()[0]
    assert node["id"] != "c_client_chosen", "clients must not choose ids"
    assert (node["x"], node["y"], node["width"], node["height"]) == (5, 6, 100, 50)
    assert node["sp_created_by"] == "claude-code", "attribution comes from actor"
    assert node["sp_rev"] == 1


def test_creating_in_an_unknown_space_is_404(client):
    r = client.post("/api/spaces/nope/cards", params=AGENT,
                    json={"cards": [{"title": "T", "content": "c"}]})
    assert r.status_code == 404


# --- auto-layout (SPEC §5) ---------------------------------------------------

def test_first_card_lands_at_the_origin(space):
    node = one_card(space, "demo")
    assert (node["x"], node["y"]) == (0, 0)
    assert node["width"] > 0 and node["height"] > 0


def test_omitted_geometry_goes_right_of_the_bounding_box_top_down(space):
    first = add_cards(space, "demo", [
        {"title": "pinned", "content": "c", "x": 0, "y": 0, "width": 320, "height": 200}])[0]
    a, b = add_cards(space, "demo", [{"title": "a", "content": "a"},
                                     {"title": "b", "content": "b"}])
    right_edge = first["x"] + first["width"]
    assert a["x"] >= right_edge and b["x"] >= right_edge
    assert a["x"] == b["x"], "a batch stacks in one column"
    assert b["y"] >= a["y"] + a["height"], "top-down, no overlap"


def test_explicit_geometry_wins(space):
    node = add_cards(space, "demo", [
        {"title": "T", "content": "c", "x": -40, "y": 12, "width": 999, "height": 111}])[0]
    assert (node["x"], node["y"], node["width"], node["height"]) == (-40, 12, 999, 111)


def test_layout_ignores_deleted_cards(space):
    doomed = add_cards(space, "demo", [
        {"title": "far", "content": "c", "x": 5000, "y": 0, "width": 320, "height": 200}])[0]
    space.delete(f"/api/spaces/demo/cards/{doomed['id']}", params=HUMAN)
    assert one_card(space, "demo")["x"] < 5000


# --- patch: moved vs updated (schema.sql implementer notes 1 and 2) ----------

def test_geometry_only_patch_moves_without_bumping_rev(space):
    card = one_card(space, "demo")
    r = space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                    json={"x": 100, "y": 200})
    assert r.status_code == 200, r.text
    assert r.json()["sp_rev"] == 1
    assert (r.json()["x"], r.json()["y"]) == (100, 200)
    assert space.get("/api/spaces/demo/events").json()["events"][-1]["type"] == "card.moved"


def test_resize_is_a_move_not_an_edit(space):
    """Normalized selectors survive a resize, so a resize must not stale annotations."""
    card = one_card(space, "demo")
    r = space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                    json={"width": 640, "height": 400})
    assert r.json()["sp_rev"] == 1
    assert space.get("/api/spaces/demo/events").json()["events"][-1]["type"] == "card.moved"


def test_a_no_op_move_still_emits_one_move_event(space):
    """Fixture event 15 moves c_opt_a from [0,0] to [0,0]: classification is by the
    keys in the patch, not by whether the values differ."""
    card = one_card(space, "demo")
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                json={"x": card["x"], "y": card["y"]})
    ev = space.get("/api/spaces/demo/events").json()["events"][-1]
    assert ev["type"] == "card.moved"
    assert ev["payload"]["from"] == [card["x"], card["y"]]
    assert ev["payload"]["to"] == [card["x"], card["y"]]


def test_content_patch_updates_and_bumps_rev(space):
    card = one_card(space, "demo")
    r = space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                    json={"text": "rewritten"})
    assert r.json()["sp_rev"] == 2
    assert r.json()["text"] == "rewritten"
    ev = space.get("/api/spaces/demo/events").json()["events"][-1]
    assert ev["type"] == "card.updated"
    assert ev["payload"]["changed"] == ["text"]
    assert ev["payload"]["rev"] == 2


@pytest.mark.parametrize("patch", [
    {"text": "x"}, {"sp_title": "New"}, {"sp_kind": "plain"}, {"sp_meta": {"a": 1}},
    {"color": "4"},
])
def test_non_geometry_patches_are_edits(space, patch):
    card = one_card(space, "demo")
    r = space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json=patch)
    assert r.json()["sp_rev"] == 2, patch
    assert space.get("/api/spaces/demo/events").json()["events"][-1]["type"] == "card.updated"


def test_mixed_patch_is_an_edit(space):
    card = one_card(space, "demo")
    r = space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                    json={"x": 50, "text": "new"})
    assert r.json()["sp_rev"] == 2
    ev = space.get("/api/spaces/demo/events").json()["events"][-1]
    assert ev["type"] == "card.updated"
    assert ev["payload"]["changed"] == ["text", "x"]


def test_patch_preserves_unmentioned_keys(space):
    card = add_cards(space, "demo", [
        {"title": "Keep", "content": "c", "meta": {"k": 1}}])[0]
    patched = space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                          json={"text": "new"}).json()
    assert patched["sp_title"] == "Keep"
    assert patched["sp_meta"] == {"k": 1}
    assert patched["sp_created_by"] == "claude-code", "the original author is not overwritten"


def test_patch_cannot_change_the_id(space):
    card = one_card(space, "demo")
    patched = space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                          json={"id": "c_hijack", "text": "x"}).json()
    assert patched["id"] == card["id"]


def test_empty_patch_is_rejected(space):
    card = one_card(space, "demo")
    r = space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={})
    assert r.status_code == 400
    assert r.json()["error"] == "validation_failed"


def test_patching_an_unknown_card_is_404(space):
    assert space.patch("/api/spaces/demo/cards/c_nope", params=HUMAN,
                       json={"text": "x"}).status_code == 404


# --- If-Match (SPEC §3) ------------------------------------------------------

def test_if_match_on_the_current_rev_succeeds(space):
    card = one_card(space, "demo")
    r = space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                    headers={"If-Match": "1"}, json={"text": "x"})
    assert r.status_code == 200
    assert r.json()["sp_rev"] == 2


def test_if_match_mismatch_is_409_with_the_current_node(space):
    card = one_card(space, "demo")
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"text": "first"})
    r = space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                    headers={"If-Match": "1"}, json={"text": "second"})
    assert r.status_code == 409
    body = r.json()
    assert body["error"] == "conflict"
    assert body["current"]["sp_rev"] == 2
    assert body["current"]["text"] == "first", "the losing write must not have applied"
    assert_valid(body["current"], "Node")


def test_absent_if_match_is_last_write_wins(space):
    card = one_card(space, "demo")
    space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN, json={"text": "first"})
    r = space.patch(f"/api/spaces/demo/cards/{card['id']}", params=AGENT, json={"text": "second"})
    assert r.status_code == 200
    assert r.json()["text"] == "second"


# --- delete (SPEC §2.2 soft delete) ------------------------------------------

def test_delete_is_soft(space):
    card = one_card(space, "demo", title="Option D")
    assert space.delete(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN).status_code == 204

    live = space.get("/api/spaces/demo/canvas").json()["nodes"]
    assert card["id"] not in {n["id"] for n in live}

    kept = space.get("/api/spaces/demo/canvas", params={"include_deleted": True}).json()["nodes"]
    tombstone = next(n for n in kept if n["id"] == card["id"])
    assert tombstone["sp_deleted_at"]

    ev = space.get("/api/spaces/demo/events").json()["events"][-1]
    assert ev["type"] == "card.deleted"
    assert ev["payload"]["title"] == "Option D", "the agent needs the title, the card is gone"


def test_deleting_twice_is_404(space):
    card = one_card(space, "demo")
    space.delete(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN)
    assert space.delete(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN).status_code == 404


def test_patching_a_deleted_card_is_404(space):
    card = one_card(space, "demo")
    space.delete(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN)
    assert space.patch(f"/api/spaces/demo/cards/{card['id']}", params=HUMAN,
                       json={"text": "x"}).status_code == 404


def test_deleting_an_unknown_card_is_404(space):
    assert space.delete("/api/spaces/demo/cards/c_nope", params=HUMAN).status_code == 404


def test_a_long_batch_wraps_into_a_new_column(space):
    """SPEC §5 says a column; five cards of one is a strip you have to zoom out to
    read, so a batch wraps once the column passes LAYOUT_MAX_COLUMN."""
    nodes = add_cards(space, "demo", [
        {"title": str(i), "content": "c", "height": 200, "width": 320} for i in range(6)])
    columns: dict[float, list[dict]] = {}
    for node in nodes:
        columns.setdefault(node["x"], []).append(node)

    assert len(columns) > 1, "six cards must not be one column"
    for x, column in columns.items():
        top = min(n["y"] for n in column)
        bottom = max(n["y"] + n["height"] for n in column)
        assert bottom - top <= LAYOUT_MAX_COLUMN + 200, f"column at {x} is too tall"
    for column in columns.values():
        ys = sorted(n["y"] for n in column)
        assert ys == [n["y"] for n in column], "each column is still top-down"
    assert len({n["y"] for n in nodes}) < len(nodes), "columns reuse the same rows"


def test_wrapping_columns_do_not_overlap(space):
    nodes = add_cards(space, "demo", [
        {"title": str(i), "content": "c", "height": 200, "width": 320} for i in range(8)])
    boxes = [(n["x"], n["y"], n["x"] + n["width"], n["y"] + n["height"]) for n in nodes]
    for i, a in enumerate(boxes):
        for b in boxes[i + 1:]:
            overlaps = a[0] < b[2] and b[0] < a[2] and a[1] < b[3] and b[1] < a[3]
            assert not overlaps, f"{a} overlaps {b}"
