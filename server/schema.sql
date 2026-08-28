-- Analog v0.1 — storage schema
-- FROZEN CONTRACT. Changes go through WP0 only.
-- SQLite. Whole JSON Canvas nodes/edges are stored as blobs so this schema
-- does not need migrating when the canvas format or sp_* extensions change.

PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;

CREATE TABLE space (
  id             TEXT PRIMARY KEY,          -- s_<ulid>
  slug           TEXT NOT NULL UNIQUE,      -- [a-z0-9-]{1,64}
  title          TEXT NOT NULL,
  revision_mode  TEXT NOT NULL DEFAULT 'replace'
                 CHECK (revision_mode IN ('replace','branch')),
  seq            INTEGER NOT NULL DEFAULT 0,  -- monotonic event counter
  created_at     TEXT NOT NULL
);

CREATE TABLE card (
  id          TEXT PRIMARY KEY,             -- c_<ulid>
  space_id    TEXT NOT NULL REFERENCES space(id) ON DELETE CASCADE,
  node_json   TEXT NOT NULL,                -- full JSON Canvas node, incl. sp_* keys
  rev         INTEGER NOT NULL DEFAULT 1,   -- mirrors node.sp_rev
  created_by  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  deleted_at  TEXT                          -- soft delete; row is never removed
);
CREATE INDEX card_by_space ON card(space_id, deleted_at);

CREATE TABLE link (
  id          TEXT PRIMARY KEY,             -- l_<ulid>
  space_id    TEXT NOT NULL REFERENCES space(id) ON DELETE CASCADE,
  edge_json   TEXT NOT NULL,                -- full JSON Canvas edge
  created_by  TEXT NOT NULL,
  updated_at  TEXT NOT NULL,
  deleted_at  TEXT
);
CREATE INDEX link_by_space ON link(space_id, deleted_at);

CREATE TABLE annotation (
  id             TEXT PRIMARY KEY,          -- a_<ulid>
  space_id       TEXT NOT NULL REFERENCES space(id) ON DELETE CASCADE,
  card_id        TEXT NOT NULL REFERENCES card(id),
  card_rev       INTEGER NOT NULL,          -- card.rev at time of creation
  selector       TEXT,                      -- NULL = whole card; else JSON, spec 2.3
  body           TEXT NOT NULL,
  motivation     TEXT NOT NULL DEFAULT 'commenting'
                 CHECK (motivation IN ('commenting','assessing','editing')),
  creator        TEXT NOT NULL,
  creator_kind   TEXT NOT NULL CHECK (creator_kind IN ('human','agent')),
  resolved       INTEGER NOT NULL DEFAULT 0,
  resolved_reply TEXT,
  resolved_at    TEXT,
  created_at     TEXT NOT NULL
);
CREATE INDEX annotation_open ON annotation(space_id, resolved);

CREATE TABLE event (
  space_id    TEXT NOT NULL REFERENCES space(id) ON DELETE CASCADE,
  seq         INTEGER NOT NULL,
  ts          TEXT NOT NULL,
  type        TEXT NOT NULL CHECK (type IN (
                'card.created','card.updated','card.moved','card.deleted',
                'link.created','link.deleted',
                'annotation.created','annotation.resolved')),
  subject_id  TEXT NOT NULL,
  actor       TEXT NOT NULL,                -- 'human' or the agent's name
  actor_kind  TEXT NOT NULL CHECK (actor_kind IN ('human','agent')),
  payload     TEXT,                         -- JSON; type-specific, see contracts
  PRIMARY KEY (space_id, seq)
);

CREATE TABLE actor_cursor (
  space_id  TEXT NOT NULL REFERENCES space(id) ON DELETE CASCADE,
  actor     TEXT NOT NULL,
  seq       INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (space_id, actor)
);

-- Notes for implementers (WP1):
--
-- 1. card.moved is emitted when ONLY x/y/width/height changed. Any change touching
--    text/file/sp_* emits card.updated. This is what lets an agent cheaply ignore
--    the human rearranging the canvas.
-- 2. rev increments on card.updated only. card.moved does NOT bump rev, so moving a
--    card never makes its annotations stale.
-- 3. An annotation is stale when annotation.card_rev < card.rev. Computed on read,
--    never stored.
-- 4. In branch mode the superseded card is never written again, so its rev freezes
--    and its annotations can never go stale.
