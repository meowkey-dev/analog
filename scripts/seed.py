#!/usr/bin/env python3
"""Load contracts/fixtures/ into a fresh SQLite database.

The seeded DB is not decorative: it is calibrated so that the API reproduces the
fixtures byte-for-byte. `GET /spaces/redesign/canvas` returns canvas.json,
`?include_deleted=true` returns canvas.with-deleted.json, and
`GET /spaces/redesign/feedback?actor=claude-code` — with no `since` — returns
feedback.claude-code.since-12.json, because claude-code's stored cursor is seeded
at 12. tests/contract/test_fixtures_roundtrip.py asserts exactly that.

Usage:
    python scripts/seed.py                    # seed the default DB, refuse if it exists
    python scripts/seed.py --reset            # delete the DB first
    python scripts/seed.py --db /tmp/x.db --media-dir /tmp/media
"""

from __future__ import annotations

import argparse
import json
import sqlite3
import struct
import sys
import zlib
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(REPO_ROOT))

from server import config  # noqa: E402

FIXTURES = REPO_ROOT / "contracts" / "fixtures"


def load(name: str):
    return json.loads((FIXTURES / name).read_text())


def apply_schema(conn: sqlite3.Connection) -> None:
    conn.executescript(config.SCHEMA_PATH.read_text())


def last_ts(events: list[dict], subject_id: str, default: str) -> str:
    """updated_at is not in the fixtures; derive it from the event log."""
    stamps = [e["ts"] for e in events if e["subject_id"] == subject_id]
    return stamps[-1] if stamps else default


def seed(conn: sqlite3.Connection, media_root: Path) -> str:
    space = load("space.json")
    canvas = load("canvas.with-deleted.json")
    annotations = load("annotations.json")
    events = load("events.json")["events"]

    space_id = space["id"]
    created_at = space["created_at"]

    conn.execute(
        "INSERT INTO space (id, slug, title, revision_mode, seq, created_at)"
        " VALUES (?, ?, ?, ?, ?, ?)",
        (space_id, space["slug"], space["title"], space["revision_mode"],
         space["seq"], created_at),
    )

    # Cards. Rows are inserted in fixture order so `ORDER BY rowid` reproduces it.
    # sp_deleted_at is a read-time projection of card.deleted_at, not stored in the
    # node blob — otherwise GET /canvas (live only) would leak it.
    for node in canvas["nodes"]:
        node = dict(node)
        deleted_at = node.pop("sp_deleted_at", None)
        conn.execute(
            "INSERT INTO card (id, space_id, node_json, rev, created_by, updated_at,"
            " deleted_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
            (
                node["id"], space_id, json.dumps(node, ensure_ascii=False),
                node.get("sp_rev", 1), node.get("sp_created_by", "claude-code"),
                last_ts(events, node["id"], created_at), deleted_at,
            ),
        )

    for edge in canvas["edges"]:
        conn.execute(
            "INSERT INTO link (id, space_id, edge_json, created_by, updated_at,"
            " deleted_at) VALUES (?, ?, ?, ?, ?, NULL)",
            (
                edge["id"], space_id, json.dumps(edge, ensure_ascii=False),
                edge.get("sp_created_by", "claude-code"),
                last_ts(events, edge["id"], created_at),
            ),
        )

    resolved_ts = {
        e["subject_id"]: e["ts"] for e in events if e["type"] == "annotation.resolved"
    }
    for ann in annotations:
        # card_title and stale are computed on read; never stored.
        conn.execute(
            "INSERT INTO annotation (id, space_id, card_id, card_rev, selector, body,"
            " motivation, creator, creator_kind, resolved, resolved_reply, resolved_at,"
            " created_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)",
            (
                ann["id"], space_id, ann["card_id"], ann["card_rev"],
                json.dumps(ann["selector"]) if ann["selector"] is not None else None,
                ann["body"], ann["motivation"], ann["creator"], ann["creator_kind"],
                1 if ann["resolved"] else 0, ann.get("resolved_reply"),
                resolved_ts.get(ann["id"]) if ann["resolved"] else None,
                ann["created_at"],
            ),
        )

    for ev in events:
        conn.execute(
            "INSERT INTO event (space_id, seq, ts, type, subject_id, actor, actor_kind,"
            " payload) VALUES (?,?,?,?,?,?,?,?)",
            (
                space_id, ev["seq"], ev["ts"], ev["type"], ev["subject_id"],
                ev["actor"], ev["actor_kind"],
                json.dumps(ev["payload"]) if ev.get("payload") is not None else None,
            ),
        )

    # The cursor that makes the default feedback call reproduce the fixture.
    conn.execute(
        "INSERT INTO actor_cursor (space_id, actor, seq) VALUES (?, ?, ?)",
        (space_id, "claude-code", 12),
    )

    conn.commit()
    write_fixture_media(media_root / space_id)
    return space_id


def write_fixture_media(dest: Path) -> None:
    """c_shot is a file node pointing at .../media/m_01.png. Give it a real file."""
    dest.mkdir(parents=True, exist_ok=True)
    (dest / "m_01.png").write_bytes(_placeholder_png(360, 280))


def _placeholder_png(width: int, height: int) -> bytes:
    """A valid greyscale PNG, no dependencies. Diagonal stripes so it is obviously
    a placeholder and not a broken image."""

    def chunk(tag: bytes, data: bytes) -> bytes:
        return (struct.pack(">I", len(data)) + tag + data
                + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))

    raw = bytearray()
    for y in range(height):
        raw.append(0)  # filter: none
        for x in range(width):
            raw.append(0xE8 if ((x + y) // 12) % 2 else 0xC8)
    return (
        b"\x89PNG\r\n\x1a\n"
        + chunk(b"IHDR", struct.pack(">IIBBBBB", width, height, 8, 0, 0, 0, 0))
        + chunk(b"IDAT", zlib.compress(bytes(raw), 9))
        + chunk(b"IEND", b"")
    )


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--db", type=Path, default=None, help="defaults to ANALOG_DB / data/analog.db")
    ap.add_argument("--media-dir", type=Path, default=None, help="defaults to data/media")
    ap.add_argument("--reset", action="store_true", help="delete an existing DB first")
    args = ap.parse_args(argv)

    db = args.db or config.db_path()
    media = args.media_dir or config.media_dir()
    db.parent.mkdir(parents=True, exist_ok=True)

    if db.exists():
        if not args.reset:
            print(f"{db} already exists; pass --reset to replace it", file=sys.stderr)
            return 1
        for suffix in ("", "-wal", "-shm"):
            Path(str(db) + suffix).unlink(missing_ok=True)

    conn = sqlite3.connect(db)
    try:
        apply_schema(conn)
        space_id = seed(conn, media)
    finally:
        conn.close()

    print(f"seeded {db}")
    print(f"  space   redesign ({space_id}) — 6 live cards, 1 deleted, 4 links")
    print(f"  cursor  claude-code @ 12")
    print(f"  media   {media / space_id / 'm_01.png'}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
