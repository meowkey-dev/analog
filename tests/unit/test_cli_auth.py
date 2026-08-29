"""`analog token`, `analog login`, `analog whoami` against a real secured server."""

from __future__ import annotations

import json

import pytest
from typer.testing import CliRunner

import cli.main as cli_main
from client import Analog

runner = CliRunner()


@pytest.fixture
def token_store(data_root, monkeypatch):
    from server.auth import TokenStore

    path = data_root / "auth.json"
    monkeypatch.setenv("ANALOG_AUTH_FILE", str(path))
    return TokenStore(path)


def invoke(*args, input=None):
    return runner.invoke(cli_main.app, list(args), input=input)


def ok(result):
    assert result.exit_code == 0, f"exit {result.exit_code}\n{result.output}\n{result.exception}"
    return result.output


# --- analog token ------------------------------------------------------------

def test_token_add_prints_the_secret_once(token_store):
    output = ok(invoke("token", "add", "claude-code", "--kind", "agent"))
    secret = next(w for w in output.split() if w.startswith("analog_"))
    assert token_store.resolve(secret).actor == "claude-code"
    assert "not recoverable" in output
    assert "ANALOG_TOKEN" in output, "tell the operator what to do with it"

    # It is never printed again.
    assert secret not in ok(invoke("token", "list"))


def test_token_list_and_revoke(token_store):
    ok(invoke("token", "add", "kai", "--kind", "human"))
    ok(invoke("token", "add", "codex", "--kind", "agent"))

    listing = ok(invoke("token", "list"))
    assert "kai" in listing and "codex" in listing

    rows = json.loads(ok(invoke("token", "list", "--json")))
    assert {r["name"] for r in rows} == {"kai", "codex"}
    assert all("token_sha256" not in r for r in rows)

    ok(invoke("token", "revoke", "codex"))
    assert "codex" not in ok(invoke("token", "list"))
    assert invoke("token", "revoke", "codex").exit_code == 1


def test_revoking_the_last_token_warns_that_auth_is_off(token_store):
    ok(invoke("token", "add", "kai", "--kind", "human"))
    result = invoke("token", "revoke", "kai")
    assert result.exit_code == 0
    assert "auth is now OFF" in result.output


def test_token_add_rejects_a_bad_kind(token_store):
    assert invoke("token", "add", "x", "--kind", "robot").exit_code == 1


# --- against a live secured server -------------------------------------------

@pytest.fixture
def secured_server(live_server, token_store, monkeypatch):
    """live_server is already running; issuing a token secures it in place, because
    the store is read per request."""
    secret = token_store.issue("kai", "human")
    return live_server, secret


def test_whoami_reports_the_open_server(live_server, monkeypatch):
    monkeypatch.setenv("ANALOG_URL", live_server)
    monkeypatch.setenv("ANALOG_ACTOR", "claude-code")
    monkeypatch.delenv("ANALOG_TOKEN", raising=False)
    output = ok(invoke("whoami"))
    assert live_server in output
    assert "auth    off" in output


def test_whoami_reports_the_token_identity(secured_server, monkeypatch, tmp_path):
    url, secret = secured_server
    monkeypatch.setenv("ANALOG_CONFIG", str(tmp_path / "none.toml"))
    monkeypatch.setenv("ANALOG_URL", url)
    monkeypatch.setenv("ANALOG_ACTOR", "kai")
    monkeypatch.setenv("ANALOG_TOKEN", secret)
    output = ok(invoke("whoami"))
    assert "per-actor tokens" in output
    assert "token   valid" in output
    assert "kai (human)" in output


def test_whoami_warns_when_the_actor_disagrees_with_the_token(secured_server, monkeypatch, tmp_path):
    url, secret = secured_server
    monkeypatch.setenv("ANALOG_CONFIG", str(tmp_path / "none.toml"))
    monkeypatch.setenv("ANALOG_URL", url)
    monkeypatch.setenv("ANALOG_ACTOR", "someone-else")
    monkeypatch.setenv("ANALOG_TOKEN", secret)
    output = ok(invoke("whoami"))
    assert "warning" in output and "writes will be refused" in output


def test_a_command_without_a_token_exits_3(secured_server, monkeypatch, tmp_path):
    url, _ = secured_server
    monkeypatch.setenv("ANALOG_CONFIG", str(tmp_path / "none.toml"))
    monkeypatch.setenv("ANALOG_URL", url)
    monkeypatch.setenv("ANALOG_ACTOR", "kai")
    monkeypatch.delenv("ANALOG_TOKEN", raising=False)
    result = invoke("spaces")
    assert result.exit_code == 3
    assert "unauthorized" in result.output
    assert "ANALOG_TOKEN" in result.output, "say how to fix it"


def test_writing_as_the_wrong_actor_is_refused(secured_server, monkeypatch, tmp_path):
    url, secret = secured_server
    monkeypatch.setenv("ANALOG_CONFIG", str(tmp_path / "none.toml"))
    monkeypatch.setenv("ANALOG_URL", url)
    monkeypatch.setenv("ANALOG_TOKEN", secret)
    monkeypatch.setenv("ANALOG_ACTOR", "kai")
    monkeypatch.setenv("ANALOG_ACTOR_KIND", "human")
    ok(invoke("new", "demo", "--title", "Demo"))

    monkeypatch.setenv("ANALOG_ACTOR", "claude-code")   # same token, different claim
    result = invoke("add", "demo", "--text", "x", "--title", "T")
    assert result.exit_code == 1
    assert "forbidden" in result.output.lower() + str(result.exception).lower()


def test_a_kind_only_mismatch_says_which_variable_to_set(secured_server, monkeypatch, tmp_path):
    """ANALOG_ACTOR_KIND defaults to 'agent'; a human with a human token trips this
    on their very first command, so the message has to be actionable."""
    url, secret = secured_server
    monkeypatch.setenv("ANALOG_CONFIG", str(tmp_path / "none.toml"))
    monkeypatch.setenv("ANALOG_URL", url)
    monkeypatch.setenv("ANALOG_TOKEN", secret)
    monkeypatch.setenv("ANALOG_ACTOR", "kai")
    monkeypatch.delenv("ANALOG_ACTOR_KIND", raising=False)      # defaults to agent

    result = invoke("new", "demo", "--title", "Demo")
    assert result.exit_code == 1
    message = result.output + str(result.exception)
    assert "ANALOG_ACTOR_KIND=human" in message


# --- analog login ------------------------------------------------------------

def test_login_writes_a_config_and_learns_the_actor(secured_server, tmp_path, monkeypatch):
    url, secret = secured_server
    config = tmp_path / ".analog.toml"
    ok(invoke("login", url, "--token", secret, "--config", str(config)))

    body = config.read_text()
    assert secret in body and 'actor = "kai"' in body and 'actor_kind = "human"' in body
    assert config.stat().st_mode & 0o077 == 0, "it holds a credential"

    monkeypatch.setenv("ANALOG_CONFIG", str(config))
    for var in ("ANALOG_URL", "ANALOG_ACTOR", "ANALOG_TOKEN", "ANALOG_ACTOR_KIND"):
        monkeypatch.delenv(var, raising=False)
    api = Analog()
    assert api.actor == "kai" and api.token == secret
    assert api.whoami()["actor"] == "kai"


def test_login_refuses_a_bad_token(secured_server, tmp_path):
    url, _ = secured_server
    result = invoke("login", url, "--token", "analog_nope",
                    "--config", str(tmp_path / "c.toml"))
    assert result.exit_code == 3, "3 is the auth-failure exit code"
    assert "did not accept that token" in result.output
    assert not (tmp_path / "c.toml").exists()


def test_login_requires_a_token_when_the_server_wants_one(secured_server, tmp_path):
    url, _ = secured_server
    result = invoke("login", url, "--config", str(tmp_path / "c.toml"))
    assert result.exit_code == 1
    assert "requires a token" in result.output


def test_login_works_against_an_open_server(live_server, tmp_path, monkeypatch):
    config = tmp_path / "c.toml"
    ok(invoke("login", live_server, "--actor", "codex", "--config", str(config)))
    monkeypatch.setenv("ANALOG_CONFIG", str(config))
    for var in ("ANALOG_URL", "ANALOG_ACTOR", "ANALOG_TOKEN"):
        monkeypatch.delenv(var, raising=False)
    assert Analog().actor == "codex"
