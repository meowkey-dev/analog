"""HTTP routes. SPEC §3, contracts/openapi.json.

Handlers do argument plumbing and nothing else; every rule lives in store.py.
"""

from __future__ import annotations

from pathlib import Path
from typing import Annotated, Any

from fastapi import Body, Depends, FastAPI, File, Header, Query, Request, UploadFile
from fastapi.exceptions import RequestValidationError
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import FileResponse, JSONResponse, Response, StreamingResponse

from server import config, events as sse
from server.errors import ActorRequired, ApiError, ValidationFailed
from server.models import (AnnotationCreate, AnnotationPatch, CanvasImport, CardsCreate,
                           LinksCreate, SpaceCreate, SpacePatch)
from server.store import Store

API = config.API_PREFIX


def require_actor(
    actor: Annotated[str | None, Query()] = None,
    actor_kind: Annotated[str | None, Query()] = None,
    header_actor: Annotated[str | None, Header(alias="X-Analog-Actor")] = None,
    header_kind: Annotated[str | None, Header(alias="X-Analog-Actor-Kind")] = None,
) -> tuple[str, str]:
    """SPEC §2.2: mandatory, no default, so a misconfigured agent fails loudly."""
    name = actor or header_actor
    kind = actor_kind or header_kind
    if not name or not kind:
        raise ActorRequired("actor and actor_kind are required on every mutation")
    if kind not in ("human", "agent"):
        raise ValidationFailed("actor_kind must be 'human' or 'agent'", actor_kind=kind)
    return name, kind


Actor = Annotated[tuple[str, str], Depends(require_actor)]


def create_app(store: Store | None = None) -> FastAPI:
    app = FastAPI(title="Analog", version="0.1.0")
    app.state.store = store or Store(config.db_path(), config.media_dir())
    app.state.broker = sse.Broker()

    app.add_middleware(
        CORSMiddleware,
        allow_origins=config.cors_origins(),
        allow_credentials=False,
        allow_methods=["*"],
        allow_headers=["*"],
    )

    @app.middleware("http")
    async def fan_out_events(request: Request, call_next):
        response = await call_next(request)
        for space_id, event in app.state.store.drain():
            app.state.broker.publish(space_id, event)
        return response

    @app.exception_handler(ApiError)
    async def _api_error(_: Request, exc: ApiError):
        return JSONResponse(status_code=exc.status, content=exc.body())

    @app.exception_handler(RequestValidationError)
    async def _validation(_: Request, exc: RequestValidationError):
        # FastAPI's 422 body is not the contract's Error schema.
        return JSONResponse(status_code=400, content={
            "error": "validation_failed",
            "message": "request did not match the schema",
            "detail": {"errors": _jsonable(exc.errors())},
        })

    def st() -> Store:
        return app.state.store

    # --- spaces --------------------------------------------------------------

    @app.get(f"{API}/spaces")
    async def list_spaces():
        return st().list_spaces()

    @app.post(f"{API}/spaces", status_code=201)
    async def create_space(actor: Actor, payload: SpaceCreate):
        return st().create_space(payload.slug, payload.title, payload.revision_mode)

    @app.get(API + "/spaces/{slug}")
    async def get_space(slug: str):
        return st().space(slug)

    @app.patch(API + "/spaces/{slug}")
    async def update_space(slug: str, actor: Actor, payload: SpacePatch):
        return st().update_space(slug, payload.model_dump(exclude_none=True))

    @app.delete(API + "/spaces/{slug}", status_code=204)
    async def delete_space(slug: str, actor: Actor):
        st().delete_space(slug)
        return Response(status_code=204)

    # --- canvas --------------------------------------------------------------

    @app.get(API + "/spaces/{slug}/canvas")
    async def get_canvas(slug: str, include_deleted: bool = False):
        return st().canvas(slug, include_deleted)

    @app.post(API + "/spaces/{slug}/import", status_code=201)
    async def import_canvas(slug: str, actor: Actor, payload: CanvasImport):
        name, kind = actor
        return st().import_canvas(slug, payload.model_dump(), actor=name, actor_kind=kind)

    # --- cards ---------------------------------------------------------------

    @app.post(API + "/spaces/{slug}/cards", status_code=201)
    async def create_cards(slug: str, actor: Actor, payload: CardsCreate):
        name, kind = actor
        drafts = ([c.model_dump(exclude_none=True) for c in payload.cards]
                  if payload.cards is not None else None)
        return st().create_cards(slug, drafts=drafts, nodes=payload.nodes,
                                 actor=name, actor_kind=kind)

    @app.patch(API + "/spaces/{slug}/cards/{card_id}")
    async def update_card(
        slug: str, card_id: str, actor: Actor,
        patch: Annotated[dict[str, Any], Body()],
        mode: Annotated[str | None, Query()] = None,
        if_match: Annotated[str | None, Header(alias="If-Match")] = None,
    ):
        name, kind = actor
        return st().update_card(slug, card_id, patch, actor=name, actor_kind=kind,
                                mode=mode, if_match=_parse_if_match(if_match))

    @app.delete(API + "/spaces/{slug}/cards/{card_id}", status_code=204)
    async def delete_card(slug: str, card_id: str, actor: Actor):
        name, kind = actor
        st().delete_card(slug, card_id, actor=name, actor_kind=kind)
        return Response(status_code=204)

    # --- links ---------------------------------------------------------------

    @app.post(API + "/spaces/{slug}/links", status_code=201)
    async def create_links(slug: str, actor: Actor, payload: LinksCreate):
        name, kind = actor
        edges = [e.model_dump(exclude_none=True) for e in payload.edges]
        return st().create_links(slug, edges, actor=name, actor_kind=kind)

    @app.delete(API + "/spaces/{slug}/links/{link_id}", status_code=204)
    async def delete_link(slug: str, link_id: str, actor: Actor):
        name, kind = actor
        st().delete_link(slug, link_id, actor=name, actor_kind=kind)
        return Response(status_code=204)

    # --- annotations ---------------------------------------------------------

    @app.get(API + "/spaces/{slug}/annotations")
    async def list_annotations(slug: str, resolved: bool | None = None,
                               card_id: str | None = None):
        return st().annotations(slug, resolved=resolved, card_id=card_id)

    @app.post(API + "/spaces/{slug}/annotations", status_code=201)
    async def create_annotation(slug: str, actor: Actor, payload: AnnotationCreate):
        name, kind = actor
        return st().create_annotation(
            slug, card_id=payload.card_id, body=payload.body, selector=payload.selector,
            motivation=payload.motivation, actor=name, actor_kind=kind)

    @app.patch(API + "/spaces/{slug}/annotations/{annotation_id}")
    async def resolve_annotation(slug: str, annotation_id: str, actor: Actor,
                                 payload: AnnotationPatch):
        name, kind = actor
        return st().resolve_annotation(slug, annotation_id, resolved=payload.resolved,
                                       reply=payload.reply, actor=name, actor_kind=kind)

    # --- feedback, events ----------------------------------------------------

    @app.get(API + "/spaces/{slug}/feedback")
    async def get_feedback(slug: str, actor: Annotated[str | None, Query()] = None,
                           since: int | None = None, advance: bool = True,
                           header_actor: Annotated[str | None,
                                                   Header(alias="X-Analog-Actor")] = None):
        name = actor or header_actor
        if not name:
            raise ActorRequired("actor is required: a cursor is keyed by actor name")
        return st().feedback(slug, actor=name, since=since, advance=advance)

    @app.get(API + "/spaces/{slug}/events")
    async def list_events(slug: str, since: int = 0, limit: int = 200):
        return st().list_events(slug, since=since, limit=min(max(limit, 1), 1000))

    @app.get(API + "/spaces/{slug}/events/stream")
    async def stream_events(slug: str, request: Request, since: int = 0,
                            last_event_id: Annotated[str | None,
                                                     Header(alias="Last-Event-ID")] = None):
        space_id = st().space_row(slug)["id"]
        start = int(last_event_id) if (last_event_id or "").isdigit() else since
        return StreamingResponse(
            sse.stream(st(), space_id, start, app.state.broker),
            media_type="text/event-stream",
            headers={"Cache-Control": "no-cache", "Connection": "keep-alive",
                     "X-Accel-Buffering": "no"},
        )

    # --- media ---------------------------------------------------------------

    @app.post(API + "/spaces/{slug}/media", status_code=201)
    async def upload_media(slug: str, actor: Actor, file: Annotated[UploadFile, File()]):
        return st().save_media(slug, filename=file.filename or "upload",
                               content_type=file.content_type or "",
                               data=await file.read())

    @app.get(API + "/spaces/{slug}/media/{name}")
    async def get_media(slug: str, name: str):
        """Not in openapi.json — see AMENDMENTS.md #1. canvas.json's file node needs it."""
        path, content_type = st().media_path(slug, name)
        return FileResponse(path, media_type=content_type)

    _mount_web(app)
    return app


def _parse_if_match(raw: str | None) -> int | None:
    if raw is None:
        return None
    value = raw.strip().removeprefix("W/").strip('"')
    if not value.lstrip("-").isdigit():
        raise ValidationFailed("If-Match must be an integer sp_rev", if_match=raw)
    return int(value)


def _jsonable(value):
    if isinstance(value, dict):
        return {k: _jsonable(v) for k, v in value.items()}
    if isinstance(value, (list, tuple)):
        return [_jsonable(v) for v in value]
    if isinstance(value, (str, int, float, bool)) or value is None:
        return value
    return str(value)


def _mount_web(app: FastAPI) -> None:
    """Serve the built SPA when it exists, so production is one origin (SPEC §5)."""
    dist = config.REPO_ROOT / "web" / "dist"
    index = dist / "index.html"
    if not index.is_file():
        return
    from fastapi.staticfiles import StaticFiles

    app.mount("/assets", StaticFiles(directory=dist / "assets"), name="assets")

    @app.get("/{path:path}", include_in_schema=False)
    async def spa(path: str):
        candidate = dist / path
        if path and candidate.is_file() and dist in candidate.resolve().parents:
            return FileResponse(candidate)
        return FileResponse(index)


app = create_app()
