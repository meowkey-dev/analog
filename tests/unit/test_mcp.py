"""WP2b: all 10 tools callable, get_feedback returns the §4.1 shape, mocked client."""

from __future__ import annotations

import asyncio

import pytest

from mcp_server.server import create_server
from tests.conftest import assert_valid, fixture

CANVAS = fixture("canvas.json")
SPACE = fixture("space.json")
ANNOTATIONS = fixture("annotations.json")
FEEDBACK = fixture("feedback.claude-code.since-12.json")

SPEC_TOOLS = {"list_spaces", "create_space", "read_space", "add_cards", "update_card",
              "delete_card", "link_cards", "get_feedback", "resolve_annotation",
              "await_feedback"}


class MockClient:
    """Records calls; returns fixtures. WP2b depends on WP2a, not on WP1."""

    def __init__(self):
        self.calls: list[tuple] = []
        self.feedback_queue: list[dict] = []

    def _record(self, name, *args, **kw):
        self.calls.append((name, args, kw))

    def list_spaces(self):
        self._record("list_spaces")
        return [SPACE]

    def create_space(self, slug, title, revision_mode="replace"):
        self._record("create_space", slug, title, revision_mode)
        return {**SPACE, "slug": slug, "title": title, "revision_mode": revision_mode}

    def get_space(self, slug):
        self._record("get_space", slug)
        return SPACE

    def get_canvas(self, slug, include_deleted=False):
        self._record("get_canvas", slug, include_deleted)
        return CANVAS

    def list_annotations(self, slug, *, resolved=None, card_id=None):
        self._record("list_annotations", slug, resolved, card_id)
        return [a for a in ANNOTATIONS if resolved is None or a["resolved"] == resolved]

    def create_cards(self, slug, cards):
        self._record("create_cards", slug, cards)
        return [{**CANVAS["nodes"][0], "sp_title": c.get("title", ""),
                 "text": c.get("content", ""), "sp_kind": c.get("kind", "md")}
                for c in cards]

    def update_card(self, slug, card_id, patch, *, mode=None, if_match=None):
        self._record("update_card", slug, card_id, patch, mode, if_match)
        return {**CANVAS["nodes"][0], **patch, "sp_rev": 3}

    def delete_card(self, slug, card_id):
        self._record("delete_card", slug, card_id)

    def link_cards(self, slug, from_id, to_id, label=None):
        self._record("link_cards", slug, from_id, to_id, label)
        return {"id": "l_9", "fromNode": from_id, "toNode": to_id, "label": label}

    def get_feedback(self, slug, *, since=None, advance=True):
        self._record("get_feedback", slug, since, advance)
        if self.feedback_queue:
            # Successive states; the last one repeats forever.
            return (self.feedback_queue.pop(0) if len(self.feedback_queue) > 1
                    else self.feedback_queue[0])
        return FEEDBACK

    def resolve_annotation(self, slug, annotation_id, *, reply=None, resolved=True):
        self._record("resolve_annotation", slug, annotation_id, reply)
        return {**ANNOTATIONS[0], "id": annotation_id, "resolved": True,
                "resolved_reply": reply}

    def find_annotation(self, annotation_id):
        self._record("find_annotation", annotation_id)
        return "redesign", ANNOTATIONS[0]


@pytest.fixture
def mock():
    return MockClient()


@pytest.fixture
def server(mock):
    return create_server(mock)


def call(server, name, **arguments):
    result = asyncio.run(server.call_tool(name, arguments))
    return result.structured_content if hasattr(result, "structured_content") else result


def data(server, name, **arguments):
    """The tool's return value, unwrapped from the MCP envelope."""
    structured = call(server, name, **arguments)
    return structured.get("result", structured) if isinstance(structured, dict) else structured


# --- inventory ---------------------------------------------------------------

def test_exactly_the_ten_tools_in_spec_41(server):
    names = {t.name for t in asyncio.run(server.list_tools())}
    assert names == SPEC_TOOLS


def test_every_tool_has_a_description(server):
    for tool in asyncio.run(server.list_tools()):
        assert (tool.description or "").strip(), tool.name


# --- each tool ---------------------------------------------------------------

def test_list_spaces(server):
    assert data(server, "list_spaces") == [SPACE]


def test_create_space(server, mock):
    result = data(server, "create_space", slug="demo", title="Demo")
    assert result["slug"] == "demo"
    assert mock.calls[-1][1] == ("demo", "Demo", "replace")


def test_create_space_accepts_branch(server, mock):
    data(server, "create_space", slug="demo", title="D", revision_mode="branch")
    assert mock.calls[-1][1][2] == "branch"


def test_read_space_returns_nodes_edges_and_open_annotations(server):
    result = data(server, "read_space", slug="redesign")
    assert result["nodes"] == CANVAS["nodes"]
    assert result["edges"] == CANVAS["edges"]
    assert [a["id"] for a in result["annotations"]] == ["a_1", "a_2"], "open only"
    assert result["space"]["slug"] == "redesign"


def test_add_cards_takes_friendly_drafts(server, mock):
    """SPEC §4.1: agents shouldn't have to compute geometry."""
    result = data(server, "add_cards", slug="redesign", cards=[
        {"title": "Option E", "content": "## E", "kind": "md"},
        {"title": "Chart", "content": "<svg/>", "kind": "svg"}])
    assert [n["sp_title"] for n in result] == ["Option E", "Chart"]
    sent = mock.calls[-1][1][1]
    assert all("x" not in c and "y" not in c for c in sent), "no geometry required"


def test_add_cards_passes_geometry_when_given(server, mock):
    data(server, "add_cards", slug="redesign",
         cards=[{"title": "T", "content": "c", "x": 10, "y": 20}])
    assert mock.calls[-1][1][1][0]["x"] == 10


def test_update_card(server, mock):
    result = data(server, "update_card", slug="redesign", card_id="c_chart",
                  patch={"text": "<svg/>"})
    assert result["text"] == "<svg/>"
    assert mock.calls[-1][1][:3] == ("redesign", "c_chart", {"text": "<svg/>"})


def test_update_card_forwards_mode_and_if_match(server, mock):
    data(server, "update_card", slug="redesign", card_id="c_chart",
         patch={"text": "x"}, mode="branch", if_match=2)
    assert mock.calls[-1][1][3:] == ("branch", 2)


def test_delete_card(server, mock):
    assert data(server, "delete_card", slug="redesign", card_id="c_opt_d") == {
        "deleted": "c_opt_d"}
    assert mock.calls[-1][0] == "delete_card"


def test_link_cards(server, mock):
    result = data(server, "link_cards", slug="redesign", from_card="c_opt_b",
                  to_card="c_opt_d", label="contradicts")
    assert result["label"] == "contradicts"
    assert mock.calls[-1][1] == ("redesign", "c_opt_b", "c_opt_d", "contradicts")


def test_resolve_annotation_without_a_slug_looks_it_up(server, mock):
    result = data(server, "resolve_annotation", annotation_id="a_1",
                  reply="rebased axis at 0")
    assert result["resolved"] is True
    assert result["resolved_reply"] == "rebased axis at 0"
    assert [c[0] for c in mock.calls] == ["find_annotation", "resolve_annotation"]


def test_resolve_annotation_with_a_slug_skips_the_lookup(server, mock):
    data(server, "resolve_annotation", annotation_id="a_1", slug="redesign")
    assert [c[0] for c in mock.calls] == ["resolve_annotation"]


# --- get_feedback (the contract) --------------------------------------------

def test_get_feedback_returns_the_spec_41_shape(server):
    result = data(server, "get_feedback", slug="redesign")
    assert result == FEEDBACK
    assert_valid(result, "Feedback")


def test_get_feedback_defaults_to_the_server_side_cursor(server, mock):
    """SPEC §10: agents stay stateless; a fresh session has nowhere to keep since=58."""
    data(server, "get_feedback", slug="redesign")
    assert mock.calls[-1][1] == ("redesign", None, True)


def test_get_feedback_accepts_an_explicit_since_for_replay(server, mock):
    data(server, "get_feedback", slug="redesign", since=12)
    assert mock.calls[-1][1] == ("redesign", 12, True)


# --- await_feedback ----------------------------------------------------------

EMPTY = {"cursor": 19, "annotations": [], "cards_edited": [], "cards_deleted": [],
         "cards_moved": [], "links_added": [], "links_removed": [], "summary": ""}


def test_await_feedback_returns_as_soon_as_something_arrives(server, mock):
    mock.feedback_queue = [EMPTY, FEEDBACK, FEEDBACK]
    result = data(server, "await_feedback", slug="redesign", timeout_s=5, poll_s=0.01)
    assert result["summary"] == FEEDBACK["summary"]


def test_await_feedback_peeks_before_it_consumes(server, mock):
    """A poll that finds nothing must never advance the cursor."""
    mock.feedback_queue = [EMPTY]
    data(server, "await_feedback", slug="redesign", timeout_s=0.05, poll_s=0.01)
    polls = [args for name, args, _ in mock.calls if name == "get_feedback"]
    assert polls, "it must have polled at least once"
    assert all(advance is False for _, _, advance in polls)


def test_await_feedback_times_out_quietly(server, mock):
    mock.feedback_queue = [EMPTY]
    result = data(server, "await_feedback", slug="redesign", timeout_s=0.05, poll_s=0.01)
    assert result["summary"] == ""
    assert_valid(result, "Feedback")


# --- errors ------------------------------------------------------------------

def test_a_client_error_reaches_the_agent_as_a_readable_message(server, mock):
    from fastmcp.exceptions import ToolError

    from client import NotFound

    def boom(slug, include_deleted=False):
        raise NotFound(404, {"error": "not_found", "message": "no space 'nope'"})

    mock.get_canvas = boom
    with pytest.raises(ToolError) as exc:
        asyncio.run(server.call_tool("read_space", {"slug": "nope"}))
    assert "no space 'nope'" in str(exc.value)
    assert "Traceback" not in str(exc.value)
