"""A seeded database must reproduce contracts/fixtures/ through the API, exactly.

This is the strongest test in the suite: it pins response *shape* (no stray keys),
ordering, soft-delete projection, cursor seeding and delta computation in one go.
"""

from __future__ import annotations

import pytest

from tests.conftest import assert_valid, fixture

pytestmark = pytest.mark.contract


def test_space_matches_fixture(seeded):
    r = seeded.get("/api/spaces/redesign")
    assert r.status_code == 200, r.text
    assert r.json() == fixture("space.json")


def test_space_list_contains_only_the_seeded_space(seeded):
    r = seeded.get("/api/spaces")
    assert r.status_code == 200
    assert r.json() == [fixture("space.json")]


def test_canvas_matches_fixture(seeded):
    r = seeded.get("/api/spaces/redesign/canvas")
    assert r.status_code == 200, r.text
    assert r.json() == fixture("canvas.json")


def test_canvas_include_deleted_matches_fixture(seeded):
    r = seeded.get("/api/spaces/redesign/canvas", params={"include_deleted": True})
    assert r.json() == fixture("canvas.with-deleted.json")


def test_live_canvas_never_leaks_the_tombstone(seeded):
    nodes = seeded.get("/api/spaces/redesign/canvas").json()["nodes"]
    assert all("sp_deleted_at" not in n for n in nodes)


def test_annotations_match_fixture(seeded):
    r = seeded.get("/api/spaces/redesign/annotations")
    assert r.json() == fixture("annotations.json")


def test_annotation_filters(seeded):
    open_ = seeded.get("/api/spaces/redesign/annotations", params={"resolved": False}).json()
    assert [a["id"] for a in open_] == ["a_1", "a_2"]
    done = seeded.get("/api/spaces/redesign/annotations", params={"resolved": True}).json()
    assert [a["id"] for a in done] == ["a_3"]
    on_chart = seeded.get("/api/spaces/redesign/annotations",
                          params={"card_id": "c_chart"}).json()
    assert [a["id"] for a in on_chart] == ["a_1"]


def test_events_match_fixture(seeded):
    r = seeded.get("/api/spaces/redesign/events")
    assert r.json() == fixture("events.json")


def test_feedback_matches_fixture_without_an_explicit_since(seeded):
    """The seed parks claude-code's cursor at 12, so the default call is the
    `since=12` fixture. This is the §4.1 contract end to end."""
    r = seeded.get("/api/spaces/redesign/feedback",
                   params={"actor": "claude-code", "advance": False})
    assert r.status_code == 200, r.text
    assert r.json() == fixture("feedback.claude-code.since-12.json")


def test_feedback_matches_fixture_with_an_explicit_since(seeded):
    r = seeded.get("/api/spaces/redesign/feedback",
                   params={"actor": "claude-code", "since": 12, "advance": False})
    assert r.json() == fixture("feedback.claude-code.since-12.json")


def test_advance_consumes_the_cursor(seeded):
    first = seeded.get("/api/spaces/redesign/feedback", params={"actor": "claude-code"})
    assert first.json() == fixture("feedback.claude-code.since-12.json")

    second = seeded.get("/api/spaces/redesign/feedback", params={"actor": "claude-code"}).json()
    assert second["cursor"] == 19
    assert second["cards_edited"] == second["cards_deleted"] == []
    assert second["cards_moved"] == second["links_added"] == second["links_removed"] == []
    # Annotations are cursor-independent: they come back every single time.
    assert [a["id"] for a in second["annotations"]] == ["a_1", "a_2"]
    assert second["summary"] == "2 open comments (1 stale)."


def test_peeking_does_not_consume(seeded):
    for _ in range(3):
        r = seeded.get("/api/spaces/redesign/feedback",
                       params={"actor": "claude-code", "advance": False})
        assert r.json() == fixture("feedback.claude-code.since-12.json")


def test_an_unknown_actor_starts_at_zero_and_sees_everything(seeded):
    fb = seeded.get("/api/spaces/redesign/feedback",
                    params={"actor": "codex", "advance": False}).json()
    assert {c["id"] for c in fb["cards_edited"]} == {"c_opt_b", "c_chart"}
    assert {c["id"] for c in fb["cards_deleted"]} == {"c_opt_d"}
    assert {l["id"] for l in fb["links_added"]} == {"l_1", "l_2", "l_3", "l_4"}


def test_the_media_referenced_by_the_file_node_is_served(seeded):
    node = next(n for n in seeded.get("/api/spaces/redesign/canvas").json()["nodes"]
                if n["type"] == "file")
    r = seeded.get(node["file"])
    assert r.status_code == 200, f"{node['file']} is unreachable"
    assert r.headers["content-type"].startswith("image/png")
    assert r.content[:8] == b"\x89PNG\r\n\x1a\n"


@pytest.mark.parametrize("path,schema,many", [
    ("/api/spaces/redesign", "Space", False),
    ("/api/spaces/redesign/canvas", "Canvas", False),
    ("/api/spaces/redesign/annotations", "Annotation", True),
])
def test_seeded_responses_validate(seeded, path, schema, many):
    assert_valid(seeded.get(path).json(), schema, many=many)
