"""Shared fixtures for the contract suite.

The suite is written against contracts/ and SPEC.md, not against an implementation,
and it talks to the server the way any other client would: over HTTP, to a separate
process. Nothing here imports the server, so the same tests judge any binary that
answers the socket.

Contract the server binary must honour (see tests/README.md):

    <bin> --host H --port P            serve; /api/health answers when ready
    <bin> seed --db D --media-dir M --reset
                                       load contracts/fixtures/ into a fresh database
    <bin> token add ACTOR --kind K     mint a token into $ANALOG_AUTH_FILE and print
                                       it on a line of its own

`ANALOG_SERVER_BIN` names the binary. Unset, the fixtures fall back to
`bin/analog-server` in the checkout, which is where scripts/build.sh puts it.

Configuration reaches the server through the environment, explicitly — a subprocess
does not see monkeypatch.setenv, so `data_root` records the values and the spawn
helpers pass them on.
"""

from __future__ import annotations

import json
import os
import re
import shlex
import socket
import subprocess
import sys
import time
from pathlib import Path

import httpx
import pytest
from jsonschema import Draft202012Validator, FormatChecker

REPO_ROOT = Path(__file__).resolve().parent.parent
FIXTURES = REPO_ROOT / "contracts" / "fixtures"
sys.path.insert(0, str(REPO_ROOT))

OPENAPI = json.loads((REPO_ROOT / "contracts" / "openapi.json").read_text())


def schema_sql() -> Path:
    """schema.sql, as a file rather than as an import.

    Frozen bytes; the path moved once already (analog/server/ -> internal/store/),
    so this looks it up rather than importing a constant from an implementation.
    """
    path = REPO_ROOT / "internal" / "store" / "schema.sql"
    if not path.is_file():
        raise AssertionError(f"no schema.sql at {path}")
    return path

# How long to wait for a freshly spawned server to answer /api/health.
STARTUP_TIMEOUT = 20.0
TOKEN_RE = re.compile(r"^analog_[A-Za-z0-9_-]+$")


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


# --- the binary under test ---------------------------------------------------

def server_bin() -> list[str]:
    """The server command under test."""
    raw = os.environ.get("ANALOG_SERVER_BIN", "").strip()
    if raw:
        return shlex.split(raw)
    default = REPO_ROOT / "bin" / "analog-server"
    if not default.is_file():
        raise pytest.UsageError(
            f"no server binary: {default} does not exist and ANALOG_SERVER_BIN is "
            f"unset.\n  build one with:  scripts/build.sh")
    return [str(default)]


def _serve_cmd(host: str, port: int) -> list[str]:
    return [*server_bin(), "--host", host, "--port", str(port)]


def _seed_cmd(db: Path, media: Path) -> list[str]:
    # The seed path a human runs is the seed path the tests exercise.
    return [*server_bin(), "seed", "--db", str(db), "--media-dir", str(media), "--reset"]


def _token_cmd(actor: str, kind: str) -> list[str]:
    return [*server_bin(), "token", "add", actor, "--kind", kind]


def _run(cmd: list[str], env: dict[str, str]) -> subprocess.CompletedProcess:
    proc = subprocess.run(cmd, capture_output=True, text=True,
                          env={**os.environ, **env})
    assert proc.returncode == 0, (
        f"{' '.join(cmd)} failed ({proc.returncode}):\n{proc.stdout}\n{proc.stderr}")
    return proc


def _free_port() -> int:
    probe = socket.socket()
    probe.bind(("127.0.0.1", 0))
    port = probe.getsockname()[1]
    probe.close()
    return port


class Server:
    """A running server process, and an httpx client pointed at it.

    Tests use it exactly like the old TestClient: `server.get("/api/health")`.
    """

    def __init__(self, env: dict[str, str],
                 tokens: list[tuple[str, str]] | None = None):
        self.env = env
        self.port = _free_port()
        self.base_url = f"http://127.0.0.1:{self.port}"
        self.secrets: dict[str, str] = {}
        # Issued before the process starts, the way an operator would. The store is
        # re-read per request, so issuing later works too — test_cli_auth relies on
        # it — but most tests should not depend on that.
        for actor, kind in tokens or ():
            self.issue_token(actor, kind)
        self.proc = subprocess.Popen(
            _serve_cmd("127.0.0.1", self.port),
            env={**os.environ, **env},
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
        # follow_redirects matches TestClient's default, so a trailing-slash
        # redirect does not change what a test sees.
        self._http = httpx.Client(base_url=self.base_url, timeout=30.0,
                                  follow_redirects=True)
        self._await_health()

    def _await_health(self) -> None:
        deadline = time.monotonic() + STARTUP_TIMEOUT
        while time.monotonic() < deadline:
            if self.proc.poll() is not None:
                raise AssertionError(
                    f"server exited with {self.proc.returncode} before serving:\n"
                    f"{self.proc.stdout.read() if self.proc.stdout else ''}")
            try:
                if self._http.get("/api/health").status_code == 200:
                    return
            except httpx.HTTPError:
                pass
            time.sleep(0.05)
        self.close()
        raise AssertionError(f"server did not answer /api/health in {STARTUP_TIMEOUT}s")

    # httpx passthrough --------------------------------------------------------
    def request(self, *a, **kw):
        return self._http.request(*a, **kw)

    def get(self, *a, **kw):
        return self._http.get(*a, **kw)

    def post(self, *a, **kw):
        return self._http.post(*a, **kw)

    def patch(self, *a, **kw):
        return self._http.patch(*a, **kw)

    def put(self, *a, **kw):
        return self._http.put(*a, **kw)

    def delete(self, *a, **kw):
        return self._http.delete(*a, **kw)

    def options(self, *a, **kw):
        return self._http.options(*a, **kw)

    def stream(self, *a, **kw):
        return self._http.stream(*a, **kw)

    def issue_token(self, actor: str, kind: str) -> str:
        """Mint a token against this server's auth file and remember the secret."""
        proc = _run(_token_cmd(actor, kind), self.env)
        for line in proc.stdout.splitlines():
            if TOKEN_RE.match(line.strip()):
                self.secrets[actor] = line.strip()
                return line.strip()
        raise AssertionError(f"no token on stdout:\n{proc.stdout}\n{proc.stderr}")

    def close(self) -> None:
        self._http.close()
        self.proc.terminate()
        try:
            self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            self.proc.wait(timeout=5)

    def __enter__(self) -> "Server":
        return self

    def __exit__(self, *exc) -> None:
        self.close()


def env_for(root: Path) -> dict[str, str]:
    """Everything the server needs to find this data directory.

    Derived from the path rather than read back from os.environ, because a
    subprocess never sees monkeypatch.setenv.
    """
    return {
        "ANALOG_DATA_DIR": str(root),
        "ANALOG_DB": str(root / "analog.db"),
        "ANALOG_AUTH_FILE": str(root / "auth.json"),
    }


@pytest.fixture
def data_root(tmp_path, monkeypatch) -> Path:
    """A throwaway data directory.

    monkeypatch.setenv still runs so in-process helpers — the CLI tests build a
    client in this process — see the same values a spawned server gets.
    """
    root = tmp_path / "analog-data"
    root.mkdir()
    for key, value in env_for(root).items():
        monkeypatch.setenv(key, value)
    return root


@pytest.fixture
def client(data_root):
    """Empty database."""
    with Server(env_for(data_root)) as server:
        yield server


@pytest.fixture
def seeded(data_root):
    """Database loaded from contracts/fixtures/ by the binary's seed command."""
    env = env_for(data_root)
    _run(_seed_cmd(data_root / "analog.db", data_root / "media"), env)
    with Server(env) as server:
        yield server


@pytest.fixture
def live_server(client) -> str:
    """The base URL of the running server.

    Once `client` is a real process this is the same server; the fixture stays
    because SSE tests and the CLI tests want the URL rather than a client.
    """
    return client.base_url


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
