"""Shared fixtures for the contract suite.

The suite is written against contracts/ and SPEC.md, not against an implementation.
Tests that need a running app import `server.main.create_app` lazily and skip with a
clear reason until WP1 lands, so the parts that can be checked today (the spec itself,
the fixtures, schema.sql) still run.

Contract the server must honour for these fixtures to work:

    server.main.create_app() -> FastAPI

reading ANALOG_DB / ANALOG_DATA_DIR at call time, not at import time.
"""

from __future__ import annotations

import json
import subprocess
import sys
from pathlib import Path

import pytest
from jsonschema import Draft202012Validator, FormatChecker

REPO_ROOT = Path(__file__).resolve().parent.parent
FIXTURES = REPO_ROOT / "contracts" / "fixtures"
sys.path.insert(0, str(REPO_ROOT))

OPENAPI = json.loads((REPO_ROOT / "contracts" / "openapi.json").read_text())


def fixture(name: str):
    """Load a contracts/fixtures/ file."""
    return json.loads((FIXTURES / name).read_text())


def schema_for(name: str) -> dict:
    """A standalone JSON Schema for one openapi component, internal $refs intact."""
    return {
        "$ref": f"#/components/schemas/{name}",
        "components": OPENAPI["components"],
    }


def assert_valid(instance, name: str, *, many: bool = False) -> None:
    """Assert `instance` matches the named openapi component schema."""
    schema = schema_for(name)
    if many:
        schema = {"type": "array", "items": schema, "components": OPENAPI["components"]}
    errors = sorted(
        Draft202012Validator(schema, format_checker=FormatChecker()).iter_errors(instance),
        key=lambda e: list(e.path),
    )
    if errors:
        detail = "\n".join(f"  {list(e.path)}: {e.message}" for e in errors[:8])
        raise AssertionError(f"does not match schema {name}:\n{detail}")


@pytest.fixture(scope="session")
def openapi() -> dict:
    return OPENAPI


@pytest.fixture
def data_root(tmp_path, monkeypatch) -> Path:
    """Point the server at a throwaway data directory."""
    root = tmp_path / "analog-data"
    root.mkdir()
    monkeypatch.setenv("ANALOG_DATA_DIR", str(root))
    monkeypatch.setenv("ANALOG_DB", str(root / "analog.db"))
    return root


def _client(data_root: Path):
    pytest.importorskip(
        "server.main",
        reason="WP1 not implemented yet: server.main.create_app() is missing",
    )
    from fastapi.testclient import TestClient

    from server.main import create_app

    return TestClient(create_app())


@pytest.fixture
def client(data_root):
    """Empty database."""
    with _client(data_root) as c:
        yield c


@pytest.fixture
def seeded(data_root):
    """Database loaded from contracts/fixtures/ by scripts/seed.py.

    Deliberately shells out to the real script rather than importing it: the seed
    path a human runs is the seed path the tests exercise.
    """
    proc = subprocess.run(
        [sys.executable, str(REPO_ROOT / "scripts" / "seed.py"),
         "--db", str(data_root / "analog.db"),
         "--media-dir", str(data_root / "media"), "--reset"],
        capture_output=True, text=True,
    )
    assert proc.returncode == 0, f"seed failed:\n{proc.stdout}\n{proc.stderr}"
    with _client(data_root) as c:
        yield c


@pytest.fixture
def live_server(data_root):
    """A real uvicorn server on an ephemeral port.

    Needed for SSE only: starlette's TestClient buffers the whole response body, so
    it can never observe an open stream.
    """
    pytest.importorskip("server.main", reason="WP1 not implemented yet")
    import socket
    import threading
    import time

    import uvicorn

    from server.main import create_app

    probe = socket.socket()
    probe.bind(("127.0.0.1", 0))
    port = probe.getsockname()[1]
    probe.close()

    server = uvicorn.Server(uvicorn.Config(
        create_app(), host="127.0.0.1", port=port, log_level="warning"))
    thread = threading.Thread(target=server.run, daemon=True)
    thread.start()
    deadline = time.monotonic() + 10
    while not server.started and time.monotonic() < deadline:
        time.sleep(0.02)
    assert server.started, "uvicorn did not start"
    try:
        yield f"http://127.0.0.1:{port}"
    finally:
        server.should_exit = True
        thread.join(timeout=5)


# --- request helpers ---------------------------------------------------------
# openapi.json puts actor/actor_kind in the query string. SPEC §3 also permits
# headers; the contract form is what these tests assert.

HUMAN = {"actor": "human", "actor_kind": "human"}
AGENT = {"actor": "claude-code", "actor_kind": "agent"}


def as_actor(actor: str, kind: str = "agent") -> dict:
    return {"actor": actor, "actor_kind": kind}


def make_space(client, slug="demo", title="Demo", revision_mode=None) -> dict:
    body = {"slug": slug, "title": title}
    if revision_mode:
        body["revision_mode"] = revision_mode
    r = client.post("/api/spaces", params=HUMAN, json=body)
    assert r.status_code == 201, r.text
    return r.json()


def add_cards(client, slug, cards, actor=None) -> list[dict]:
    r = client.post(f"/api/spaces/{slug}/cards", params=actor or AGENT, json={"cards": cards})
    assert r.status_code == 201, r.text
    return r.json()


def one_card(client, slug, *, title="Card", content="body", kind="md", **extra) -> dict:
    return add_cards(client, slug, [{"title": title, "content": content, "kind": kind, **extra}])[0]


def events_of(client, slug, since=0) -> list[dict]:
    r = client.get(f"/api/spaces/{slug}/events", params={"since": since})
    assert r.status_code == 200, r.text
    return r.json()["events"]
