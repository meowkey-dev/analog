package store

import (
	"database/sql"
	"strings"

	"github.com/meowkey-dev/analog/internal/apierr"
	"github.com/meowkey-dev/analog/internal/ids"
)

type Annotation struct {
	ID        string `json:"id"`
	CardID    string `json:"card_id"`
	CardTitle string `json:"card_title"`
	// Only present when there is a chain to follow, so a current card's annotation
	// keeps the exact shape the fixtures pin.
	CardSupersededBy string `json:"card_superseded_by,omitempty"`
	CardRev          int64  `json:"card_rev"`
	// `any` rather than a pointer type: an absent selector is `null`, never missing.
	Selector      any    `json:"selector"`
	Body          string `json:"body"`
	Motivation    string `json:"motivation"`
	Creator       string `json:"creator"`
	CreatorKind   string `json:"creator_kind"`
	Resolved      bool   `json:"resolved"`
	ResolvedReply any    `json:"resolved_reply"`
	Stale         bool   `json:"stale"`
	CreatedAt     string `json:"created_at"`
}

type annotationRow struct {
	ID            string
	SpaceID       string
	CardID        string
	CardRev       int64
	Selector      sql.NullString
	Body          string
	Motivation    string
	Creator       string
	CreatorKind   string
	Resolved      int
	ResolvedReply sql.NullString
	ResolvedAt    sql.NullString
	CreatedAt     string
}

const annotationColumns = "id, space_id, card_id, card_rev, selector, body, motivation," +
	" creator, creator_kind, resolved, resolved_reply, resolved_at, created_at"

func scanAnnotation(scan func(...any) error) (annotationRow, error) {
	var r annotationRow
	err := scan(&r.ID, &r.SpaceID, &r.CardID, &r.CardRev, &r.Selector, &r.Body,
		&r.Motivation, &r.Creator, &r.CreatorKind, &r.Resolved, &r.ResolvedReply,
		&r.ResolvedAt, &r.CreatedAt)
	return r, err
}

// cardSummary is the part of a card an annotation needs: its title, its current rev
// and whether it has been superseded.
type cardSummary struct {
	Rev          int64
	Title        string
	SupersededBy string
}

// cardIndex covers every card in the space, deleted ones included: an annotation on
// a deleted card still reports its title.
func cardIndex(q querier, spaceID string) (map[string]cardSummary, error) {
	rows, err := q.Query("SELECT id, node_json, rev FROM card WHERE space_id = ?", spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	index := map[string]cardSummary{}
	for rows.Next() {
		var id, nodeJSON string
		var rev int64
		if err := rows.Scan(&id, &nodeJSON, &rev); err != nil {
			return nil, err
		}
		node, err := decodeObject([]byte(nodeJSON))
		if err != nil {
			return nil, err
		}
		index[id] = cardSummary{Rev: rev, Title: stringOf(node["sp_title"]),
			SupersededBy: stringOf(node["sp_superseded_by"])}
	}
	return index, rows.Err()
}

func (r annotationRow) annotation(index map[string]cardSummary) (Annotation, error) {
	card, known := index[r.CardID]
	out := Annotation{
		ID: r.ID, CardID: r.CardID, CardTitle: card.Title, CardRev: r.CardRev,
		Body: r.Body, Motivation: r.Motivation, Creator: r.Creator,
		CreatorKind: r.CreatorKind, Resolved: r.Resolved != 0,
		ResolvedReply: nullString(r.ResolvedReply),
		// Stale is computed on read, never stored. In branch mode a superseded card
		// is never written again, so its rev freezes and this can never become true.
		Stale:     known && r.CardRev < card.Rev,
		CreatedAt: r.CreatedAt,
		Selector:  nil,
	}
	if r.Selector.Valid {
		selector, err := decodeObject([]byte(r.Selector.String))
		if err != nil {
			return out, err
		}
		out.Selector = selector
	}
	out.CardSupersededBy = card.SupersededBy
	return out, nil
}

// Annotations lists a space's annotations in insertion order. resolved and cardID
// are nil/"" for "no filter".
func (s *Store) Annotations(slug string, resolved *bool, cardID string) ([]Annotation, error) {
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return nil, err
	}
	query := "SELECT " + annotationColumns + " FROM annotation WHERE space_id = ?"
	args := []any{space.ID}
	if resolved != nil {
		query += " AND resolved = ?"
		if *resolved {
			args = append(args, 1)
		} else {
			args = append(args, 0)
		}
	}
	if cardID != "" {
		query += " AND card_id = ?"
		args = append(args, cardID)
	}
	return s.queryAnnotations(space.ID, query+" ORDER BY rowid", args...)
}

func (s *Store) queryAnnotations(spaceID, query string, args ...any) ([]Annotation, error) {
	index, err := cardIndex(s.read, spaceID)
	if err != nil {
		return nil, err
	}
	rows, err := s.read.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Annotation{}
	for rows.Next() {
		r, err := scanAnnotation(rows.Scan)
		if err != nil {
			return nil, err
		}
		a, err := r.annotation(index)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) one(spaceID, annotationID string) (Annotation, error) {
	found, err := s.queryAnnotations(spaceID,
		"SELECT "+annotationColumns+" FROM annotation WHERE id = ?", annotationID)
	if err != nil {
		return Annotation{}, err
	}
	if len(found) == 0 {
		return Annotation{}, apierr.NotFound("no annotation '" + annotationID + "' in this space")
	}
	return found[0], nil
}

func (s *Store) CreateAnnotation(slug, cardID, body string, selector map[string]any,
	motivation, actor, actorKind string) (Annotation, error) {
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return Annotation{}, err
	}
	// allowDeleted: you can still comment on something the human just removed.
	card, err := cardRowByID(s.read, space.ID, cardID, true)
	if err != nil {
		return Annotation{}, err
	}
	if motivation == "" {
		motivation = "commenting"
	}
	if !contains(motivations, motivation) {
		return Annotation{}, apierr.ValidationFailed(
			"motivation must be one of " + strings.Join(motivations, ", "))
	}

	annotationID := ids.AnnotationID()
	var encodedSelector any
	if selector != nil {
		encodedSelector = mustEncode(selector)
	}
	err = s.withWrite(func(t *tx) error {
		if _, err := t.Exec(
			"INSERT INTO annotation (id, space_id, card_id, card_rev, selector, body,"+
				" motivation, creator, creator_kind, resolved, created_at)"+
				" VALUES (?,?,?,?,?,?,?,?,?,0,?)",
			annotationID, space.ID, cardID, card.Rev, encodedSelector, body,
			motivation, actor, actorKind, Now()); err != nil {
			return err
		}
		_, err := t.emit(space.ID, "annotation.created", annotationID, actor, actorKind,
			map[string]any{"card_id": cardID})
		return err
	})
	if err != nil {
		return Annotation{}, err
	}
	return s.one(space.ID, annotationID)
}

// ResolveAnnotation resolves or reopens. Reopening is silent: there is no
// annotation.reopened event type.
func (s *Store) ResolveAnnotation(slug, annotationID string, resolved bool, reply *string,
	actor, actorKind string) (Annotation, error) {
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return Annotation{}, err
	}
	var found string
	err = s.read.QueryRow("SELECT id FROM annotation WHERE id = ? AND space_id = ?",
		annotationID, space.ID).Scan(&found)
	if err != nil {
		return Annotation{}, apierr.NotFound(
			"no annotation '" + annotationID + "' in this space")
	}

	err = s.withWrite(func(t *tx) error {
		if !resolved {
			_, err := t.Exec(
				"UPDATE annotation SET resolved = 0, resolved_reply = NULL,"+
					" resolved_at = NULL WHERE id = ?", annotationID)
			return err
		}
		var stored any
		if reply != nil {
			stored = *reply
		}
		if _, err := t.Exec(
			"UPDATE annotation SET resolved = 1, resolved_reply = ?,"+
				" resolved_at = ? WHERE id = ?", stored, Now(), annotationID); err != nil {
			return err
		}
		_, err := t.emit(space.ID, "annotation.resolved", annotationID, actor, actorKind,
			map[string]any{"reply": stored})
		return err
	})
	if err != nil {
		return Annotation{}, err
	}
	return s.one(space.ID, annotationID)
}
