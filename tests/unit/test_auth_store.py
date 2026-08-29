"""TokenStore, bearer parsing and the network-bind rail.

These reach into the implementation rather than a socket, so they live in
tests/unit/ with the other reference tests: they are rewritten in the server's own
language, not run against a binary. The behaviour they describe is asserted
black-box in tests/contract/test_auth.py.
"""

from __future__ import annotations

import json
import stat

import pytest

from analog.server.auth import AuthError, TokenStore, bearer, is_loopback, require_auth_for_host


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
