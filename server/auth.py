"""Per-actor bearer tokens.

SPEC §3 sketched "a single shared bearer token from an env var" for the moment the
server leaves localhost. That gatekeeps the server but not identity: anyone holding
the shared token could still write as any actor, and the event log's whole value is
that `actor` is trustworthy (§2.2, §10). So a token identifies exactly one actor,
and the server takes `actor`/`actor_kind` from the token rather than believing the
query string.

The store is a JSON file, not a table: `server/schema.sql` is a frozen contract, and
credentials are operator state rather than canvas data. It holds SHA-256 digests, so
a leaked file does not hand over working tokens.

Auth is OFF when the file is absent or empty, which keeps a loopback dev server
exactly as it was. `require_auth_for_host` refuses to start an unauthenticated
server on a non-loopback address, so nobody exposes an open canvas by accident.
"""

from __future__ import annotations

import hashlib
import hmac
import ipaddress
import json
import secrets
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path

TOKEN_PREFIX = "analog_"
TOKEN_BYTES = 32
STORE_VERSION = 1


@dataclass(frozen=True)
class Identity:
    actor: str
    actor_kind: str


class AuthError(Exception):
    """Raised for configuration problems, not for failed requests."""


def new_token() -> str:
    """A fresh secret. The prefix makes it recognisable in logs and leak scanners."""
    return TOKEN_PREFIX + secrets.token_urlsafe(TOKEN_BYTES)


def digest(token: str) -> str:
    return hashlib.sha256(token.encode()).hexdigest()


def now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds").replace("+00:00", "Z")


class TokenStore:
    def __init__(self, path: Path):
        self.path = Path(path)

    # --- persistence ---------------------------------------------------------

    def _read(self) -> dict:
        if not self.path.is_file():
            return {"version": STORE_VERSION, "actors": []}
        try:
            data = json.loads(self.path.read_text() or "{}")
        except ValueError as exc:
            raise AuthError(f"{self.path} is not valid JSON: {exc}") from exc
        data.setdefault("version", STORE_VERSION)
        data.setdefault("actors", [])
        return data

    def _write(self, data: dict) -> None:
        self.path.parent.mkdir(parents=True, exist_ok=True)
        tmp = self.path.with_suffix(".tmp")
        tmp.write_text(json.dumps(data, indent=2) + "\n")
        tmp.replace(self.path)
        # Tokens are credentials even as digests; don't leave them world-readable.
        self.path.chmod(0o600)

    # --- queries -------------------------------------------------------------

    @property
    def enabled(self) -> bool:
        return bool(self._read()["actors"])

    def entries(self) -> list[dict]:
        """Every actor, without any secret material."""
        return [{k: v for k, v in entry.items() if k != "token_sha256"}
                for entry in self._read()["actors"]]

    def resolve(self, token: str | None) -> Identity | None:
        if not token:
            return None
        wanted = digest(token)
        for entry in self._read()["actors"]:
            # Constant-time: a timing oracle on a digest is cheap to avoid.
            if hmac.compare_digest(entry.get("token_sha256", ""), wanted):
                return Identity(entry["name"], entry["kind"])
        return None

    # --- administration ------------------------------------------------------

    def issue(self, actor: str, actor_kind: str) -> str:
        """Mint a token for `actor`, replacing any existing one. Returns the secret,
        which is the only time it is ever visible."""
        if actor_kind not in ("human", "agent"):
            raise AuthError("actor_kind must be 'human' or 'agent'")
        if not actor or len(actor) > 64:
            raise AuthError("actor must be 1-64 characters")

        token = new_token()
        data = self._read()
        data["actors"] = [e for e in data["actors"] if e["name"] != actor]
        data["actors"].append({
            "name": actor, "kind": actor_kind,
            "token_sha256": digest(token), "created_at": now(),
        })
        self._write(data)
        return token

    def revoke(self, actor: str) -> bool:
        data = self._read()
        remaining = [e for e in data["actors"] if e["name"] != actor]
        if len(remaining) == len(data["actors"]):
            return False
        data["actors"] = remaining
        self._write(data)
        return True


def bearer(header: str | None) -> str | None:
    """Pull the token out of an Authorization header."""
    if not header:
        return None
    scheme, _, value = header.partition(" ")
    return value.strip() or None if scheme.lower() == "bearer" else None


def is_loopback(host: str) -> bool:
    if host in ("localhost", ""):
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


def require_auth_for_host(host: str, store: TokenStore) -> None:
    """Refuse to serve an unauthenticated canvas to a network."""
    if is_loopback(host) or store.enabled:
        return
    raise AuthError(
        f"refusing to bind {host} with no tokens configured — an unauthenticated "
        f"Analog on a network is world-writable.\n"
        f"Issue one first:  analog token add <actor> --kind human\n"
        f"(store: {store.path})"
    )
