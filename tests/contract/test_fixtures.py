"""The fixtures are valid, and they exercise what contracts/README.md claims.

These assertions are the reason the fixtures can be trusted by WP2b/WP3/WP4 with no
server running. If one fails, the amendment process in contracts/README.md applies.
"""

from __future__ import annotations

import pytest

from tests.conftest import assert_valid, fixture

pytestmark = pytest.mark.contract

CANVAS = fixture("canvas.json")
WITH_DELETED = fixture("canvas.with-deleted.json")
ANNOTATIONS = fixture("annotations.json")
EVENTS = fixture("events.json")
FEEDBACK = fixture("feedback.claude-code.since-12.json")
SPACE = fixture("space.json")


# --- shape -------------------------------------------------------------------

def test_space_matches_schema():
    assert_valid(SPACE, "Space")


@pytest.mark.parametrize("doc", [CANVAS, WITH_DELETED], ids=["live", "with-deleted"])
def test_canvas_matches_schema(doc):
    assert_valid(doc, "Canvas")


def test_canvas_is_valid_json_canvas_10():
    """SPEC §2.1: the wire format is JSON Canvas 1.0, validated by openjsoncanvas."""
    import openjsoncanvas

    canvas = openjsoncanvas.Canvas.from_dict(CANVAS)
    assert {n.id for n in canvas.nodes} == {n["id"] for n in CANVAS["nodes"]}
    assert {e.id for e in canvas.edges} == {e["id"] for e in CANVAS["edges"]}


def test_annotations_match_schema():
    assert_valid(ANNOTATIONS, "Annotation", many=True)


def test_events_match_schema():
    assert_valid(EVENTS["events"], "Event", many=True)


def test_feedback_matches_schema():
    assert_valid(FEEDBACK, "Feedback")


# --- the counts contracts/README.md advertises -------------------------------

def test_fixture_inventory():
    assert len(CANVAS["nodes"]) == 6
    assert len(CANVAS["edges"]) == 4
    assert len(ANNOTATIONS) == 3
    assert len(EVENTS["events"]) == 19
    assert SPACE["counts"] == {"cards": 6, "links": 4, "open_annotations": 2}


def test_event_seqs_are_contiguous_from_one():
    seqs = [e["seq"] for e in EVENTS["events"]]
    assert seqs == list(range(1, 20))
    assert EVENTS["cursor"] == seqs[-1] == SPACE["seq"]


def test_every_event_subject_exists():
    known = ({n["id"] for n in WITH_DELETED["nodes"]}
             | {e["id"] for e in WITH_DELETED["edges"]}
             | {a["id"] for a in ANNOTATIONS})
    assert {e["subject_id"] for e in EVENTS["events"]} <= known


# --- the rules the fixtures exist to pin (contracts/README.md) ---------------

def test_own_events_are_filtered_from_the_authors_feedback():
    own = [e for e in EVENTS["events"] if e["seq"] > 12 and e["actor"] == "claude-code"]
    assert {e["seq"] for e in own} == {18, 19}
    reported = (
        {c["id"] for c in FEEDBACK["cards_edited"]}
        | {c["id"] for c in FEEDBACK["cards_deleted"]}
        | {c["id"] for c in FEEDBACK["cards_moved"]}
        | {l["id"] for l in FEEDBACK["links_added"]}
    )
    assert not reported & {e["subject_id"] for e in own}
    assert all(row["actor"] != "claude-code"
               for bucket in ("cards_edited", "cards_deleted", "cards_moved",
                              "links_added", "links_removed")
               for row in FEEDBACK[bucket])


def test_unresolved_annotations_ignore_the_cursor():
    """a_1 was created at seq 12 — at or before the cursor — and still appears."""
    created_at_seq = {e["subject_id"]: e["seq"] for e in EVENTS["events"]
                      if e["type"] == "annotation.created"}
    assert created_at_seq["a_1"] == 12
    assert "a_1" in {a["id"] for a in FEEDBACK["annotations"]}


def test_resolved_annotations_are_excluded():
    assert next(a for a in ANNOTATIONS if a["id"] == "a_3")["resolved"] is True
    assert "a_3" not in {a["id"] for a in FEEDBACK["annotations"]}


def test_staleness_is_card_rev_less_than_current_rev():
    rev = {n["id"]: n.get("sp_rev", 1) for n in WITH_DELETED["nodes"]}
    for ann in ANNOTATIONS:
        assert ann["stale"] == (ann["card_rev"] < rev[ann["card_id"]]), ann["id"]
    stale = {a["id"] for a in ANNOTATIONS if a["stale"]}
    assert stale == {"a_1"}


def test_moved_is_not_edited():
    """Event 15 moved c_opt_a without bumping rev; it must not land in cards_edited."""
    moved = next(e for e in EVENTS["events"] if e["type"] == "card.moved")
    assert moved["seq"] == 15 and moved["subject_id"] == "c_opt_a"
    assert "rev" not in (moved.get("payload") or {})
    assert {c["id"] for c in FEEDBACK["cards_moved"]} == {"c_opt_a"}
    assert "c_opt_a" not in {c["id"] for c in FEEDBACK["cards_edited"]}


def test_soft_delete_keeps_the_card_visible_to_agents():
    live = {n["id"] for n in CANVAS["nodes"]}
    all_nodes = {n["id"] for n in WITH_DELETED["nodes"]}
    assert all_nodes - live == {"c_opt_d"}
    deleted = next(n for n in WITH_DELETED["nodes"] if n["id"] == "c_opt_d")
    assert deleted["sp_deleted_at"], "include_deleted must expose the tombstone"
    assert {c["id"] for c in FEEDBACK["cards_deleted"]} == {"c_opt_d"}


def test_all_four_render_paths_are_present():
    """SPEC §5 card rendering: md, svg, html text nodes plus a file node."""
    kinds = {n["id"]: n.get("sp_kind") for n in CANVAS["nodes"]}
    assert {"md", "svg", "html"} <= set(kinds.values())
    files = [n for n in CANVAS["nodes"] if n["type"] == "file"]
    assert len(files) == 1 and files[0]["file"].startswith("/api/spaces/redesign/media/")
    assert files[0].get("sp_kind") is None, "sp_kind is meaningful only on text nodes"


def test_html_card_carries_a_script_for_the_sandbox_test():
    """SPEC §5: it must run inside the iframe and never in the parent frame."""
    html = next(n for n in CANVAS["nodes"] if n.get("sp_kind") == "html")
    assert "<script>" in html["text"]


def test_selectors_cover_both_v1_shapes():
    selectors = [a["selector"] for a in ANNOTATIONS]
    assert None in selectors
    assert {s["type"] for s in selectors if s} == {"point", "rect"}
    for s in selectors:
        if s:
            assert all(0.0 <= s[k] <= 1.0 for k in s if k != "type")


def test_deltas_agree_with_the_event_log_after_seq_12():
    """The fixture feedback is exactly what a correct implementation would compute."""
    after = [e for e in EVENTS["events"] if e["seq"] > 12 and e["actor"] != "claude-code"]
    by_type: dict[str, set[str]] = {}
    for e in after:
        by_type.setdefault(e["type"], set()).add(e["subject_id"])
    assert by_type.get("card.updated", set()) == {c["id"] for c in FEEDBACK["cards_edited"]}
    assert by_type.get("card.deleted", set()) == {c["id"] for c in FEEDBACK["cards_deleted"]}
    assert by_type.get("card.moved", set()) == {c["id"] for c in FEEDBACK["cards_moved"]}
    assert by_type.get("link.created", set()) == {l["id"] for l in FEEDBACK["links_added"]}
    assert by_type.get("link.deleted", set()) == {l["id"] for l in FEEDBACK["links_removed"]}
    assert FEEDBACK["cursor"] == EVENTS["cursor"]
