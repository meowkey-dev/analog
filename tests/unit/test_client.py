"""WP2a: every §3 endpoint callable, typed, retries once, unit-tested on fixtures.

No server: an httpx MockTransport serves contracts/fixtures/ and records requests.
"""

from __future__ import annotations

import httpx
import pytest

import analog.client as analog_client
from analog.client import Analog, ActorRequired, Conflict, NotFound, ValidationFailed
from tests.conftest import fixture

CANVAS = fixture("canvas.json")
SPACE = fixture("space.json")
ANNOTATIONS = fixture("annotations.json")
EVENTS = fixture("events.json")
FEEDBACK = fixture("feedback.claude-code.since-12.json")

ROUTES = {
    ("GET", "/api/spaces"): [SPACE],
    ("POST", "/api/spaces"): SPACE,
    ("GET", "/api/spaces/redesign"): SPACE,
    ("PATCH", "/api/spaces/redesign"): SPACE,
    ("DELETE", "/api/spaces/redesign"): None,
    ("GET", "/api/spaces/redesign/canvas"): CANVAS,
    ("POST", "/api/spaces/redesign/import"): {"id_map": {}, "canvas": CANVAS},
    ("POST", "/api/spaces/redesign/cards"): CANVAS["nodes"][:1],
    ("PATCH", "/api/spaces/redesign/cards/c_opt_a"): CANVAS["nodes"][0],
    ("DELETE", "/api/spaces/redesign/cards/c_opt_a"): None,
    ("POST", "/api/spaces/redesign/links"): CANVAS["edges"][:1],
    ("DELETE", "/api/spaces/redesign/links/l_1"): None,
    ("GET", "/api/spaces/redesign/annotations"): ANNOTATIONS,
    ("POST", "/api/spaces/redesign/annotations"): ANNOTATIONS[0],
    ("PATCH", "/api/spaces/redesign/annotations/a_1"): ANNOTATIONS[0],
    ("GET", "/api/spaces/redesign/feedback"): FEEDBACK,
    ("GET", "/api/spaces/redesign/events"): EVENTS,
    ("POST", "/api/spaces/redesign/media"): {"url": "/api/spaces/redesign/media/m_9.png",
                                             "content_type": "image/png", "bytes": 4},
}


@pytest.fixture
def calls():
    return []


@pytest.fixture
def api(calls):
    def handler(request: httpx.Request) -> httpx.Response:
        calls.append(request)
        body = ROUTES.get((request.method, request.url.path), "__missing__")
        if body == "__missing__":
            return httpx.Response(404, json={"error": "not_found", "message": request.url.path})
        if body is None:
            return httpx.Response(204)
        return httpx.Response(201 if request.method == "POST" else 200, json=body)

    return Analog(url="http://testserver", actor="claude-code", actor_kind="agent",
                  transport=httpx.MockTransport(handler), config={})


# --- coverage of §3 ----------------------------------------------------------

def test_every_endpoint_is_callable(api, calls, tmp_path):
    assert api.list_spaces() == [SPACE]
    assert api.create_space("redesign", "T")["slug"] == "redesign"
    assert api.get_space("redesign") == SPACE
    assert api.update_space("redesign", title="T2") == SPACE
    api.delete_space("redesign")
    assert api.get_canvas("redesign") == CANVAS
    assert api.import_canvas("redesign", CANVAS)["canvas"] == CANVAS
    assert api.create_cards("redesign", [{"title": "T", "content": "c"}])[0]["id"] == "c_opt_a"
    assert api.create_nodes("redesign", CANVAS["nodes"][:1])[0]["id"] == "c_opt_a"
    assert api.update_card("redesign", "c_opt_a", {"text": "x"})["id"] == "c_opt_a"
    api.delete_card("redesign", "c_opt_a")
    assert api.create_links("redesign", [{"fromNode": "a", "toNode": "b"}])[0]["id"] == "l_1"
    assert api.link_cards("redesign", "a", "b", "why")["id"] == "l_1"
    api.delete_link("redesign", "l_1")
    assert api.list_annotations("redesign") == ANNOTATIONS
    assert api.create_annotation("redesign", "c_chart", "b")["id"] == "a_1"
    assert api.resolve_annotation("redesign", "a_1", reply="fixed")["id"] == "a_1"
    assert api.get_feedback("redesign") == FEEDBACK
    assert api.list_events("redesign") == EVENTS

    png = tmp_path / "shot.png"
    png.write_bytes(b"\x89PNG")
    assert api.upload_media("redesign", png)["url"].endswith(".png")

    # Every §3 operation, and nothing left untested.
    seen = {(c.method, c.url.path) for c in calls}
    assert seen == set(ROUTES)


# --- actor plumbing (SPEC §2.2) ---------------------------------------------

def test_every_mutation_carries_actor_and_kind(api, calls):
    api.create_space("redesign", "T")
    api.create_cards("redesign", [{"title": "T", "content": "c"}])
    api.update_card("redesign", "c_opt_a", {"text": "x"})
    api.delete_card("redesign", "c_opt_a")
    api.create_annotation("redesign", "c_chart", "b")
    for call in calls:
        assert call.url.params["actor"] == "claude-code"
        assert call.url.params["actor_kind"] == "agent"


def test_feedback_sends_actor_but_not_actor_kind(api, calls):
    api.get_feedback("redesign")
    assert calls[-1].url.params["actor"] == "claude-code"
    assert "actor_kind" not in calls[-1].url.params


def test_reads_send_no_actor(api, calls):
    api.get_canvas("redesign")
    api.list_annotations("redesign")
    api.list_events("redesign")
    assert all("actor" not in c.url.params for c in calls)


def test_an_unconfigured_actor_fails_before_the_request(calls):
    api = Analog(url="http://testserver", actor=None,
                 transport=httpx.MockTransport(lambda r: httpx.Response(200, json={})),
                 config={})
    with pytest.raises(ActorRequired):
        api.create_cards("redesign", [{"title": "T", "content": "c"}])
    assert calls == [], "SPEC §10: fail loudly, do not write anonymously"


def test_reads_still_work_without_an_actor(api):
    api.actor = None
    assert api.get_canvas("redesign") == CANVAS


# --- parameters --------------------------------------------------------------

def test_mode_and_if_match_are_forwarded(api, calls):
    api.update_card("redesign", "c_opt_a", {"text": "x"}, mode="branch", if_match=3)
    assert calls[-1].url.params["mode"] == "branch"
    assert calls[-1].headers["If-Match"] == "3"


def test_feedback_since_and_advance(api, calls):
    api.get_feedback("redesign", since=12, advance=False)
    assert calls[-1].url.params["since"] == "12"
    assert calls[-1].url.params["advance"] == "false"

    api.get_feedback("redesign")
    assert "since" not in calls[-1].url.params
    assert calls[-1].url.params["advance"] == "true"


def test_annotation_filters(api, calls):
    api.list_annotations("redesign", resolved=False, card_id="c_chart")
    assert calls[-1].url.params["resolved"] == "false"
    assert calls[-1].url.params["card_id"] == "c_chart"


def test_include_deleted(api, calls):
    api.get_canvas("redesign", include_deleted=True)
    assert calls[-1].url.params["include_deleted"] == "true"


# --- errors ------------------------------------------------------------------

@pytest.mark.parametrize("status,code,expected", [
    (404, "not_found", NotFound),
    (409, "conflict", Conflict),
    (400, "actor_required", ActorRequired),
    (400, "validation_failed", ValidationFailed),
    (400, "unsupported_kind", ValidationFailed),
])
def test_error_codes_map_to_exceptions(status, code, expected):
    api = Analog(url="http://testserver", actor="a", config={},
                 transport=httpx.MockTransport(lambda r: httpx.Response(
                     status, json={"error": code, "message": "nope"})))
    with pytest.raises(expected) as exc:
        api.get_canvas("redesign")
    assert exc.value.status == status
    assert exc.value.code == code


def test_conflict_exposes_the_current_node():
    node = CANVAS["nodes"][0]
    api = Analog(url="http://testserver", actor="a", config={},
                 transport=httpx.MockTransport(lambda r: httpx.Response(
                     409, json={"error": "conflict", "message": "stale",
                                "current": node})))
    with pytest.raises(Conflict) as exc:
        api.update_card("redesign", "c_opt_a", {"text": "x"}, if_match=1)
    assert exc.value.current == node


# --- retry (WP2a) ------------------------------------------------------------

def test_retries_once_on_a_connection_error(monkeypatch):
    monkeypatch.setattr(analog_client.time, "sleep", lambda _: None)
    attempts = []

    def handler(request):
        attempts.append(request)
        if len(attempts) == 1:
            raise httpx.ConnectError("refused", request=request)
        return httpx.Response(200, json=CANVAS)

    api = Analog(url="http://testserver", actor="a", config={},
                 transport=httpx.MockTransport(handler))
    assert api.get_canvas("redesign") == CANVAS
    assert len(attempts) == 2


def test_gives_up_after_the_second_connection_error(monkeypatch):
    monkeypatch.setattr(analog_client.time, "sleep", lambda _: None)
    attempts = []

    def handler(request):
        attempts.append(request)
        raise httpx.ConnectError("refused", request=request)

    api = Analog(url="http://testserver", actor="a", config={},
                 transport=httpx.MockTransport(handler))
    with pytest.raises(analog_client.AnalogError) as exc:
        api.get_canvas("redesign")
    assert len(attempts) == 2
    assert "cannot reach" in str(exc.value)


def test_http_errors_are_not_retried():
    attempts = []

    def handler(request):
        attempts.append(request)
        return httpx.Response(404, json={"error": "not_found", "message": "x"})

    api = Analog(url="http://testserver", actor="a", config={},
                 transport=httpx.MockTransport(handler))
    with pytest.raises(NotFound):
        api.get_canvas("redesign")
    assert len(attempts) == 1


# --- config (SPEC §4.2) ------------------------------------------------------

@pytest.mark.parametrize("given,expected", [
    ("http://127.0.0.1:8787", "http://127.0.0.1:8787/api"),
    ("http://127.0.0.1:8787/", "http://127.0.0.1:8787/api"),
    ("http://127.0.0.1:8787/api", "http://127.0.0.1:8787/api"),
    ("http://127.0.0.1:8787/api/", "http://127.0.0.1:8787/api"),
])
def test_base_url_normalizes(given, expected):
    assert analog_client.normalize_base(given) == expected


def test_env_configures_url_actor_and_kind(monkeypatch, tmp_path):
    monkeypatch.setenv("ANALOG_CONFIG", str(tmp_path / "missing.toml"))
    monkeypatch.setenv("ANALOG_URL", "http://elsewhere:9000")
    monkeypatch.setenv("ANALOG_ACTOR", "researcher-1")
    monkeypatch.setenv("ANALOG_ACTOR_KIND", "agent")
    api = Analog()
    assert api.base == "http://elsewhere:9000/api"
    assert api.actor == "researcher-1"
    assert api.actor_kind == "agent"


def test_toml_config_is_read_and_env_wins(monkeypatch, tmp_path):
    path = tmp_path / ".analog.toml"
    path.write_text('url = "http://from-toml:1234"\nactor = "from-toml"\n')
    monkeypatch.setenv("ANALOG_CONFIG", str(path))
    monkeypatch.delenv("ANALOG_URL", raising=False)
    monkeypatch.delenv("ANALOG_ACTOR", raising=False)
    assert Analog().actor == "from-toml"

    monkeypatch.setenv("ANALOG_ACTOR", "from-env")
    assert Analog().actor == "from-env"


def test_default_url_is_the_contract_base(monkeypatch, tmp_path):
    monkeypatch.setenv("ANALOG_CONFIG", str(tmp_path / "missing.toml"))
    for var in ("ANALOG_URL", "ANALOG_WEB_URL"):
        monkeypatch.delenv(var, raising=False)
    api = Analog(actor="a")
    assert api.base == "http://127.0.0.1:8787/api"
    assert api.space_url("redesign") == "http://127.0.0.1:8787/s/redesign"


# --- SSE parsing -------------------------------------------------------------

def test_sse_messages_are_parsed_and_comments_ignored():
    lines = [": connected", "", "id: 1", "event: card.created",
             'data: {"seq": 1, "type": "card.created"}', "", ": keepalive", "",
             "id: 2", 'data: {"seq": 2, "type": "card.moved"}', ""]
    assert [m["seq"] for m in analog_client._sse_messages(iter(lines))] == [1, 2]


def test_find_annotation_scans_spaces(api):
    slug, annotation = api.find_annotation("a_2")
    assert slug == "redesign"
    assert annotation["body"].startswith("rewrite cost")


def test_find_annotation_raises_when_absent(api):
    with pytest.raises(NotFound):
        api.find_annotation("a_missing")
