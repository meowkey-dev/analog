"""Database access and event emission.

All business logic lives here. SPEC §4: "if you find yourself writing a rule in one
of them, it belongs in the server" — the MCP server and the CLI are thin proxies, so
this module is the only place a rule is written down.
"""

from __future__ import annotations

import json
import re
import sqlite3
import threading
from datetime import datetime, timezone
from typing import Any, Iterable

from server import config, ids
from server.errors import Conflict, NotFound, UnsupportedKind, ValidationFailed

SLUG_RE = re.compile(r"^[a-z0-9-]{1,64}$")
KINDS = ("md", "html", "svg", "plain")
MOTIVATIONS = ("commenting", "assessing", "editing")

GEOMETRY_KEYS = frozenset({"x", "y", "width", "height"})
# Set by the server, never by a client patch.
IMMUTABLE_KEYS = frozenset({"id", "sp_rev", "sp_created_by", "sp_superseded_by"})

DEFAULT_WIDTH = 320
DEFAULT_HEIGHT = 200
LAYOUT_GAP = 40
# A batch wraps into a new column past this height rather than growing one very
# tall column. SPEC §5 asks for "a column, top-down"; five cards of it is a
# 1200px strip you have to zoom out to read.
LAYOUT_MAX_COLUMN = 900


def now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds").replace("+00:00", "Z")


class Store:
    def __init__(self, db_path, media_root):
        self.db_path = str(db_path)
        self.media_root = media_root
        self._local = threading.local()
        self.ensure_schema()

    @property
    def _pending(self) -> list:
        pending = getattr(self._local, "pending", None)
        if pending is None:
            pending = self._local.pending = []
        return pending

    # --- connection ----------------------------------------------------------

    @property
    def conn(self) -> sqlite3.Connection:
        conn = getattr(self._local, "conn", None)
        if conn is None:
            conn = sqlite3.connect(self.db_path, isolation_level=None)
            conn.row_factory = sqlite3.Row
            conn.execute("PRAGMA foreign_keys = ON")
            conn.execute("PRAGMA journal_mode = WAL")
            conn.execute("PRAGMA busy_timeout = 5000")
            self._local.conn = conn
        return conn

    def ensure_schema(self) -> None:
        from pathlib import Path

        Path(self.db_path).parent.mkdir(parents=True, exist_ok=True)
        if not self.conn.execute(
            "SELECT 1 FROM sqlite_master WHERE type='table' AND name='space'"
        ).fetchone():
            self.conn.executescript(config.SCHEMA_PATH.read_text())

    def close(self) -> None:
        conn = getattr(self._local, "conn", None)
        if conn is not None:
            conn.close()
            self._local.conn = None

    class _Write:
        def __init__(self, store: "Store"):
            self.store = store

        def __enter__(self):
            self.store.conn.execute("BEGIN IMMEDIATE")
            return self.store.conn

        def __exit__(self, exc_type, exc, tb):
            if exc_type is None:
                self.store.conn.execute("COMMIT")
            else:
                self.store.conn.execute("ROLLBACK")
                self.store._local.pending = []
            return False

    def write(self) -> "_Write":
        return Store._Write(self)

    # --- events --------------------------------------------------------------

    def emit(self, space_id: str, type_: str, subject_id: str, actor: str,
             actor_kind: str, payload: dict | None = None) -> dict:
        """Allocate the next seq for this space and append one row."""
        seq = self.conn.execute(
            "UPDATE space SET seq = seq + 1 WHERE id = ? RETURNING seq", (space_id,)
        ).fetchone()[0]
        ts = now()
        self.conn.execute(
            "INSERT INTO event (space_id, seq, ts, type, subject_id, actor, actor_kind,"
            " payload) VALUES (?,?,?,?,?,?,?,?)",
            (space_id, seq, ts, type_, subject_id, actor, actor_kind,
             json.dumps(payload, ensure_ascii=False) if payload is not None else None),
        )
        event = {"seq": seq, "ts": ts, "type": type_, "subject_id": subject_id,
                 "actor": actor, "actor_kind": actor_kind}
        if payload is not None:
            event["payload"] = payload
        self._pending.append((space_id, event))
        return event

    def drain(self) -> list[tuple[str, dict]]:
        """Events committed since the last drain, for the SSE broker."""
        out, self._local.pending = self._pending, []
        return out

    # --- spaces --------------------------------------------------------------

    def space_row(self, slug: str) -> sqlite3.Row:
        row = self.conn.execute("SELECT * FROM space WHERE slug = ?", (slug,)).fetchone()
        if row is None:
            raise NotFound(f"no space with slug {slug!r}")
        return row

    def space(self, slug: str) -> dict:
        return self._space_dict(self.space_row(slug))

    def _space_dict(self, row: sqlite3.Row) -> dict:
        sid = row["id"]
        counts = {
            "cards": self.conn.execute(
                "SELECT count(*) FROM card WHERE space_id = ? AND deleted_at IS NULL",
                (sid,)).fetchone()[0],
            "links": self.conn.execute(
                "SELECT count(*) FROM link WHERE space_id = ? AND deleted_at IS NULL",
                (sid,)).fetchone()[0],
            "open_annotations": self.conn.execute(
                "SELECT count(*) FROM annotation WHERE space_id = ? AND resolved = 0",
                (sid,)).fetchone()[0],
        }
        return {"id": sid, "slug": row["slug"], "title": row["title"],
                "revision_mode": row["revision_mode"], "seq": row["seq"],
                "created_at": row["created_at"], "counts": counts}

    def list_spaces(self) -> list[dict]:
        return [self._space_dict(r) for r in
                self.conn.execute("SELECT * FROM space ORDER BY rowid")]

    def create_space(self, slug: str, title: str, revision_mode: str = "replace", *,
                     actor: str = "human", actor_kind: str = "human") -> dict:
        if not SLUG_RE.match(slug or ""):
            raise ValidationFailed("slug must match ^[a-z0-9-]{1,64}$", slug=slug)
        if revision_mode not in ("replace", "branch"):
            raise ValidationFailed("revision_mode must be 'replace' or 'branch'")
        if self.conn.execute("SELECT 1 FROM space WHERE slug = ?", (slug,)).fetchone():
            raise Conflict(f"a space with slug {slug!r} already exists")
        space_id = ids.space_id()
        with self.write():
            self.conn.execute(
                "INSERT INTO space (id, slug, title, revision_mode, seq, created_at)"
                " VALUES (?,?,?,?,0,?)",
                (space_id, slug, title, revision_mode, now()))
            self.emit(space_id, "space.created", space_id, actor, actor_kind,
                      {"slug": slug, "title": title})
        return self.space(slug)

    def update_space(self, slug: str, patch: dict) -> dict:
        row = self.space_row(slug)
        title = patch.get("title", row["title"])
        mode = patch.get("revision_mode", row["revision_mode"])
        if mode not in ("replace", "branch"):
            raise ValidationFailed("revision_mode must be 'replace' or 'branch'")
        with self.write():
            self.conn.execute("UPDATE space SET title = ?, revision_mode = ? WHERE id = ?",
                              (title, mode, row["id"]))
        return self.space(slug)

    def delete_space(self, slug: str, *, actor: str = "human",
                     actor_kind: str = "human") -> None:
        row = self.space_row(slug)
        with self.write():
            # Emitted for live subscribers, then taken by the cascade: a per-space
            # log cannot outlive its space. See schema.sql note 5.
            self.emit(row["id"], "space.deleted", row["id"], actor, actor_kind,
                      {"slug": slug})
            self.conn.execute("DELETE FROM space WHERE id = ?", (row["id"],))

    # --- cards ---------------------------------------------------------------

    def _card_row(self, space_id: str, card_id: str, *, allow_deleted=False) -> sqlite3.Row:
        row = self.conn.execute("SELECT * FROM card WHERE id = ? AND space_id = ?",
                                (card_id, space_id)).fetchone()
        if row is None or (row["deleted_at"] and not allow_deleted):
            raise NotFound(f"no card {card_id!r} in this space")
        return row

    def _node(self, row: sqlite3.Row, *, include_deleted=False) -> dict:
        node = json.loads(row["node_json"])
        node["sp_rev"] = row["rev"]
        if include_deleted and row["deleted_at"]:
            node["sp_deleted_at"] = row["deleted_at"]
        return node

    def canvas(self, slug: str, include_deleted: bool = False) -> dict:
        space_id = self.space_row(slug)["id"]
        where = "" if include_deleted else " AND deleted_at IS NULL"
        nodes = [self._node(r, include_deleted=include_deleted) for r in self.conn.execute(
            f"SELECT * FROM card WHERE space_id = ?{where} ORDER BY rowid", (space_id,))]
        edges = [json.loads(r["edge_json"]) for r in self.conn.execute(
            f"SELECT * FROM link WHERE space_id = ?{where} ORDER BY rowid", (space_id,))]
        return {"nodes": nodes, "edges": edges}

    def _layout_cursor(self, space_id: str) -> tuple[float, float]:
        """SPEC §5: a column to the right of the live bounding box, top-down."""
        rows = self.conn.execute(
            "SELECT node_json FROM card WHERE space_id = ? AND deleted_at IS NULL",
            (space_id,)).fetchall()
        boxes = [json.loads(r["node_json"]) for r in rows]
        if not boxes:
            return 0.0, 0.0
        right = max(b.get("x", 0) + b.get("width", DEFAULT_WIDTH) for b in boxes)
        top = min(b.get("y", 0) for b in boxes)
        return right + LAYOUT_GAP, top

    def _draft_to_node(self, draft: dict, actor: str) -> dict:
        kind = draft.get("kind") or "md"
        if kind not in KINDS:
            raise UnsupportedKind(f"kind must be one of {', '.join(KINDS)}", kind=kind)
        node = {
            "id": ids.card_id(), "type": "text",
            "x": draft.get("x"), "y": draft.get("y"),
            "width": draft.get("width") or DEFAULT_WIDTH,
            "height": draft.get("height") or DEFAULT_HEIGHT,
        }
        if draft.get("color"):
            node["color"] = draft["color"]
        node["text"] = draft.get("content", "")
        node["sp_kind"] = kind
        node["sp_title"] = draft.get("title", "")
        node["sp_created_by"] = actor
        node["sp_rev"] = 1
        if draft.get("meta") is not None:
            node["sp_meta"] = draft["meta"]
        return node

    def _raw_to_node(self, raw: dict, actor: str) -> dict:
        node = {k: v for k, v in raw.items() if k not in IMMUTABLE_KEYS}
        node = {"id": ids.card_id(), **node}
        node.setdefault("type", "text")
        if node.get("sp_kind") and node["sp_kind"] not in KINDS:
            raise UnsupportedKind(f"sp_kind must be one of {', '.join(KINDS)}",
                                  kind=node["sp_kind"])
        if node["type"] != "text":
            node.pop("sp_kind", None)
        node["width"] = node.get("width") or DEFAULT_WIDTH
        node["height"] = node.get("height") or DEFAULT_HEIGHT
        node["sp_created_by"] = actor
        node["sp_rev"] = 1
        return node

    def create_cards(self, slug: str, *, drafts=None, nodes=None,
                     actor: str, actor_kind: str) -> list[dict]:
        space_id = self.space_row(slug)["id"]
        if (drafts is None) == (nodes is None):
            raise ValidationFailed("provide exactly one of `cards` or `nodes`")

        built = ([self._draft_to_node(d, actor) for d in drafts] if drafts is not None
                 else [self._raw_to_node(n, actor) for n in nodes])
        return self._insert_nodes(space_id, built, actor=actor, actor_kind=actor_kind)

    def _insert_nodes(self, space_id: str, built: list[dict], *, actor: str,
                      actor_kind: str, in_transaction: bool = False) -> list[dict]:
        next_x, top = self._layout_cursor(space_id)
        next_y = top
        column_width = 0.0
        for node in built:
            if node.get("x") is None or node.get("y") is None:
                if next_y > top and next_y + node["height"] > top + LAYOUT_MAX_COLUMN:
                    next_x += column_width + LAYOUT_GAP
                    next_y = top
                    column_width = 0.0
                node["x"], node["y"] = next_x, next_y
                next_y += node["height"] + LAYOUT_GAP
                column_width = max(column_width, node["width"])

        def run():
            ts = now()
            for node in built:
                self.conn.execute(
                    "INSERT INTO card (id, space_id, node_json, rev, created_by,"
                    " updated_at) VALUES (?,?,?,1,?,?)",
                    (node["id"], space_id, json.dumps(node, ensure_ascii=False),
                     actor, ts))
                self.emit(space_id, "card.created", node["id"], actor, actor_kind,
                          {"title": node.get("sp_title", ""),
                           "kind": node.get("sp_kind") or node["type"]})

        if in_transaction:
            run()
        else:
            with self.write():
                run()
        return built

    def update_card(self, slug: str, card_id: str, patch: dict, *, actor: str,
                    actor_kind: str, mode: str | None = None,
                    if_match: int | None = None) -> dict:
        space = self.space_row(slug)
        row = self._card_row(space["id"], card_id)
        node = self._node(row)

        applied = {k: v for k, v in patch.items() if k not in IMMUTABLE_KEYS}
        if not applied:
            raise ValidationFailed("patch is empty", ignored=sorted(set(patch) & IMMUTABLE_KEYS))
        if if_match is not None and if_match != row["rev"]:
            raise Conflict("If-Match did not match the current sp_rev",
                           current=node, expected=if_match, actual=row["rev"])

        geometry_only = set(applied) <= GEOMETRY_KEYS
        if mode is not None and mode not in ("replace", "branch"):
            raise ValidationFailed("mode must be 'replace' or 'branch'")
        effective = mode or space["revision_mode"]

        if geometry_only:
            return self._move_card(space["id"], row, node, applied, actor, actor_kind)
        if effective == "branch":
            return self._branch_card(space["id"], row, node, applied, actor, actor_kind)
        return self._replace_card(space["id"], row, node, applied, actor, actor_kind)

    def _move_card(self, space_id, row, node, applied, actor, actor_kind) -> dict:
        before = [node.get("x"), node.get("y")]
        node.update(applied)
        with self.write():
            self.conn.execute("UPDATE card SET node_json = ?, updated_at = ? WHERE id = ?",
                              (json.dumps(node, ensure_ascii=False), now(), row["id"]))
            self.emit(space_id, "card.moved", row["id"], actor, actor_kind,
                      {"from": before, "to": [node.get("x"), node.get("y")]})
        return node

    def _replace_card(self, space_id, row, node, applied, actor, actor_kind) -> dict:
        rev = row["rev"] + 1
        node.update(applied)
        node["sp_rev"] = rev
        with self.write():
            self.conn.execute(
                "UPDATE card SET node_json = ?, rev = ?, updated_at = ? WHERE id = ?",
                (json.dumps(node, ensure_ascii=False), rev, now(), row["id"]))
            self.emit(space_id, "card.updated", row["id"], actor, actor_kind,
                      {"changed": sorted(applied), "rev": rev})
        return node

    def _branch_card(self, space_id, row, node, applied, actor, actor_kind) -> dict:
        """SPEC §2.4. Emits card.created + link.created; the superseded card's rev is
        never touched, which is what keeps its annotations from going stale."""
        if node.get("sp_superseded_by"):
            raise Conflict("this card has already been superseded",
                           current=node, superseded_by=node["sp_superseded_by"])

        new = {k: v for k, v in node.items() if k not in IMMUTABLE_KEYS}
        new.update(applied)
        new = {"id": ids.card_id(), **new}
        new["sp_created_by"] = actor
        new["sp_rev"] = 1
        if "x" not in applied or "y" not in applied:
            new["x"], new["y"] = None, None

        node["sp_superseded_by"] = new["id"]
        with self.write():
            self.conn.execute("UPDATE card SET node_json = ?, updated_at = ? WHERE id = ?",
                              (json.dumps(node, ensure_ascii=False), now(), row["id"]))
            self._insert_nodes(space_id, [new], actor=actor, actor_kind=actor_kind,
                               in_transaction=True)
            self._insert_links(space_id, [{"fromNode": row["id"], "toNode": new["id"],
                                           "label": "revised"}],
                               actor=actor, actor_kind=actor_kind, in_transaction=True)
        return new

    def delete_card(self, slug: str, card_id: str, *, actor: str, actor_kind: str) -> None:
        space_id = self.space_row(slug)["id"]
        row = self._card_row(space_id, card_id)
        node = self._node(row)
        with self.write():
            self.conn.execute("UPDATE card SET deleted_at = ?, updated_at = ? WHERE id = ?",
                              (now(), now(), card_id))
            self.emit(space_id, "card.deleted", card_id, actor, actor_kind,
                      {"title": node.get("sp_title", "")})

    # --- links ---------------------------------------------------------------

    def _insert_links(self, space_id: str, edges: list[dict], *, actor: str,
                      actor_kind: str, in_transaction: bool = False,
                      known_ids: set[str] | None = None) -> list[dict]:
        live = known_ids if known_ids is not None else {
            r["id"] for r in self.conn.execute(
                "SELECT id FROM card WHERE space_id = ? AND deleted_at IS NULL", (space_id,))}

        built = []
        for edge in edges:
            for side in ("fromNode", "toNode"):
                if edge.get(side) not in live:
                    raise NotFound(f"{side} {edge.get(side)!r} is not a live card in this space")
            out = {k: v for k, v in edge.items() if k != "id" and v is not None}
            out = {"id": ids.link_id(), **out}
            out["sp_created_by"] = actor
            built.append(out)

        def run():
            ts = now()
            for out in built:
                self.conn.execute(
                    "INSERT INTO link (id, space_id, edge_json, created_by, updated_at)"
                    " VALUES (?,?,?,?,?)",
                    (out["id"], space_id, json.dumps(out, ensure_ascii=False), actor, ts))
                self.emit(space_id, "link.created", out["id"], actor, actor_kind,
                          {"from": out["fromNode"], "to": out["toNode"],
                           "label": out.get("label")})

        if in_transaction:
            run()
        else:
            with self.write():
                run()
        return built

    def create_links(self, slug: str, edges: list[dict], *, actor: str,
                     actor_kind: str) -> list[dict]:
        space_id = self.space_row(slug)["id"]
        return self._insert_links(space_id, edges, actor=actor, actor_kind=actor_kind)

    def delete_link(self, slug: str, link_id: str, *, actor: str, actor_kind: str) -> None:
        space_id = self.space_row(slug)["id"]
        row = self.conn.execute(
            "SELECT * FROM link WHERE id = ? AND space_id = ? AND deleted_at IS NULL",
            (link_id, space_id)).fetchone()
        if row is None:
            raise NotFound(f"no link {link_id!r} in this space")
        with self.write():
            self.conn.execute("UPDATE link SET deleted_at = ?, updated_at = ? WHERE id = ?",
                              (now(), now(), link_id))
            self.emit(space_id, "link.deleted", link_id, actor, actor_kind, None)

    # --- import (SPEC §3: additive only) -------------------------------------

    def import_canvas(self, slug: str, canvas: dict, *, actor: str,
                      actor_kind: str) -> dict:
        space_id = self.space_row(slug)["id"]
        incoming_nodes = canvas.get("nodes") or []
        incoming_edges = canvas.get("edges") or []

        id_map: dict[str, str] = {}
        built_nodes = []
        for raw in incoming_nodes:
            node = self._raw_to_node(raw, actor)
            if raw.get("id"):
                id_map[raw["id"]] = node["id"]
            built_nodes.append(node)

        live = {r["id"] for r in self.conn.execute(
            "SELECT id FROM card WHERE space_id = ? AND deleted_at IS NULL", (space_id,))}
        known = live | {n["id"] for n in built_nodes}

        remapped_edges = []
        for edge in incoming_edges:
            out = {k: v for k, v in edge.items() if k != "id"}
            for side in ("fromNode", "toNode"):
                original = edge.get(side)
                out[side] = id_map.get(original, original)
                if out[side] not in known:
                    raise ValidationFailed(
                        f"edge {edge.get('id')!r} references unknown node {original!r}")
            remapped_edges.append((edge.get("id"), out))

        with self.write():
            self._insert_nodes(space_id, built_nodes, actor=actor, actor_kind=actor_kind,
                               in_transaction=True)
            built_edges = self._insert_links(
                space_id, [e for _, e in remapped_edges], actor=actor,
                actor_kind=actor_kind, in_transaction=True, known_ids=known)
        for (original, _), built in zip(remapped_edges, built_edges):
            if original:
                id_map[original] = built["id"]
        return {"id_map": id_map, "canvas": {"nodes": built_nodes, "edges": built_edges}}

    # --- annotations ---------------------------------------------------------

    def _annotation(self, row: sqlite3.Row, card_index: dict[str, sqlite3.Row]) -> dict:
        card = card_index.get(row["card_id"])
        node = json.loads(card["node_json"]) if card else {}
        out = {
            "id": row["id"], "card_id": row["card_id"],
            "card_title": node.get("sp_title", ""),
            "card_rev": row["card_rev"],
            "selector": json.loads(row["selector"]) if row["selector"] else None,
            "body": row["body"], "motivation": row["motivation"],
            "creator": row["creator"], "creator_kind": row["creator_kind"],
            "resolved": bool(row["resolved"]),
            "resolved_reply": row["resolved_reply"],
            "stale": bool(card) and row["card_rev"] < card["rev"],
            "created_at": row["created_at"],
        }
        # Only present when there is a chain to follow, so a current card's
        # annotation keeps the exact shape the fixtures pin.
        if node.get("sp_superseded_by"):
            out["card_superseded_by"] = node["sp_superseded_by"]
        return out

    def _card_index(self, space_id: str) -> dict[str, sqlite3.Row]:
        return {r["id"]: r for r in self.conn.execute(
            "SELECT * FROM card WHERE space_id = ?", (space_id,))}

    def annotations(self, slug: str, *, resolved: bool | None = None,
                    card_id: str | None = None) -> list[dict]:
        space_id = self.space_row(slug)["id"]
        sql = "SELECT * FROM annotation WHERE space_id = ?"
        args: list[Any] = [space_id]
        if resolved is not None:
            sql += " AND resolved = ?"
            args.append(1 if resolved else 0)
        if card_id is not None:
            sql += " AND card_id = ?"
            args.append(card_id)
        index = self._card_index(space_id)
        return [self._annotation(r, index)
                for r in self.conn.execute(sql + " ORDER BY rowid", args)]

    def create_annotation(self, slug: str, *, card_id: str, body: str,
                          selector: dict | None = None, motivation: str = "commenting",
                          actor: str, actor_kind: str) -> dict:
        space_id = self.space_row(slug)["id"]
        card = self._card_row(space_id, card_id, allow_deleted=True)
        if motivation not in MOTIVATIONS:
            raise ValidationFailed(f"motivation must be one of {', '.join(MOTIVATIONS)}")
        annotation_id = ids.annotation_id()
        with self.write():
            self.conn.execute(
                "INSERT INTO annotation (id, space_id, card_id, card_rev, selector, body,"
                " motivation, creator, creator_kind, resolved, created_at)"
                " VALUES (?,?,?,?,?,?,?,?,?,0,?)",
                (annotation_id, space_id, card_id, card["rev"],
                 json.dumps(selector) if selector is not None else None,
                 body, motivation, actor, actor_kind, now()))
            self.emit(space_id, "annotation.created", annotation_id, actor, actor_kind,
                      {"card_id": card_id})
        row = self.conn.execute("SELECT * FROM annotation WHERE id = ?",
                                (annotation_id,)).fetchone()
        return self._annotation(row, self._card_index(space_id))

    def resolve_annotation(self, slug: str, annotation_id: str, *, resolved: bool = True,
                           reply: str | None = None, actor: str, actor_kind: str) -> dict:
        space_id = self.space_row(slug)["id"]
        row = self.conn.execute("SELECT * FROM annotation WHERE id = ? AND space_id = ?",
                                (annotation_id, space_id)).fetchone()
        if row is None:
            raise NotFound(f"no annotation {annotation_id!r} in this space")
        with self.write():
            if resolved:
                self.conn.execute(
                    "UPDATE annotation SET resolved = 1, resolved_reply = ?,"
                    " resolved_at = ? WHERE id = ?", (reply, now(), annotation_id))
                self.emit(space_id, "annotation.resolved", annotation_id, actor,
                          actor_kind, {"reply": reply})
            else:
                # There is no annotation.reopened event type, so reopening is silent.
                self.conn.execute(
                    "UPDATE annotation SET resolved = 0, resolved_reply = NULL,"
                    " resolved_at = NULL WHERE id = ?", (annotation_id,))
        row = self.conn.execute("SELECT * FROM annotation WHERE id = ?",
                                (annotation_id,)).fetchone()
        return self._annotation(row, self._card_index(space_id))

    # --- events --------------------------------------------------------------

    def events(self, space_id: str, *, since: int = 0, limit: int = 200) -> list[dict]:
        rows = self.conn.execute(
            "SELECT * FROM event WHERE space_id = ? AND seq > ? ORDER BY seq LIMIT ?",
            (space_id, since, limit)).fetchall()
        out = []
        for r in rows:
            event = {"seq": r["seq"], "ts": r["ts"], "type": r["type"],
                     "subject_id": r["subject_id"], "actor": r["actor"],
                     "actor_kind": r["actor_kind"]}
            if r["payload"] is not None:
                event["payload"] = json.loads(r["payload"])
            out.append(event)
        return out

    def list_events(self, slug: str, *, since: int = 0, limit: int = 200) -> dict:
        space_id = self.space_row(slug)["id"]
        events = self.events(space_id, since=since, limit=limit)
        return {"events": events, "cursor": events[-1]["seq"] if events else since}

    # --- media ---------------------------------------------------------------

    def save_media(self, slug: str, *, filename: str, content_type: str,
                   data: bytes) -> dict:
        space_id = self.space_row(slug)["id"]
        suffix = config.MEDIA_EXTENSIONS.get((content_type or "").split(";")[0].strip())
        if suffix is None:
            raise UnsupportedKind(
                f"unsupported content type {content_type!r}",
                supported=sorted(config.MEDIA_EXTENSIONS))
        if len(data) > config.MAX_UPLOAD_BYTES:
            raise ValidationFailed(f"upload exceeds {config.MAX_UPLOAD_BYTES} bytes")

        # The client's filename is advisory: the stored name is server-assigned, so a
        # traversal attempt cannot reach the filesystem.
        name = f"{ids.media_id()}{suffix}"
        target = self.media_root / space_id
        target.mkdir(parents=True, exist_ok=True)
        (target / name).write_bytes(data)
        return {"url": f"{config.API_PREFIX}/spaces/{slug}/media/{name}",
                "content_type": content_type, "bytes": len(data)}

    def media_path(self, slug: str, name: str):
        space_id = self.space_row(slug)["id"]
        if not re.fullmatch(r"[A-Za-z0-9_.-]{1,128}", name) or ".." in name:
            raise NotFound("no such media")
        path = self.media_root / space_id / name
        if not path.is_file():
            raise NotFound("no such media")
        suffix = path.suffix.lower()
        content_type = next(
            (ct for ct, ext in config.MEDIA_EXTENSIONS.items() if ext == suffix),
            "application/octet-stream")
        return path, content_type

    # --- feedback (SPEC §4.1) ------------------------------------------------

    def feedback(self, slug: str, *, actor: str, since: int | None = None,
                 advance: bool = True) -> dict:
        space = self.space_row(slug)
        space_id = space["id"]

        if since is None:
            row = self.conn.execute(
                "SELECT seq FROM actor_cursor WHERE space_id = ? AND actor = ?",
                (space_id, actor)).fetchone()
            since = row["seq"] if row else 0

        index = self._card_index(space_id)
        annotations = [self._annotation(r, index) for r in self.conn.execute(
            "SELECT * FROM annotation WHERE space_id = ? AND resolved = 0 ORDER BY rowid",
            (space_id,))]

        edited: dict[str, dict] = {}
        moved: dict[str, dict] = {}
        deleted: dict[str, dict] = {}
        added: dict[str, dict] = {}
        removed: dict[str, dict] = {}

        def title(subject_id: str, event: dict) -> str:
            card = index.get(subject_id)
            if card:
                return json.loads(card["node_json"]).get("sp_title", "")
            return (event.get("payload") or {}).get("title", "")

        for event in self.events(space_id, since=since, limit=1_000_000):
            if event["actor"] == actor:
                continue                       # SPEC §10: never read your own writes back
            subject, payload = event["subject_id"], event.get("payload") or {}
            match event["type"]:
                case "card.updated":
                    row = edited.setdefault(
                        subject, {"id": subject, "title": title(subject, event),
                                  "changed": [], "actor": event["actor"]})
                    row["changed"] = sorted(set(row["changed"]) | set(payload.get("changed", [])))
                    row["actor"] = event["actor"]
                case "card.moved":
                    moved[subject] = {"id": subject, "title": title(subject, event),
                                      "actor": event["actor"]}
                case "card.deleted":
                    deleted[subject] = {"id": subject, "title": title(subject, event),
                                        "actor": event["actor"]}
                case "link.created":
                    edge = payload
                    if not edge:
                        link = self.conn.execute("SELECT edge_json FROM link WHERE id = ?",
                                                 (subject,)).fetchone()
                        raw = json.loads(link["edge_json"]) if link else {}
                        edge = {"from": raw.get("fromNode"), "to": raw.get("toNode"),
                                "label": raw.get("label")}
                    row = {"id": subject, "from": edge.get("from"), "to": edge.get("to"),
                           "actor": event["actor"]}
                    if edge.get("label") is not None:
                        row["label"] = edge["label"]
                    added[subject] = row
                case "link.deleted":
                    removed[subject] = {"id": subject, "actor": event["actor"]}

        # One row per subject, strongest signal wins.
        for subject in deleted:
            edited.pop(subject, None)
            moved.pop(subject, None)
        for subject in edited:
            moved.pop(subject, None)
        for subject in set(added) & set(removed):
            added.pop(subject)
            removed.pop(subject)

        cursor = space["seq"]
        if advance:
            with self.write():
                self.conn.execute(
                    "INSERT INTO actor_cursor (space_id, actor, seq) VALUES (?,?,?)"
                    " ON CONFLICT(space_id, actor) DO UPDATE SET seq = excluded.seq",
                    (space_id, actor, cursor))

        result = {
            "cursor": cursor,
            "annotations": annotations,
            "cards_edited": list(edited.values()),
            "cards_deleted": list(deleted.values()),
            "cards_moved": list(moved.values()),
            "links_added": list(added.values()),
            "links_removed": list(removed.values()),
        }
        result["summary"] = summarize(result)
        return result


def _plural(n: int, one: str, many: str) -> str:
    return f"{n} {one if n == 1 else many}"


def summarize(feedback: dict) -> str:
    """The grammar pinned by contracts/fixtures/feedback.claude-code.since-12.json."""
    parts: list[str] = []
    annotations = feedback["annotations"]
    if annotations:
        part = _plural(len(annotations), "open comment", "open comments")
        stale = sum(1 for a in annotations if a["stale"])
        if stale:
            part += f" ({stale} stale)"
        parts.append(part)
    if feedback["cards_edited"]:
        parts.append(_plural(len(feedback["cards_edited"]), "card edited", "cards edited"))
    if feedback["cards_deleted"]:
        parts.append(f"{len(feedback['cards_deleted'])} deleted")
    if feedback["cards_moved"]:
        parts.append(f"{len(feedback['cards_moved'])} moved")
    if feedback["links_added"]:
        parts.append(_plural(len(feedback["links_added"]), "new link", "new links"))
    if feedback["links_removed"]:
        parts.append(_plural(len(feedback["links_removed"]), "link removed", "links removed"))
    return ", ".join(parts) + "." if parts else ""
