package store

import (
	"github.com/meowkey-dev/analog/internal/apierr"
	"github.com/meowkey-dev/analog/internal/ids"
)

// Edge is a JSON Canvas edge, free-form for the same reason Node is.
type Edge = map[string]any

// liveCardIDs is the set an edge may point at.
func liveCardIDs(q querier, spaceID string) (map[string]bool, error) {
	rows, err := q.Query(
		"SELECT id FROM card WHERE space_id = ? AND deleted_at IS NULL", spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	live := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		live[id] = true
	}
	return live, rows.Err()
}

// insertLinks writes edges after checking both endpoints are live cards.
//
// known is the permitted endpoint set; nil means "look up the live cards", which is
// what every caller outside import wants.
func insertLinks(t *tx, spaceID string, edges []Edge, actor, actorKind string,
	known map[string]bool) ([]Edge, error) {
	live := known
	if live == nil {
		var err error
		if live, err = liveCardIDs(t, spaceID); err != nil {
			return nil, err
		}
	}

	built := make([]Edge, 0, len(edges))
	for _, edge := range edges {
		for _, side := range []string{"fromNode", "toNode"} {
			if !live[stringOf(edge[side])] {
				return nil, apierr.NotFound(
					side + " '" + stringOf(edge[side]) + "' is not a live card in this space")
			}
		}
		out := Edge{"id": ids.LinkID()}
		for k, v := range edge {
			// A client-supplied id is discarded; explicit nulls are dropped rather
			// than stored as `"label": null` on the edge itself.
			if k != "id" && v != nil {
				out[k] = v
			}
		}
		out["sp_created_by"] = actor
		built = append(built, out)
	}

	ts := Now()
	for _, out := range built {
		if _, err := t.Exec(
			"INSERT INTO link (id, space_id, edge_json, created_by, updated_at)"+
				" VALUES (?,?,?,?,?)",
			out["id"], spaceID, mustEncode(out), actor, ts); err != nil {
			return nil, err
		}
		if _, err := t.emit(spaceID, "link.created", stringOf(out["id"]), actor, actorKind,
			map[string]any{"from": out["fromNode"], "to": out["toNode"],
				"label": out["label"]}); err != nil {
			return nil, err
		}
	}
	return built, nil
}

func (s *Store) CreateLinks(slug string, edges []Edge, actor, actorKind string) ([]Edge, error) {
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return nil, err
	}
	var built []Edge
	err = s.withWrite(func(t *tx) error {
		var err error
		built, err = insertLinks(t, space.ID, edges, actor, actorKind, nil)
		return err
	})
	if err != nil {
		return nil, err
	}
	return built, nil
}

func (s *Store) DeleteLink(slug, linkID, actor, actorKind string) error {
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return err
	}
	var found string
	err = s.read.QueryRow(
		"SELECT id FROM link WHERE id = ? AND space_id = ? AND deleted_at IS NULL",
		linkID, space.ID).Scan(&found)
	if err != nil {
		return apierr.NotFound("no link '" + linkID + "' in this space")
	}
	return s.withWrite(func(t *tx) error {
		if _, err := t.Exec("UPDATE link SET deleted_at = ?, updated_at = ? WHERE id = ?",
			Now(), Now(), linkID); err != nil {
			return err
		}
		_, err := t.emit(space.ID, "link.deleted", linkID, actor, actorKind, nil)
		return err
	})
}

// --- import (SPEC §3: additive only) -----------------------------------------

// ImportResult is what POST /import returns: the remapping, and what was written.
type ImportResult struct {
	IDMap  map[string]string `json:"id_map"`
	Canvas Canvas            `json:"canvas"`
}

func (s *Store) ImportCanvas(slug string, incoming Canvas, actor, actorKind string) (ImportResult, error) {
	out := ImportResult{IDMap: map[string]string{}}
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return out, err
	}

	// Clients never choose ids: every incoming node gets a fresh one, and the map
	// tells the caller what happened to theirs.
	builtNodes := make([]Node, 0, len(incoming.Nodes))
	for _, raw := range incoming.Nodes {
		n, err := rawToNode(raw, actor)
		if err != nil {
			return out, err
		}
		if original := stringOf(raw["id"]); original != "" {
			out.IDMap[original] = stringOf(n["id"])
		}
		builtNodes = append(builtNodes, n)
	}

	live, err := liveCardIDs(s.read, space.ID)
	if err != nil {
		return out, err
	}
	known := map[string]bool{}
	for id := range live {
		known[id] = true
	}
	for _, n := range builtNodes {
		known[stringOf(n["id"])] = true
	}

	// An edge may reference a node from this import or one already in the space;
	// anything else is a validation failure, not a silent drop.
	originals := make([]string, 0, len(incoming.Edges))
	remapped := make([]Edge, 0, len(incoming.Edges))
	for _, edge := range incoming.Edges {
		e := Edge{}
		for k, v := range edge {
			if k != "id" {
				e[k] = v
			}
		}
		for _, side := range []string{"fromNode", "toNode"} {
			original := stringOf(edge[side])
			target := original
			if mapped, ok := out.IDMap[original]; ok {
				target = mapped
			}
			e[side] = target
			if !known[target] {
				return ImportResult{IDMap: map[string]string{}}, apierr.ValidationFailed(
					"edge '" + stringOf(edge["id"]) + "' references unknown node '" +
						original + "'")
			}
		}
		originals = append(originals, stringOf(edge["id"]))
		remapped = append(remapped, e)
	}

	var builtEdges []Edge
	err = s.withWrite(func(t *tx) error {
		if err := insertNodes(t, space.ID, builtNodes, actor, actorKind); err != nil {
			return err
		}
		var err error
		builtEdges, err = insertLinks(t, space.ID, remapped, actor, actorKind, known)
		return err
	})
	if err != nil {
		return ImportResult{IDMap: map[string]string{}}, err
	}
	for i, original := range originals {
		if original != "" {
			out.IDMap[original] = stringOf(builtEdges[i]["id"])
		}
	}
	out.Canvas = Canvas{Nodes: builtNodes, Edges: builtEdges}
	if out.Canvas.Nodes == nil {
		out.Canvas.Nodes = []Node{}
	}
	if out.Canvas.Edges == nil {
		out.Canvas.Edges = []Edge{}
	}
	return out, nil
}
