"""analog/server/schema.sql is frozen; these tests pin what WP1 may rely on."""

from __future__ import annotations

import sqlite3

import pytest

from analog.server import config

pytestmark = pytest.mark.contract

EXPECTED_COLUMNS = {
    "space": {"id", "slug", "title", "revision_mode", "seq", "created_at"},
    "card": {"id", "space_id", "node_json", "rev", "created_by", "updated_at", "deleted_at"},
    "link": {"id", "space_id", "edge_json", "created_by", "updated_at", "deleted_at"},
    "annotation": {"id", "space_id", "card_id", "card_rev", "selector", "body",
                   "motivation", "creator", "creator_kind", "resolved",
                   "resolved_reply", "resolved_at", "created_at"},
    "event": {"space_id", "seq", "ts", "type", "subject_id", "actor", "actor_kind", "payload"},
    "actor_cursor": {"space_id", "actor", "seq"},
}


@pytest.fixture
def db():
    conn = sqlite3.connect(":memory:")
    conn.executescript(config.SCHEMA_PATH.read_text())
    conn.execute("PRAGMA foreign_keys = ON")
    yield conn
    conn.close()


def test_schema_applies_cleanly(db):
    names = {r[0] for r in db.execute(
        "SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'")}
    assert names == set(EXPECTED_COLUMNS)


@pytest.mark.parametrize("table,columns", sorted(EXPECTED_COLUMNS.items()))
def test_table_columns(db, table, columns):
    actual = {r[1] for r in db.execute(f"PRAGMA table_info({table})")}
    assert actual == columns


def _space(db, sid="s_1", slug="demo", mode="replace"):
    db.execute("INSERT INTO space (id,slug,title,revision_mode,seq,created_at)"
               " VALUES (?,?,?,?,0,'2026-01-01T00:00:00Z')", (sid, slug, "T", mode))
    return sid


def test_slug_is_unique(db):
    _space(db)
    with pytest.raises(sqlite3.IntegrityError):
        _space(db, sid="s_2")


@pytest.mark.parametrize("mode", ["replace", "branch"])
def test_revision_modes_accepted(db, mode):
    _space(db, sid=f"s_{mode}", slug=mode, mode=mode)


def test_revision_mode_check_rejects_others(db):
    with pytest.raises(sqlite3.IntegrityError):
        _space(db, mode="merge")


def test_event_type_check(db):
    sid = _space(db)
    for t in ("card.created", "card.updated", "card.moved", "card.deleted",
              "link.created", "link.deleted", "annotation.created", "annotation.resolved"):
        db.execute("INSERT INTO event (space_id,seq,ts,type,subject_id,actor,actor_kind)"
                   " VALUES (?,?,?,?,?,?,?)",
                   (sid, hash(t) % 10**6, "t", t, "x", "human", "human"))
    with pytest.raises(sqlite3.IntegrityError):
        db.execute("INSERT INTO event (space_id,seq,ts,type,subject_id,actor,actor_kind)"
                   " VALUES (?,999,'t','card.renamed','x','human','human')", (sid,))


def test_event_seq_is_unique_per_space(db):
    sid = _space(db)
    other = _space(db, sid="s_2", slug="two")
    row = "(?,1,'t','card.created','c','human','human')"
    db.execute(f"INSERT INTO event (space_id,seq,ts,type,subject_id,actor,actor_kind) VALUES {row}", (sid,))
    db.execute(f"INSERT INTO event (space_id,seq,ts,type,subject_id,actor,actor_kind) VALUES {row}", (other,))
    with pytest.raises(sqlite3.IntegrityError):
        db.execute(f"INSERT INTO event (space_id,seq,ts,type,subject_id,actor,actor_kind) VALUES {row}", (sid,))


def test_actor_kind_and_motivation_checks(db):
    sid = _space(db)
    db.execute("INSERT INTO card (id,space_id,node_json,rev,created_by,updated_at)"
               " VALUES ('c_1',?,'{}',1,'human','t')", (sid,))
    ok = ("INSERT INTO annotation (id,space_id,card_id,card_rev,body,motivation,"
          "creator,creator_kind,created_at) VALUES (?,?, 'c_1',1,'b',?,'human',?,'t')")
    for m in ("commenting", "assessing", "editing"):
        db.execute(ok, (f"a_{m}", sid, m, "human"))
    with pytest.raises(sqlite3.IntegrityError):
        db.execute(ok, ("a_bad", sid, "praising", "human"))
    with pytest.raises(sqlite3.IntegrityError):
        db.execute(ok, ("a_bad2", sid, "editing", "robot"))


def test_deleting_a_space_cascades(db):
    sid = _space(db)
    db.execute("INSERT INTO card (id,space_id,node_json,rev,created_by,updated_at)"
               " VALUES ('c_1',?,'{}',1,'human','t')", (sid,))
    db.execute("INSERT INTO actor_cursor (space_id,actor,seq) VALUES (?,'a',3)", (sid,))
    db.execute("DELETE FROM space WHERE id = ?", (sid,))
    assert db.execute("SELECT count(*) FROM card").fetchone()[0] == 0
    assert db.execute("SELECT count(*) FROM actor_cursor").fetchone()[0] == 0


def test_soft_delete_columns_default_null(db):
    sid = _space(db)
    db.execute("INSERT INTO card (id,space_id,node_json,rev,created_by,updated_at)"
               " VALUES ('c_1',?,'{}',1,'human','t')", (sid,))
    assert db.execute("SELECT deleted_at FROM card").fetchone()[0] is None
