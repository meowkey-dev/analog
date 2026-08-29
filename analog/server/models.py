"""Request bodies. Responses are plain dicts assembled in store.py so nothing the
contract requires can be silently dropped by a response model."""

from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, ConfigDict, Field


class SpaceCreate(BaseModel):
    # The slug pattern is enforced in the store so the failure is a contract-shaped
    # 400 validation_failed rather than FastAPI's 422.
    slug: str
    title: str
    revision_mode: Literal["replace", "branch"] = "replace"


class SpacePatch(BaseModel):
    title: str | None = None
    revision_mode: Literal["replace", "branch"] | None = None


class CardDraft(BaseModel):
    model_config = ConfigDict(extra="forbid")
    title: str = ""
    content: str = ""
    kind: str = "md"
    x: float | None = None
    y: float | None = None
    width: float | None = None
    height: float | None = None
    color: str | None = None
    meta: dict[str, Any] | None = None


class CardsCreate(BaseModel):
    cards: list[CardDraft] | None = None
    nodes: list[dict[str, Any]] | None = None


class EdgeDraft(BaseModel):
    fromNode: str
    toNode: str
    fromSide: str | None = None
    toSide: str | None = None
    label: str | None = None
    color: str | None = None


class LinksCreate(BaseModel):
    edges: list[EdgeDraft]


class CanvasImport(BaseModel):
    nodes: list[dict[str, Any]] = Field(default_factory=list)
    edges: list[dict[str, Any]] = Field(default_factory=list)


class AnnotationCreate(BaseModel):
    card_id: str
    body: str
    selector: dict[str, Any] | None = None
    # Validated in the store, for the same reason as slug.
    motivation: str = "commenting"


class AnnotationPatch(BaseModel):
    # SPEC §4.1/§4.2: every caller surface only ever resolves.
    resolved: bool = True
    reply: str | None = None
