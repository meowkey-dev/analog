"""Typed HTTP client for the Analog API.

Shapes follow contracts/openapi.json. This is the only place the MCP server and the
CLI talk to the API: SPEC §4 says neither may contain business logic, so this module
does transport, config and error mapping and nothing else.
"""

from __future__ import annotations

import json
import os
import time
from pathlib import Path
from typing import Any, Iterator, Literal, TypedDict

import httpx

__all__ = [
    "Analog", "AnalogError", "NotFound", "Conflict", "ActorRequired", "ValidationFailed",
    "Unauthorized", "Forbidden",
    "Node", "Edge", "Canvas", "Space", "Annotation", "Event", "Feedback", "CardDraft",
    "DEFAULT_URL", "load_config",
]

DEFAULT_URL = "http://127.0.0.1:8787"
ActorKind = Literal["human", "agent"]
Kind = Literal["md", "html", "svg", "plain"]


# --- types (contracts/openapi.json components.schemas) -----------------------

class Node(TypedDict, total=False):
    id: str
    type: Literal["text", "file", "link", "group"]
    x: float
    y: float
    width: float
    height: float
    color: str
    text: str
    file: str
    sp_kind: Kind
    sp_title: str
    sp_created_by: str
    sp_rev: int
    sp_superseded_by: str
    sp_deleted_at: str
    sp_meta: dict[str, Any]


class Edge(TypedDict, total=False):
    id: str
    fromNode: str
    fromSide: Literal["top", "right", "bottom", "left"]
    toNode: str
    toSide: Literal["top", "right", "bottom", "left"]
    label: str
    color: str
    sp_created_by: str


class Canvas(TypedDict):
    nodes: list[Node]
    edges: list[Edge]


class Counts(TypedDict):
    cards: int
    links: int
    open_annotations: int


class Space(TypedDict, total=False):
    id: str
    slug: str
    title: str
    revision_mode: Literal["replace", "branch"]
    seq: int
    created_at: str
    counts: Counts


class Annotation(TypedDict, total=False):
    id: str
    card_id: str
    card_title: str
    card_superseded_by: str      # branch mode only; absent while the card is current
    card_rev: int
    selector: dict[str, Any] | None
    body: str
    motivation: Literal["commenting", "assessing", "editing"]
    creator: str
    creator_kind: ActorKind
    resolved: bool
    resolved_reply: str | None
    stale: bool
    created_at: str


class Event(TypedDict, total=False):
    seq: int
    ts: str
    type: str
    subject_id: str
    actor: str
    actor_kind: ActorKind
    payload: dict[str, Any]


class Feedback(TypedDict):
    cursor: int
    annotations: list[Annotation]
    cards_edited: list[dict[str, Any]]
    cards_deleted: list[dict[str, Any]]
    cards_moved: list[dict[str, Any]]
    links_added: list[dict[str, Any]]
    links_removed: list[dict[str, Any]]
    summary: str


class CardDraft(TypedDict, total=False):
    title: str
    content: str
    kind: Kind
    x: float
    y: float
    width: float
    height: float
    color: str
    meta: dict[str, Any]


# --- errors ------------------------------------------------------------------

class AnalogError(Exception):
    def __init__(self, status: int, body: Any, url: str = ""):
        self.status = status
        self.body = body if isinstance(body, dict) else {"message": str(body)}
        self.code = self.body.get("error", "error")
        self.url = url
        super().__init__(f"{status} {self.code}: {self.body.get('message', body)}")


class NotFound(AnalogError):
    pass


class Conflict(AnalogError):
    @property
    def current(self) -> Node | None:
        """The server's current node. SPEC §3: 409 is surfaced, never auto-resolved."""
        return self.body.get("current")


class ActorRequired(AnalogError):
    pass


class ValidationFailed(AnalogError):
    pass


class Unauthorized(AnalogError):
    """No token, or one the server does not recognise."""


class Forbidden(AnalogError):
    """The token is valid but writes as a different actor."""


_BY_CODE = {"not_found": NotFound, "conflict": Conflict, "actor_required": ActorRequired,
            "validation_failed": ValidationFailed, "unsupported_kind": ValidationFailed,
            "unauthorized": Unauthorized, "forbidden": Forbidden}


# --- config ------------------------------------------------------------------

def load_config(path: Path | None = None) -> dict[str, str]:
    """`~/.analog.toml`, overridden by ANALOG_URL / ANALOG_ACTOR / ANALOG_ACTOR_KIND
    / ANALOG_WEB_URL / ANALOG_SPACE (SPEC §4.2)."""
    import tomllib

    config: dict[str, str] = {}
    path = path or Path(os.environ.get("ANALOG_CONFIG", Path.home() / ".analog.toml"))
    if path.is_file():
        raw = tomllib.loads(path.read_text())
        config.update({k: str(v) for k, v in raw.items() if not isinstance(v, dict)})
    for key, env in (("url", "ANALOG_URL"), ("actor", "ANALOG_ACTOR"),
                     ("actor_kind", "ANALOG_ACTOR_KIND"), ("web_url", "ANALOG_WEB_URL"),
                     ("space", "ANALOG_SPACE"), ("token", "ANALOG_TOKEN")):
        if os.environ.get(env):
            config[key] = os.environ[env]
    return config


def normalize_base(url: str) -> str:
    url = url.rstrip("/")
    return url if url.endswith("/api") else url + "/api"


# --- client ------------------------------------------------------------------

class Analog:
    """Every §3 endpoint, one method each.

    `actor` has no default on purpose (SPEC §10): an unconfigured agent must fail
    loudly rather than write anonymously.
    """

    def __init__(self, url: str | None = None, actor: str | None = None,
                 actor_kind: ActorKind | None = None, *, timeout: float = 30.0,
                 transport: httpx.BaseTransport | None = None,
                 token: str | None = None,
                 config: dict[str, str] | None = None):
        config = load_config() if config is None else config
        self.base = normalize_base(url or config.get("url") or DEFAULT_URL)
        self.actor = actor or config.get("actor")
        self.actor_kind: ActorKind = actor_kind or config.get("actor_kind") or "agent"
        # A remote server issues one token per actor and takes `actor` from it.
        self.token = token or config.get("token")
        self.web_url = (config.get("web_url") or self.base.removesuffix("/api")).rstrip("/")
        # SPEC §4.2 spells `analog resolve a_7f` with no slug; ANALOG_SPACE supplies it.
        self.config_space = config.get("space")
        headers = {"Authorization": f"Bearer {self.token}"} if self.token else {}
        self._http = httpx.Client(base_url=self.base, timeout=timeout,
                                  transport=transport, headers=headers)

    def close(self) -> None:
        self._http.close()

    def __enter__(self) -> "Analog":
        return self

    def __exit__(self, *exc) -> None:
        self.close()

    # --- plumbing ------------------------------------------------------------

    def _actor_params(self, *, kind: bool = True) -> dict[str, str]:
        if not self.actor:
            raise ActorRequired(400, {
                "error": "actor_required",
                "message": "no actor configured; set ANALOG_ACTOR or pass actor="})
        params = {"actor": self.actor}
        if kind:
            params["actor_kind"] = self.actor_kind
        return params

    def _request(self, method: str, path: str, **kw) -> Any:
        """One retry on a connection-level failure; never on an HTTP status."""
        last: Exception | None = None
        for attempt in range(2):
            try:
                response = self._http.request(method, path, **kw)
                break
            except (httpx.ConnectError, httpx.ConnectTimeout, httpx.ReadError,
                    httpx.RemoteProtocolError) as exc:
                last = exc
                if attempt == 0:
                    time.sleep(0.25)
        else:
            raise AnalogError(0, {
                "error": "unreachable",
                "message": f"cannot reach {self.base}: {last}"}, url=path) from last

        if response.status_code >= 400:
            try:
                body = response.json()
            except ValueError:
                body = {"error": "error", "message": response.text}
            raise _BY_CODE.get(body.get("error"), AnalogError)(
                response.status_code, body, url=str(response.url))
        if response.status_code == 204 or not response.content:
            return None
        return response.json()

    # --- connection ----------------------------------------------------------

    def health(self) -> dict[str, Any]:
        """Reachable without a token; says whether one is needed."""
        return self._request("GET", "/health")

    def whoami(self) -> dict[str, Any]:
        return self._request("GET", "/whoami")

    # --- spaces --------------------------------------------------------------

    def list_spaces(self) -> list[Space]:
        return self._request("GET", "/spaces")

    def create_space(self, slug: str, title: str,
                     revision_mode: Literal["replace", "branch"] = "replace") -> Space:
        return self._request("POST", "/spaces", params=self._actor_params(),
                             json={"slug": slug, "title": title,
                                   "revision_mode": revision_mode})

    def get_space(self, slug: str) -> Space:
        return self._request("GET", f"/spaces/{slug}")

    def update_space(self, slug: str, **patch: Any) -> Space:
        return self._request("PATCH", f"/spaces/{slug}", params=self._actor_params(),
                             json=patch)

    def delete_space(self, slug: str) -> None:
        self._request("DELETE", f"/spaces/{slug}", params=self._actor_params())

    # --- canvas --------------------------------------------------------------

    def get_canvas(self, slug: str, include_deleted: bool = False) -> Canvas:
        return self._request("GET", f"/spaces/{slug}/canvas",
                             params={"include_deleted": str(include_deleted).lower()})

    def import_canvas(self, slug: str, canvas: Canvas) -> dict[str, Any]:
        return self._request("POST", f"/spaces/{slug}/import",
                             params=self._actor_params(),
                             json={"nodes": canvas.get("nodes", []),
                                   "edges": canvas.get("edges", [])})

    # --- cards ---------------------------------------------------------------

    def create_cards(self, slug: str, cards: list[CardDraft]) -> list[Node]:
        return self._request("POST", f"/spaces/{slug}/cards",
                             params=self._actor_params(), json={"cards": cards})

    def create_nodes(self, slug: str, nodes: list[Node]) -> list[Node]:
        """Raw JSON Canvas nodes — the only way to create a `file` node."""
        return self._request("POST", f"/spaces/{slug}/cards",
                             params=self._actor_params(), json={"nodes": nodes})

    def update_card(self, slug: str, card_id: str, patch: dict[str, Any], *,
                    mode: Literal["replace", "branch"] | None = None,
                    if_match: int | None = None) -> Node:
        params = self._actor_params()
        if mode:
            params["mode"] = mode
        headers = {"If-Match": str(if_match)} if if_match is not None else None
        return self._request("PATCH", f"/spaces/{slug}/cards/{card_id}", params=params,
                             json=patch, headers=headers)

    def delete_card(self, slug: str, card_id: str) -> None:
        self._request("DELETE", f"/spaces/{slug}/cards/{card_id}",
                      params=self._actor_params())

    # --- links ---------------------------------------------------------------

    def create_links(self, slug: str, edges: list[dict[str, Any]]) -> list[Edge]:
        return self._request("POST", f"/spaces/{slug}/links",
                             params=self._actor_params(), json={"edges": edges})

    def link_cards(self, slug: str, from_id: str, to_id: str,
                   label: str | None = None) -> Edge:
        edge: dict[str, Any] = {"fromNode": from_id, "toNode": to_id}
        if label:
            edge["label"] = label
        return self.create_links(slug, [edge])[0]

    def delete_link(self, slug: str, link_id: str) -> None:
        self._request("DELETE", f"/spaces/{slug}/links/{link_id}",
                      params=self._actor_params())

    # --- annotations ---------------------------------------------------------

    def list_annotations(self, slug: str, *, resolved: bool | None = None,
                         card_id: str | None = None) -> list[Annotation]:
        params: dict[str, Any] = {}
        if resolved is not None:
            params["resolved"] = str(resolved).lower()
        if card_id:
            params["card_id"] = card_id
        return self._request("GET", f"/spaces/{slug}/annotations", params=params)

    def create_annotation(self, slug: str, card_id: str, body: str, *,
                          selector: dict[str, Any] | None = None,
                          motivation: str = "commenting") -> Annotation:
        return self._request("POST", f"/spaces/{slug}/annotations",
                             params=self._actor_params(),
                             json={"card_id": card_id, "body": body,
                                   "selector": selector, "motivation": motivation})

    def resolve_annotation(self, slug: str, annotation_id: str, *,
                           reply: str | None = None,
                           resolved: bool = True) -> Annotation:
        return self._request("PATCH", f"/spaces/{slug}/annotations/{annotation_id}",
                             params=self._actor_params(),
                             json={"resolved": resolved, "reply": reply})

    def find_annotation(self, annotation_id: str) -> tuple[str, Annotation]:
        """Locate an annotation by id alone.

        SPEC §4.1/§4.2 spell `resolve_annotation(id)` and `analog resolve a_7f`
        without a slug, and the API has no cross-space lookup. Scanning spaces is a
        lookup, not a rule, so it stays out of the server.
        """
        for space in self.list_spaces():
            for annotation in self.list_annotations(space["slug"]):
                if annotation["id"] == annotation_id:
                    return space["slug"], annotation
        raise NotFound(404, {"error": "not_found",
                             "message": f"no annotation {annotation_id!r} in any space"})

    # --- feedback, events ----------------------------------------------------

    def get_feedback(self, slug: str, *, since: int | None = None,
                     advance: bool = True) -> Feedback:
        params: dict[str, Any] = {**self._actor_params(kind=False),
                                  "advance": str(advance).lower()}
        if since is not None:
            params["since"] = since
        return self._request("GET", f"/spaces/{slug}/feedback", params=params)

    def list_events(self, slug: str, *, since: int = 0, limit: int = 200) -> dict[str, Any]:
        return self._request("GET", f"/spaces/{slug}/events",
                             params={"since": since, "limit": limit})

    def stream_events(self, slug: str, *, since: int = 0) -> Iterator[Event]:
        """SSE. Yields events as they arrive; reconnects with Last-Event-ID.

        The token rides in the client's default headers, which is why this uses
        httpx rather than anything EventSource-shaped.
        """
        last = since
        while True:
            try:
                with self._http.stream(
                    "GET", f"/spaces/{slug}/events/stream",
                    headers={"Last-Event-ID": str(last), "Accept": "text/event-stream"},
                    timeout=httpx.Timeout(None, connect=10.0),
                ) as response:
                    if response.status_code >= 400:
                        response.read()
                        raise AnalogError(response.status_code, {
                            "error": "error", "message": response.text})
                    for payload in _sse_messages(response.iter_lines()):
                        last = max(last, payload.get("seq", last))
                        yield payload
            except (httpx.ConnectError, httpx.ReadError, httpx.RemoteProtocolError):
                time.sleep(2.0)   # SPEC §5: fall back and retry rather than die

    # --- media ---------------------------------------------------------------

    def upload_media(self, slug: str, path: str | Path, *,
                     content_type: str | None = None) -> dict[str, Any]:
        path = Path(path)
        import mimetypes

        guessed = content_type or mimetypes.guess_type(path.name)[0] or "application/octet-stream"
        return self._request(
            "POST", f"/spaces/{slug}/media", params=self._actor_params(),
            files={"file": (path.name, path.read_bytes(), guessed)})

    # --- convenience ---------------------------------------------------------

    def space_url(self, slug: str) -> str:
        return f"{self.web_url}/s/{slug}"


def _sse_messages(lines: Iterator[str]) -> Iterator[dict[str, Any]]:
    data: list[str] = []
    for line in lines:
        if line.startswith(":"):
            continue
        if line == "":
            if data:
                try:
                    yield json.loads("\n".join(data))
                except ValueError:
                    pass
                data = []
            continue
        if line.startswith("data:"):
            data.append(line[5:].lstrip())
