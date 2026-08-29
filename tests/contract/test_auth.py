"""Per-actor bearer tokens (WP7).

SPEC §3 said "a single shared bearer token ... when you first expose it beyond
localhost". A shared token gatekeeps the server but not identity, and §2.2/§10 make
`actor` load-bearing: the event log is only worth having if attribution is true. So a
token names exactly one actor, and the server checks the claim rather than taking it.
"""

from __future__ import annotations

import json
import stat

import pytest

from server.auth import AuthError, TokenStore, bearer, is_loopback, require_auth_for_host
from tests.conftest import AGENT, HUMAN, make_space

pytestmark = pytest.mark.contract


# --- the store ---------------------------------------------------------------

@pytest.fixture
def store(tmp_path):
    return TokenStore(tmp_path / "auth.json")


def test_auth_is_off_until_a_token_exists(store):
    assert store.enabled is False
    assert store.entries() == []
    assert store.resolve("anything") is None


def test_issue_returns_a_usable_token(store):
    token = store.issue("claude-code", "agent")
    assert token.startswith("analog_")
    assert len(token) > 40
    assert store.enabled is True

    identity = store.resolve(token)
    assert (identity.actor, identity.actor_kind) == ("claude-code", "agent")


def test_the_secret_is_never_stored(store):
    token = store.issue("kai", "human")
    raw = store.path.read_text()
    assert token not in raw, "a leaked auth file must not hand over working tokens"
    assert json.loads(raw)["actors"][0]["token_sha256"]
    assert "token_sha256" not in json.dumps(store.entries())


def test_the_store_is_not_world_readable(store):
    store.issue("kai", "human")
    assert stat.S_IMODE(store.path.stat().st_mode) == 0o600


def test_tokens_are_per_actor(store):
    human = store.issue("kai", "human")
    agent = store.issue("claude-code", "agent")
    assert store.resolve(human).actor == "kai"
    assert store.resolve(agent).actor == "claude-code"
    assert human != agent


def test_reissuing_replaces_the_previous_token(store):
    old = store.issue("claude-code", "agent")
    new = store.issue("claude-code", "agent")
    assert store.resolve(old) is None, "the old token must stop working"
    assert store.resolve(new).actor == "claude-code"
    assert len(store.entries()) == 1


def test_revoke(store):
    token = store.issue("codex", "agent")
    assert store.revoke("codex") is True
    assert store.resolve(token) is None
    assert store.revoke("codex") is False


@pytest.mark.parametrize("actor,kind", [("", "agent"), ("a" * 65, "agent"), ("x", "robot")])
def test_issue_validates(store, actor, kind):
    with pytest.raises(AuthError):
        store.issue(actor, kind)


@pytest.mark.parametrize("header,expected", [
    ("Bearer abc", "abc"), ("bearer abc", "abc"), ("Bearer  abc ", "abc"),
    ("Basic abc", None), ("abc", None), ("Bearer", None), ("Bearer ", None), (None, None),
])
def test_bearer_parsing(header, expected):
    assert bearer(header) == expected


# --- the safety rail ---------------------------------------------------------

@pytest.mark.parametrize("host,loopback", [
    ("127.0.0.1", True), ("localhost", True), ("::1", True), ("127.0.0.5", True),
    ("0.0.0.0", False), ("192.168.1.10", False), ("analog.example.com", False),
])
def test_is_loopback(host, loopback):
    assert is_loopback(host) is loopback


def test_loopback_may_run_without_tokens(store):
    require_auth_for_host("127.0.0.1", store)      # must not raise


def test_a_network_bind_without_tokens_is_refused(store):
    with pytest.raises(AuthError) as exc:
        require_auth_for_host("0.0.0.0", store)
    assert "world-writable" in str(exc.value)
    assert "analog token add" in str(exc.value), "the error must say how to fix it"


def test_a_network_bind_with_tokens_is_allowed(store):
    store.issue("kai", "human")
    require_auth_for_host("0.0.0.0", store)


# --- over HTTP ---------------------------------------------------------------

@pytest.fixture
def secured(data_root, monkeypatch):
    """A server with two actors configured."""
    pytest.importorskip("server.main", reason="WP1 not implemented yet")
    from fastapi.testclient import TestClient

    from server.main import create_app

    tokens = TokenStore(data_root / "auth.json")
    secrets = {
        "kai": tokens.issue("kai", "human"),
        "claude-code": tokens.issue("claude-code", "agent"),
    }
    monkeypatch.setenv("ANALOG_AUTH_FILE", str(tokens.path))
    with TestClient(create_app()) as client:
        client.secrets = secrets
        yield client


def auth(client, actor):
    return {"Authorization": f"Bearer {client.secrets[actor]}"}


# health

def test_health_on_an_open_server(client):
    assert client.get("/api/health").json() == {
        "ok": True, "service": "analog", "version": "0.3.0", "auth_required": False}


def test_health_needs_no_token_and_says_one_is_needed(secured):
    r = secured.get("/api/health")
    assert r.status_code == 200, "a client must be able to discover the server first"
    assert r.json()["auth_required"] is True


# 401

@pytest.mark.parametrize("path", [
    "/api/spaces", "/api/spaces/demo", "/api/spaces/demo/canvas",
    "/api/spaces/demo/annotations", "/api/spaces/demo/events",
])
def test_reads_require_a_token_once_one_exists(secured, path):
    r = secured.get(path)
    assert r.status_code == 401, f"{path} was readable without a token"
    assert r.json()["error"] == "unauthorized"
    assert r.headers.get("www-authenticate") == "Bearer"


def test_writes_require_a_token(secured):
    r = secured.post("/api/spaces", params=HUMAN, json={"slug": "x", "title": "X"})
    assert r.status_code == 401


@pytest.mark.parametrize("header", [
    {"Authorization": "Bearer analog_wrong"},
    {"Authorization": "Basic analog_wrong"},
    {"Authorization": "Bearer"},
])
def test_a_bad_token_is_401(secured, header):
    assert secured.get("/api/spaces", headers=header).status_code == 401


def test_a_valid_token_gets_through(secured):
    r = secured.get("/api/spaces", headers=auth(secured, "kai"))
    assert r.status_code == 200
    assert r.json() == []


# whoami

def test_whoami_reports_the_token_identity(secured):
    assert secured.get("/api/whoami", headers=auth(secured, "claude-code")).json() == {
        "authenticated": True, "actor": "claude-code", "actor_kind": "agent"}
    assert secured.get("/api/whoami", headers=auth(secured, "kai")).json() == {
        "authenticated": True, "actor": "kai", "actor_kind": "human"}


def test_whoami_on_an_open_server(client):
    assert client.get("/api/whoami").json() == {
        "authenticated": False, "actor": None, "actor_kind": None}


# attribution

def test_the_token_decides_who_you_are(secured):
    """The point of per-actor tokens: a claim that disagrees with the token loses."""
    r = secured.post("/api/spaces", params={"actor": "kai", "actor_kind": "human"},
                     json={"slug": "demo", "title": "Demo"},
                     headers=auth(secured, "kai"))
    assert r.status_code == 201

    impersonation = secured.post(
        "/api/spaces/demo/cards", params=AGENT,           # claims to be claude-code
        json={"cards": [{"title": "T", "content": "c"}]},
        headers=auth(secured, "kai"))                     # but holds kai's token
    assert impersonation.status_code == 403
    assert impersonation.json()["error"] == "forbidden"
    assert "claude-code" in impersonation.json()["message"]


def test_a_matching_claim_is_accepted_and_attributed(secured):
    secured.post("/api/spaces", params={"actor": "kai", "actor_kind": "human"},
                 json={"slug": "demo", "title": "Demo"}, headers=auth(secured, "kai"))
    node = secured.post("/api/spaces/demo/cards", params=AGENT,
                        json={"cards": [{"title": "T", "content": "c"}]},
                        headers=auth(secured, "claude-code")).json()[0]
    assert node["sp_created_by"] == "claude-code"

    log = secured.get("/api/spaces/demo/events",
                      headers=auth(secured, "kai")).json()["events"]
    assert [(e["type"], e["actor"]) for e in log] == [
        ("space.created", "kai"), ("card.created", "claude-code")]


def test_the_actor_params_are_still_required(secured):
    """Not inferred from the token: SPEC §10 wants a misconfigured agent to fail
    loudly, and a silently corrected actor is not loud."""
    r = secured.post("/api/spaces", json={"slug": "demo", "title": "D"},
                     headers=auth(secured, "kai"))
    assert r.status_code == 400
    assert r.json()["error"] == "actor_required"


def test_kind_must_match_too(secured):
    secured.post("/api/spaces", params={"actor": "kai", "actor_kind": "human"},
                 json={"slug": "demo", "title": "D"}, headers=auth(secured, "kai"))
    r = secured.post("/api/spaces/demo/cards",
                     params={"actor": "kai", "actor_kind": "agent"},
                     json={"cards": [{"title": "T", "content": "c"}]},
                     headers=auth(secured, "kai"))
    assert r.status_code == 403


# the surfaces that cannot send a header

def test_media_is_not_readable_without_a_token(secured):
    """An <img src> cannot carry a header, so the web client fetches media itself
    and makes a blob URL. What must not happen is media being world-readable."""
    secured.post("/api/spaces", params={"actor": "kai", "actor_kind": "human"},
                 json={"slug": "demo", "title": "D"}, headers=auth(secured, "kai"))
    uploaded = secured.post("/api/spaces/demo/media",
                            params={"actor": "kai", "actor_kind": "human"},
                            files={"file": ("a.png", b"\x89PNG", "image/png")},
                            headers=auth(secured, "kai")).json()

    assert secured.get(uploaded["url"]).status_code == 401
    served = secured.get(uploaded["url"], headers=auth(secured, "kai"))
    assert served.status_code == 200 and served.content == b"\x89PNG"


def test_the_event_stream_needs_a_token(secured):
    secured.post("/api/spaces", params={"actor": "kai", "actor_kind": "human"},
                 json={"slug": "demo", "title": "D"}, headers=auth(secured, "kai"))
    assert secured.get("/api/spaces/demo/events/stream").status_code == 401


def test_a_cors_preflight_is_never_gated(secured):
    """A browser sends OPTIONS with no Authorization header. Rejecting it turns
    every real 401 into an opaque network error."""
    r = secured.options("/api/spaces", headers={
        "Origin": "http://localhost:5173",
        "Access-Control-Request-Method": "GET",
        "Access-Control-Request-Headers": "authorization",
    })
    assert r.status_code == 200
    assert r.headers["access-control-allow-origin"] == "http://localhost:5173"


def test_a_401_still_carries_cors_headers(secured):
    """Otherwise the browser reports a network error and the user sees nothing."""
    r = secured.get("/api/spaces", headers={"Origin": "http://localhost:5173"})
    assert r.status_code == 401
    assert r.headers.get("access-control-allow-origin") == "http://localhost:5173"


def test_the_tauri_origin_is_allowed_by_default(secured):
    from server import config

    assert "tauri://localhost" in config.cors_origins()
    r = secured.get("/api/health", headers={"Origin": "tauri://localhost"})
    assert r.headers.get("access-control-allow-origin") == "tauri://localhost"


# an open server keeps behaving exactly as it did

def test_an_open_server_is_unchanged(client):
    make_space(client, "demo")
    assert client.get("/api/spaces/demo").status_code == 200
    assert client.post("/api/spaces/demo/cards", params=AGENT,
                       json={"cards": [{"title": "T", "content": "c"}]}).status_code == 201


# --- the contract ------------------------------------------------------------

def test_the_contract_documents_bearer_auth(openapi):
    scheme = openapi["components"]["securitySchemes"]["bearerAuth"]
    assert (scheme["type"], scheme["scheme"]) == ("http", "bearer")
    # [{bearerAuth}, {}] — a token is accepted, and an open server is still valid.
    assert {} in openapi["security"]
    assert {"bearerAuth": []} in openapi["security"]


def test_the_contract_documents_health_as_public(openapi):
    assert openapi["paths"]["/health"]["get"]["security"] == [{}]


def test_the_contract_documents_whoami(openapi):
    assert "/whoami" in openapi["paths"]
    assert "401" in openapi["paths"]["/whoami"]["get"]["responses"]


def test_the_error_enum_covers_the_new_codes(openapi):
    codes = openapi["components"]["schemas"]["Error"]["properties"]["error"]["enum"]
    assert {"unauthorized", "forbidden"} <= set(codes)
