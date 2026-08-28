"""SPEC §2.2 / §10: actor is mandatory on every mutation and has no default.

'A misconfigured agent should fail loudly, not write anonymously.'
"""

from __future__ import annotations

import pytest

from tests.conftest import AGENT, HUMAN, make_space, one_card

pytestmark = pytest.mark.contract


@pytest.fixture
def space(client):
    make_space(client, "demo")
    card = one_card(client, "demo")
    link = client.post("/api/spaces/demo/links", params=AGENT, json={
        "edges": [{"fromNode": card["id"], "toNode": card["id"], "label": "self"}]}).json()[0]
    ann = client.post("/api/spaces/demo/annotations", params=HUMAN, json={
        "card_id": card["id"], "body": "hi"}).json()
    return {"card": card, "link": link, "annotation": ann}


def mutations(space):
    c, l, a = space["card"]["id"], space["link"]["id"], space["annotation"]["id"]
    return {
        "createSpace": ("POST", "/api/spaces", {"slug": "other", "title": "O"}),
        "updateSpace": ("PATCH", "/api/spaces/demo", {"title": "renamed"}),
        "deleteSpace": ("DELETE", "/api/spaces/demo", None),
        "importCanvas": ("POST", "/api/spaces/demo/import", {"nodes": [], "edges": []}),
        "createCards": ("POST", "/api/spaces/demo/cards",
                        {"cards": [{"title": "t", "content": "c"}]}),
        "updateCard": ("PATCH", f"/api/spaces/demo/cards/{c}", {"x": 10}),
        "deleteCard": ("DELETE", f"/api/spaces/demo/cards/{c}", None),
        "createLinks": ("POST", "/api/spaces/demo/links",
                        {"edges": [{"fromNode": c, "toNode": c}]}),
        "deleteLink": ("DELETE", f"/api/spaces/demo/links/{l}", None),
        "createAnnotation": ("POST", "/api/spaces/demo/annotations",
                             {"card_id": c, "body": "b"}),
        "resolveAnnotation": ("PATCH", f"/api/spaces/demo/annotations/{a}",
                              {"resolved": True}),
    }


OPERATIONS = [
    "createSpace", "updateSpace", "deleteSpace", "importCanvas", "createCards",
    "updateCard", "deleteCard", "createLinks", "deleteLink", "createAnnotation",
    "resolveAnnotation",
]


@pytest.mark.parametrize("op", OPERATIONS)
@pytest.mark.parametrize("params", [
    pytest.param({}, id="neither"),
    pytest.param({"actor": "claude-code"}, id="no-actor_kind"),
    pytest.param({"actor_kind": "agent"}, id="no-actor"),
    pytest.param({"actor": "", "actor_kind": "agent"}, id="empty-actor"),
])
def test_mutations_reject_a_missing_actor(client, space, op, params):
    method, path, body = mutations(space)[op]
    r = client.request(method, path, params=params, json=body)
    assert r.status_code == 400, f"{op} accepted {params}: {r.status_code} {r.text}"
    assert r.json()["error"] == "actor_required"


@pytest.mark.parametrize("op", OPERATIONS)
def test_mutations_reject_an_unknown_actor_kind(client, space, op):
    method, path, body = mutations(space)[op]
    r = client.request(method, path, params={"actor": "x", "actor_kind": "robot"}, json=body)
    assert r.status_code == 400, f"{op} accepted actor_kind=robot"


def test_media_upload_requires_an_actor(client, space):
    r = client.post("/api/spaces/demo/media", files={"file": ("a.png", b"\x89PNG", "image/png")})
    assert r.status_code == 400
    assert r.json()["error"] == "actor_required"


def test_feedback_requires_an_actor(client, space):
    assert client.get("/api/spaces/demo/feedback").status_code == 400
    assert client.get("/api/spaces/demo/feedback").json()["error"] == "actor_required"


def test_reads_do_not_require_an_actor(client, space):
    for path in ("/api/spaces", "/api/spaces/demo", "/api/spaces/demo/canvas",
                 "/api/spaces/demo/annotations", "/api/spaces/demo/events"):
        assert client.get(path).status_code == 200, path


def test_actor_is_recorded_on_what_it_writes(client):
    make_space(client, "attrib")
    card = one_card(client, "attrib")
    assert card["sp_created_by"] == "claude-code"
    ev = client.get("/api/spaces/attrib/events").json()["events"][-1]
    assert (ev["actor"], ev["actor_kind"]) == ("claude-code", "agent")
