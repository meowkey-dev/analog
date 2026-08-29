"""WP2c: every §4.2 command, including stdin `-`, --json, --mode and ANALOG_ACTOR.

The CLI runs against a real server (an in-process app behind an httpx transport), so
these check the whole path from argv to event log.
"""

from __future__ import annotations

import json
import subprocess
import sys

import httpx
import pytest
from typer.testing import CliRunner

import analog.cli.main as cli_main
from analog.client import Analog
from tests.conftest import REPO_ROOT

runner = CliRunner()


@pytest.fixture
def cli(live_server, monkeypatch):
    """`analog` against a real server, acting as ANALOG_ACTOR.

    httpx has no sync ASGI transport, and the CLI is a sync program, so this runs
    the real thing over the loopback rather than faking the transport.
    """
    def make(actor="claude-code", kind="agent", space=None):
        return Analog(url=live_server, actor=actor, actor_kind=kind,
                      config={"space": space} if space else {})

    state = {"factory": make}
    monkeypatch.setattr(cli_main, "api", lambda: state["factory"]())

    def invoke(*args, actor="claude-code", kind="agent", space=None, input=None):
        state["factory"] = lambda: make(actor, kind, space)
        return runner.invoke(cli_main.app, list(args), input=input)

    invoke.url = live_server
    invoke.human = lambda: Analog(url=live_server, actor="human", actor_kind="human",
                                  config={})
    return invoke


def ok(result):
    assert result.exit_code == 0, f"exit {result.exit_code}\n{result.output}\n{result.exception}"
    return result.output


# --- spaces ------------------------------------------------------------------

def test_new_and_spaces(cli):
    ok(cli("new", "redesign", "--title", "Nav redesign"))
    output = ok(cli("spaces"))
    assert "redesign" in output and "Nav redesign" in output

    rows = json.loads(ok(cli("spaces", "--json")))
    assert [s["slug"] for s in rows] == ["redesign"]


def test_open_prints_the_url(cli):
    ok(cli("new", "redesign"))
    assert ok(cli("open", "redesign")).strip().endswith("/s/redesign")


def test_open_on_a_missing_space_exits_nonzero(cli):
    result = cli("open", "nope")
    assert result.exit_code == 1


# --- add, with stdin ---------------------------------------------------------

def test_add_from_a_file(cli, tmp_path):
    ok(cli("new", "redesign"))
    draft = tmp_path / "draft.md"
    draft.write_text("## Option E\n\nlazy load")
    ok(cli("add", "redesign", "--title", "Option E", "--kind", "md",
           "--file", str(draft)))
    nodes = json.loads(ok(cli("cards", "redesign", "--json")))
    assert nodes[0]["sp_title"] == "Option E"
    assert nodes[0]["text"] == "## Option E\n\nlazy load"


def test_add_from_stdin(cli):
    """`cat chart.svg | analog add redesign --title Revenue --kind svg -`"""
    ok(cli("new", "redesign"))
    svg = '<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>'
    ok(cli("add", "redesign", "-", "--title", "Revenue", "--kind", "svg", input=svg))
    node = json.loads(ok(cli("cards", "redesign", "--json")))[0]
    assert node["sp_kind"] == "svg"
    assert node["text"] == svg


def test_add_reports_the_new_id(cli):
    ok(cli("new", "redesign"))
    output = ok(cli("add", "redesign", "--text", "hi", "--title", "T"))
    assert output.split()[0].startswith("c_")


def test_add_without_content_is_a_usage_error(cli):
    ok(cli("new", "redesign"))
    assert cli("add", "redesign", "--title", "T").exit_code != 0


def test_analog_actor_drives_sp_created_by(cli):
    ok(cli("new", "redesign"))
    ok(cli("add", "redesign", "--text", "a", "--title", "A", actor="codex"))
    ok(cli("add", "redesign", "--text", "b", "--title", "B", actor="researcher-1"))
    nodes = json.loads(ok(cli("cards", "redesign", "--json")))
    assert [n["sp_created_by"] for n in nodes] == ["codex", "researcher-1"]


# --- cards, update, rm, link -------------------------------------------------

def test_cards_lists_id_title_kind_and_author(cli):
    ok(cli("new", "redesign"))
    ok(cli("add", "redesign", "--text", "a", "--title", "Option A"))
    line = ok(cli("cards", "redesign")).strip()
    assert line.startswith("c_")
    assert "Option A" in line and "md" in line and "claude-code" in line


def test_update_from_a_file_bumps_rev(cli, tmp_path):
    ok(cli("new", "redesign"))
    card = json.loads(ok(cli("add", "redesign", "--text", "v1", "--title", "T", "--json")))
    fixed = tmp_path / "fixed.svg"
    fixed.write_text("<svg/>")
    ok(cli("update", "redesign", card["id"], "--file", str(fixed)))
    node = json.loads(ok(cli("cards", "redesign", "--json")))[0]
    assert node["text"] == "<svg/>" and node["sp_rev"] == 2


def test_update_mode_branch_keeps_the_old_card(cli, tmp_path):
    ok(cli("new", "redesign"))
    card = json.loads(ok(cli("add", "redesign", "--text", "v1", "--title", "T", "--json")))
    fixed = tmp_path / "fixed.svg"
    fixed.write_text("<svg/>")
    ok(cli("update", "redesign", card["id"], "--file", str(fixed), "--mode", "branch"))
    nodes = json.loads(ok(cli("cards", "redesign", "--json")))
    assert len(nodes) == 2
    assert any(n.get("sp_superseded_by") for n in nodes)


def test_update_surfaces_a_409(cli):
    ok(cli("new", "redesign"))
    card = json.loads(ok(cli("add", "redesign", "--text", "v1", "--title", "T", "--json")))
    result = cli("update", "redesign", card["id"], "--text", "v2", "--if-match", "9")
    assert result.exit_code == 2
    assert "conflict" in result.output.lower() + str(result.exception).lower()


def test_rm_soft_deletes(cli):
    ok(cli("new", "redesign"))
    card = json.loads(ok(cli("add", "redesign", "--text", "a", "--title", "A", "--json")))
    ok(cli("rm", "redesign", card["id"]))
    assert json.loads(ok(cli("cards", "redesign", "--json"))) == []
    assert len(json.loads(ok(cli("cards", "redesign", "--deleted", "--json")))) == 1


def test_link_and_unlink(cli):
    ok(cli("new", "redesign"))
    a = json.loads(ok(cli("add", "redesign", "--text", "a", "--title", "A", "--json")))
    b = json.loads(ok(cli("add", "redesign", "--text", "b", "--title", "B", "--json")))
    edge = json.loads(ok(cli("link", "redesign", a["id"], b["id"],
                             "--label", "depends on", "--json")))
    assert edge["label"] == "depends on"
    ok(cli("unlink", "redesign", edge["id"]))
    assert json.loads(ok(cli("export", "redesign")))["edges"] == []


# --- feedback ----------------------------------------------------------------

def test_feedback_prints_nothing_when_nothing_changed(cli):
    ok(cli("new", "redesign"))
    result = cli("feedback", "redesign")
    assert result.exit_code == 0
    assert result.output.strip() == "", "SPEC §4.2: silence means nothing changed"


def test_feedback_shows_human_comments_deletions_and_links(cli):
    ok(cli("new", "redesign"))
    a = json.loads(ok(cli("add", "redesign", "--text", "a", "--title", "Option A", "--json")))
    b = json.loads(ok(cli("add", "redesign", "--text", "b", "--title", "Option B", "--json")))
    d = json.loads(ok(cli("add", "redesign", "--text", "d", "--title", "Option D", "--json")))
    ok(cli("feedback", "redesign"))                       # consume own writes

    human = cli.human()
    human.create_annotation("redesign", b["id"], "y-axis starts at 40",
                            motivation="editing")
    human.delete_card("redesign", d["id"])
    human.link_cards("redesign", a["id"], b["id"], "depends on")

    output = ok(cli("feedback", "redesign"))
    assert "1 open comment, 1 deleted, 1 new link." in output
    assert "y-axis starts at 40" in output
    assert "[editing]" in output
    assert "Option D" in output
    assert "depends on" in output


def test_feedback_json_matches_the_api_shape(cli):
    ok(cli("new", "redesign"))
    card = json.loads(ok(cli("add", "redesign", "--text", "a", "--title", "A", "--json")))
    ok(cli("feedback", "redesign"))
    human = cli.human()
    human.create_annotation("redesign", card["id"], "look")

    body = json.loads(ok(cli("feedback", "redesign", "--json")))
    assert set(body) == {"cursor", "annotations", "cards_edited", "cards_deleted",
                         "cards_moved", "links_added", "links_removed", "summary"}
    assert body["annotations"][0]["body"] == "look"


def test_feedback_peek_does_not_consume(cli):
    ok(cli("new", "redesign"))
    card = json.loads(ok(cli("add", "redesign", "--text", "a", "--title", "A", "--json")))
    ok(cli("feedback", "redesign"))
    human = cli.human()
    human.update_card("redesign", card["id"], {"text": "v2"})

    assert "1 card edited" in ok(cli("feedback", "redesign", "--peek"))
    assert "1 card edited" in ok(cli("feedback", "redesign", "--peek"))
    assert "1 card edited" in ok(cli("feedback", "redesign"))
    assert "card edited" not in ok(cli("feedback", "redesign"))


def test_cursors_are_per_actor(cli):
    ok(cli("new", "redesign"))
    card = json.loads(ok(cli("add", "redesign", "--text", "a", "--title", "A", "--json")))
    human = cli.human()
    human.update_card("redesign", card["id"], {"text": "v2"})

    assert "1 card edited" in ok(cli("feedback", "redesign", actor="claude-code"))
    assert "1 card edited" in ok(cli("feedback", "redesign", actor="codex"))
    assert ok(cli("feedback", "redesign", actor="claude-code")).strip() == ""


# --- annotations -------------------------------------------------------------

def test_comments_and_resolve_by_id_alone(cli):
    """SPEC §4.2 spells `analog resolve a_7f --reply "..."` with no slug."""
    ok(cli("new", "redesign"))
    card = json.loads(ok(cli("add", "redesign", "--text", "a", "--title", "A", "--json")))
    human = cli.human()
    annotation = human.create_annotation("redesign", card["id"], "fix the axis")

    listing = ok(cli("comments", "redesign"))
    assert annotation["id"] in listing and "fix the axis" in listing

    ok(cli("resolve", annotation["id"], "--reply", "rebased axis at 0"))
    assert ok(cli("comments", "redesign")).strip() == ""
    done = json.loads(ok(cli("comments", "redesign", "--all", "--json")))
    assert done[0]["resolved"] is True
    assert done[0]["resolved_reply"] == "rebased axis at 0"


def test_resolve_uses_analog_space_when_given(cli):
    ok(cli("new", "redesign"))
    card = json.loads(ok(cli("add", "redesign", "--text", "a", "--title", "A", "--json")))
    human = cli.human()
    annotation = human.create_annotation("redesign", card["id"], "b")
    ok(cli("resolve", annotation["id"], space="redesign"))
    assert json.loads(ok(cli("comments", "redesign", "--json"))) == []


def test_resolving_an_unknown_annotation_exits_nonzero(cli):
    ok(cli("new", "redesign"))
    assert cli("resolve", "a_nope").exit_code == 1


# --- export / import ---------------------------------------------------------

def test_export_is_json_canvas_and_import_round_trips(cli):
    ok(cli("new", "redesign"))
    ok(cli("new", "copy"))
    a = json.loads(ok(cli("add", "redesign", "--text", "a", "--title", "A", "--json")))
    b = json.loads(ok(cli("add", "redesign", "--text", "b", "--title", "B", "--json")))
    ok(cli("link", "redesign", a["id"], b["id"], "--label", "leads to"))

    exported = ok(cli("export", "redesign"))
    canvas = json.loads(exported)
    assert set(canvas) == {"nodes", "edges"}

    ok(cli("import", "copy", "-", input=exported))
    copied = json.loads(ok(cli("export", "copy")))
    assert [n["sp_title"] for n in copied["nodes"]] == ["A", "B"]
    assert copied["edges"][0]["label"] == "leads to"


def test_import_is_additive(cli):
    ok(cli("new", "redesign"))
    ok(cli("add", "redesign", "--text", "mine", "--title", "Mine"))
    ok(cli("import", "redesign", "-",
           input=json.dumps({"nodes": [{"id": "x", "type": "text", "x": 0, "y": 0,
                                        "width": 100, "height": 100, "text": "new",
                                        "sp_title": "New"}], "edges": []})))
    assert len(json.loads(ok(cli("cards", "redesign", "--json")))) == 2


# --- events ------------------------------------------------------------------

def test_events_prints_the_log(cli):
    ok(cli("new", "redesign"))
    ok(cli("add", "redesign", "--text", "a", "--title", "A"))
    output = ok(cli("events", "redesign"))
    assert "card.created" in output and "claude-code" in output


# --- packaging ---------------------------------------------------------------

def test_installed_entrypoint_runs():
    """`analog` must work as an installed command, not just as a module."""
    proc = subprocess.run([sys.executable, "-m", "analog.cli.main", "--help"],
                          capture_output=True, text=True, cwd=REPO_ROOT)
    assert proc.returncode == 0, proc.stderr
    for command in ("spaces", "feedback", "add", "cards", "update", "rm", "link",
                    "resolve", "export", "import", "open"):
        assert command in proc.stdout, command
