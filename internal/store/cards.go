package store

import (
	"database/sql"
	"slices"
	"strings"

	"github.com/meowkey-dev/analog/internal/apierr"
	"github.com/meowkey-dev/analog/internal/ids"
)

// Node is a JSON Canvas node plus its sp_* extensions, kept as a free-form map so
// keys the contract does not name survive a round trip verbatim.
type Node = map[string]any

// Canvas is what GET /canvas returns.
type Canvas struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

type cardRow struct {
	ID        string
	SpaceID   string
	NodeJSON  string
	Rev       int64
	CreatedBy string
	UpdatedAt string
	DeletedAt sql.NullString
}

const cardColumns = "id, space_id, node_json, rev, created_by, updated_at, deleted_at"

func scanCard(scan func(...any) error) (cardRow, error) {
	var r cardRow
	err := scan(&r.ID, &r.SpaceID, &r.NodeJSON, &r.Rev, &r.CreatedBy, &r.UpdatedAt, &r.DeletedAt)
	return r, err
}

func cardRowByID(q querier, spaceID, cardID string, allowDeleted bool) (cardRow, error) {
	row := q.QueryRow("SELECT "+cardColumns+" FROM card WHERE id = ? AND space_id = ?",
		cardID, spaceID)
	r, err := scanCard(row.Scan)
	if err == sql.ErrNoRows || (err == nil && r.DeletedAt.Valid && !allowDeleted) {
		return r, apierr.NotFound("no card '" + cardID + "' in this space")
	}
	return r, err
}

// node rebuilds the stored blob, projecting the two fields that live on the row.
//
// sp_deleted_at is never stored in the blob, only projected here, so GET /canvas
// cannot leak a tombstone.
func (r cardRow) node(includeDeleted bool) (Node, error) {
	n, err := decodeObject([]byte(r.NodeJSON))
	if err != nil {
		return nil, err
	}
	n["sp_rev"] = r.Rev
	if includeDeleted && r.DeletedAt.Valid {
		n["sp_deleted_at"] = r.DeletedAt.String
	}
	return n, nil
}

func (s *Store) Canvas(slug string, includeDeleted bool) (Canvas, error) {
	out := Canvas{Nodes: []Node{}, Edges: []Edge{}}
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return out, err
	}
	where := " AND deleted_at IS NULL"
	if includeDeleted {
		where = ""
	}

	// ORDER BY rowid, i.e. insertion order: the fixtures' readable ids do not sort
	// into creation order, so ordering by id would not round-trip them.
	rows, err := s.read.Query(
		"SELECT "+cardColumns+" FROM card WHERE space_id = ?"+where+" ORDER BY rowid",
		space.ID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		r, err := scanCard(rows.Scan)
		if err != nil {
			rows.Close()
			return out, err
		}
		n, err := r.node(includeDeleted)
		if err != nil {
			rows.Close()
			return out, err
		}
		out.Nodes = append(out.Nodes, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, err
	}

	edges, err := s.read.Query(
		"SELECT edge_json FROM link WHERE space_id = ?"+where+" ORDER BY rowid", space.ID)
	if err != nil {
		return out, err
	}
	defer edges.Close()
	for edges.Next() {
		var raw string
		if err := edges.Scan(&raw); err != nil {
			return out, err
		}
		edge, err := decodeObject([]byte(raw))
		if err != nil {
			return out, err
		}
		out.Edges = append(out.Edges, edge)
	}
	return out, edges.Err()
}

// --- layout ------------------------------------------------------------------

// layoutCursor is SPEC §5: a column to the right of the live bounding box, top-down.
func layoutCursor(q querier, spaceID string) (nextX, top float64, err error) {
	rows, err := q.Query(
		"SELECT node_json FROM card WHERE space_id = ? AND deleted_at IS NULL", spaceID)
	if err != nil {
		return 0, 0, err
	}
	defer rows.Close()

	first := true
	var right float64
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return 0, 0, err
		}
		box, err := decodeObject([]byte(raw))
		if err != nil {
			return 0, 0, err
		}
		x, _ := numberOf(box["x"])
		y, _ := numberOf(box["y"])
		width, ok := numberOf(box["width"])
		if !ok {
			width = DefaultWidth
		}
		if first {
			right, top, first = x+width, y, false
			continue
		}
		right = max(right, x+width)
		top = min(top, y)
	}
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	if first {
		// An empty space: the first card lands at (0,0).
		return 0, 0, nil
	}
	return right + LayoutGap, top, nil
}

// --- building nodes ----------------------------------------------------------

// CardDraft is the friendly card body: POST /cards with `cards`.
type CardDraft struct {
	Title   string         `json:"title"`
	Content string         `json:"content"`
	Kind    string         `json:"kind"`
	X       *float64       `json:"x"`
	Y       *float64       `json:"y"`
	Width   *float64       `json:"width"`
	Height  *float64       `json:"height"`
	Color   string         `json:"color"`
	Meta    map[string]any `json:"meta"`
}

func draftToNode(d CardDraft, actor string) (Node, error) {
	kind := d.Kind
	if kind == "" {
		kind = "md"
	}
	if !contains(kinds, kind) {
		return nil, apierr.UnsupportedKind(
			"kind must be one of md, html, svg, plain", apierr.Detail{"kind": kind})
	}
	n := Node{"id": ids.CardID(), "type": "text"}
	// nil x/y mean "place it for me"; the key is present either way so the layout
	// pass can tell "unset" from "explicitly 0".
	n["x"] = optional(d.X)
	n["y"] = optional(d.Y)
	n["width"] = orDefault(d.Width, DefaultWidth)
	n["height"] = orDefault(d.Height, DefaultHeight)
	if d.Color != "" {
		n["color"] = d.Color
	}
	n["text"] = d.Content
	n["sp_kind"] = kind
	n["sp_title"] = d.Title
	n["sp_created_by"] = actor
	n["sp_rev"] = 1
	if d.Meta != nil {
		n["sp_meta"] = d.Meta
	}
	return n, nil
}

func optional(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// orDefault mirrors Python's `draft.get("width") or DEFAULT_WIDTH`: a missing value
// and an explicit zero both take the default, because a zero-width card is a bug.
func orDefault(v *float64, fallback float64) float64 {
	if v == nil || *v == 0 {
		return fallback
	}
	return *v
}

// rawToNode takes a client's JSON Canvas node. Clients never choose ids, so the
// incoming one is discarded and a fresh one assigned.
func rawToNode(raw Node, actor string) (Node, error) {
	n := Node{"id": ids.CardID()}
	for k, v := range raw {
		if !immutableKeys[k] {
			n[k] = v
		}
	}
	if _, ok := n["type"]; !ok {
		n["type"] = "text"
	}
	if kind := stringOf(n["sp_kind"]); kind != "" && !contains(kinds, kind) {
		return nil, apierr.UnsupportedKind(
			"sp_kind must be one of md, html, svg, plain", apierr.Detail{"kind": kind})
	}
	if stringOf(n["type"]) != "text" {
		delete(n, "sp_kind")
	}
	if width, ok := numberOf(n["width"]); !ok || width == 0 {
		n["width"] = float64(DefaultWidth)
	}
	if height, ok := numberOf(n["height"]); !ok || height == 0 {
		n["height"] = float64(DefaultHeight)
	}
	n["sp_created_by"] = actor
	n["sp_rev"] = 1
	return n, nil
}

// CreateCards takes exactly one of drafts or nodes; nil means "not supplied".
func (s *Store) CreateCards(slug string, drafts []CardDraft, nodes []Node,
	actor, actorKind string) ([]Node, error) {
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return nil, err
	}
	if (drafts == nil) == (nodes == nil) {
		return nil, apierr.ValidationFailed("provide exactly one of `cards` or `nodes`")
	}

	built := make([]Node, 0, len(drafts)+len(nodes))
	if drafts != nil {
		for _, d := range drafts {
			n, err := draftToNode(d, actor)
			if err != nil {
				return nil, err
			}
			built = append(built, n)
		}
	} else {
		for _, raw := range nodes {
			n, err := rawToNode(raw, actor)
			if err != nil {
				return nil, err
			}
			built = append(built, n)
		}
	}

	err = s.withWrite(func(t *tx) error {
		return insertNodes(t, space.ID, built, actor, actorKind)
	})
	if err != nil {
		return nil, err
	}
	return built, nil
}

// insertNodes places any node without coordinates, then writes the batch.
func insertNodes(t *tx, spaceID string, built []Node, actor, actorKind string) error {
	nextX, top, err := layoutCursor(t, spaceID)
	if err != nil {
		return err
	}
	nextY := top
	columnWidth := 0.0
	for _, n := range built {
		if n["x"] != nil && n["y"] != nil {
			continue
		}
		height, _ := numberOf(n["height"])
		width, _ := numberOf(n["width"])
		// Wrap into a new column rather than growing one unreadably tall one.
		if nextY > top && nextY+height > top+LayoutMaxColumn {
			nextX += columnWidth + LayoutGap
			nextY = top
			columnWidth = 0
		}
		n["x"], n["y"] = nextX, nextY
		nextY += height + LayoutGap
		columnWidth = max(columnWidth, width)
	}

	ts := Now()
	for _, n := range built {
		if _, err := t.Exec(
			"INSERT INTO card (id, space_id, node_json, rev, created_by, updated_at)"+
				" VALUES (?,?,?,1,?,?)",
			n["id"], spaceID, mustEncode(n), actor, ts); err != nil {
			return err
		}
		kind := stringOf(n["sp_kind"])
		if kind == "" {
			kind = stringOf(n["type"])
		}
		if _, err := t.emit(spaceID, "card.created", stringOf(n["id"]), actor, actorKind,
			map[string]any{"title": stringOf(n["sp_title"]), "kind": kind}); err != nil {
			return err
		}
	}
	return nil
}

// --- updating ----------------------------------------------------------------

// UpdateCard applies a patch. mode is "" for the space's default; ifMatch is nil
// for an unconditional write.
func (s *Store) UpdateCard(slug, cardID string, patch Node, actor, actorKind, mode string,
	ifMatch *int64) (Node, error) {
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return nil, err
	}
	row, err := cardRowByID(s.read, space.ID, cardID, false)
	if err != nil {
		return nil, err
	}
	node, err := row.node(false)
	if err != nil {
		return nil, err
	}

	applied := Node{}
	var ignored []string
	for k, v := range patch {
		if immutableKeys[k] {
			ignored = append(ignored, k)
			continue
		}
		applied[k] = v
	}
	if len(applied) == 0 {
		slices.Sort(ignored)
		if ignored == nil {
			ignored = []string{}
		}
		return nil, apierr.ValidationFailed("patch is empty", apierr.Detail{"ignored": ignored})
	}
	// The contract gives sp_kind an enum, and creation has always enforced it.
	// A patch did not, so `PATCH {"sp_kind": "bogus"}` could put a value on the
	// canvas that the server's own Node schema rejects.
	if kind, present := applied["sp_kind"]; present {
		if name := stringOf(kind); !contains(kinds, name) {
			return nil, apierr.UnsupportedKind(
				"sp_kind must be one of "+strings.Join(kinds, ", "),
				apierr.Detail{"kind": kind})
		}
	}
	if ifMatch != nil && *ifMatch != row.Rev {
		return nil, apierr.Conflict("If-Match did not match the current sp_rev",
			apierr.Detail{"current": node, "expected": *ifMatch, "actual": row.Rev})
	}

	geometryOnly := true
	for k := range applied {
		if !geometryKeys[k] {
			geometryOnly = false
			break
		}
	}
	if mode != "" && mode != "replace" && mode != "branch" {
		return nil, apierr.ValidationFailed("mode must be 'replace' or 'branch'")
	}
	effective := mode
	if effective == "" {
		effective = space.RevisionMode
	}

	switch {
	case geometryOnly:
		return s.moveCard(space.ID, row, node, applied, actor, actorKind)
	case effective == "branch":
		return s.branchCard(space.ID, row, node, applied, actor, actorKind)
	default:
		return s.replaceCard(space.ID, row, node, applied, actor, actorKind)
	}
}

// moveCard does not bump rev, which is what stops the human rearranging the canvas
// from making an agent's annotations stale (schema.sql notes 1-2).
func (s *Store) moveCard(spaceID string, row cardRow, node, applied Node,
	actor, actorKind string) (Node, error) {
	before := []any{node["x"], node["y"]}
	for k, v := range applied {
		node[k] = v
	}
	err := s.withWrite(func(t *tx) error {
		if _, err := t.Exec("UPDATE card SET node_json = ?, updated_at = ? WHERE id = ?",
			mustEncode(node), Now(), row.ID); err != nil {
			return err
		}
		_, err := t.emit(spaceID, "card.moved", row.ID, actor, actorKind,
			map[string]any{"from": before, "to": []any{node["x"], node["y"]}})
		return err
	})
	if err != nil {
		return nil, err
	}
	return node, nil
}

func (s *Store) replaceCard(spaceID string, row cardRow, node, applied Node,
	actor, actorKind string) (Node, error) {
	rev := row.Rev + 1
	changed := make([]string, 0, len(applied))
	for k, v := range applied {
		node[k] = v
		changed = append(changed, k)
	}
	slices.Sort(changed)
	node["sp_rev"] = rev
	err := s.withWrite(func(t *tx) error {
		if _, err := t.Exec(
			"UPDATE card SET node_json = ?, rev = ?, updated_at = ? WHERE id = ?",
			mustEncode(node), rev, Now(), row.ID); err != nil {
			return err
		}
		_, err := t.emit(spaceID, "card.updated", row.ID, actor, actorKind,
			map[string]any{"changed": changed, "rev": rev})
		return err
	})
	if err != nil {
		return nil, err
	}
	return node, nil
}

// branchCard is SPEC §2.4. It emits card.created + link.created and no card.updated
// (amendment 4); the superseded card's rev is never touched, which is what keeps its
// annotations from going stale.
func (s *Store) branchCard(spaceID string, row cardRow, node, applied Node,
	actor, actorKind string) (Node, error) {
	if superseded := stringOf(node["sp_superseded_by"]); superseded != "" {
		return nil, apierr.Conflict("this card has already been superseded",
			apierr.Detail{"current": node, "superseded_by": superseded})
	}

	next := Node{}
	for k, v := range node {
		if !immutableKeys[k] {
			next[k] = v
		}
	}
	_, hasX := applied["x"]
	_, hasY := applied["y"]
	for k, v := range applied {
		next[k] = v
	}
	next["id"] = ids.CardID()
	next["sp_created_by"] = actor
	next["sp_rev"] = 1
	if !hasX || !hasY {
		// Auto-placed rather than stacked on the card it supersedes.
		next["x"], next["y"] = nil, nil
	}
	node["sp_superseded_by"] = next["id"]

	err := s.withWrite(func(t *tx) error {
		if _, err := t.Exec("UPDATE card SET node_json = ?, updated_at = ? WHERE id = ?",
			mustEncode(node), Now(), row.ID); err != nil {
			return err
		}
		if err := insertNodes(t, spaceID, []Node{next}, actor, actorKind); err != nil {
			return err
		}
		_, err := insertLinks(t, spaceID, []Edge{{
			"fromNode": row.ID, "toNode": next["id"], "label": "revised",
		}}, actor, actorKind, nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	return next, nil
}

func (s *Store) DeleteCard(slug, cardID, actor, actorKind string) error {
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return err
	}
	row, err := cardRowByID(s.read, space.ID, cardID, false)
	if err != nil {
		return err
	}
	node, err := row.node(false)
	if err != nil {
		return err
	}
	return s.withWrite(func(t *tx) error {
		if _, err := t.Exec("UPDATE card SET deleted_at = ?, updated_at = ? WHERE id = ?",
			Now(), Now(), cardID); err != nil {
			return err
		}
		_, err := t.emit(space.ID, "card.deleted", cardID, actor, actorKind,
			map[string]any{"title": stringOf(node["sp_title"])})
		return err
	})
}
