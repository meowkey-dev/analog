"""ID scheme.

schema.sql specifies `s_<ulid>`, `c_<ulid>`, `l_<ulid>`, `a_<ulid>`; media reuses the
same shape with `m_`. ULID over UUID4 because ids sort by creation time, which makes
event logs and `ORDER BY id` readable without a separate timestamp column.
"""

from __future__ import annotations

from ulid import ULID

SPACE = "s_"
CARD = "c_"
LINK = "l_"
ANNOTATION = "a_"
MEDIA = "m_"


def new(prefix: str) -> str:
    return f"{prefix}{ULID()}"


def space_id() -> str:
    return new(SPACE)


def card_id() -> str:
    return new(CARD)


def link_id() -> str:
    return new(LINK)


def annotation_id() -> str:
    return new(ANNOTATION)


def media_id() -> str:
    return new(MEDIA)
