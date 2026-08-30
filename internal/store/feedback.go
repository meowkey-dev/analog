package store

import (
	"database/sql"
	"fmt"
	"slices"
	"strings"
)

// Feedback is SPEC §4.1: what someone else changed since this actor last looked.
//
// The annotations bucket is cursor-independent — every unresolved comment, every
// call. Everything else is cursor-governed: card/link deltas and `replies` (a
// comment another actor resolved with a reply, the answer that used to be visible
// to nobody) come from the event window and exclude the caller's own events.
//
// Every slice is non-nil so the JSON is `[]` rather than `null`.
type Feedback struct {
	Cursor       int64            `json:"cursor"`
	Annotations  []Annotation     `json:"annotations"`
	Replies      []map[string]any `json:"replies"`
	CardsEdited  []map[string]any `json:"cards_edited"`
	CardsDeleted []map[string]any `json:"cards_deleted"`
	CardsMoved   []map[string]any `json:"cards_moved"`
	LinksAdded   []map[string]any `json:"links_added"`
	LinksRemoved []map[string]any `json:"links_removed"`
	Summary      string           `json:"summary"`
}

// bucket is an insertion-ordered map, because the buckets are compared against
// frozen fixtures and Go's map iteration order is deliberately random. A repeated
// key keeps its original position, matching Python's dict.
type bucket struct {
	keys []string
	vals map[string]map[string]any
}

func newBucket() *bucket { return &bucket{vals: map[string]map[string]any{}} }

func (b *bucket) get(key string) (map[string]any, bool) {
	v, ok := b.vals[key]
	return v, ok
}

func (b *bucket) set(key string, value map[string]any) {
	if _, seen := b.vals[key]; !seen {
		b.keys = append(b.keys, key)
	}
	b.vals[key] = value
}

// setdefault returns the existing row, or inserts and returns the given one.
func (b *bucket) setdefault(key string, value map[string]any) map[string]any {
	if existing, ok := b.vals[key]; ok {
		return existing
	}
	b.set(key, value)
	return value
}

func (b *bucket) remove(key string) {
	if _, ok := b.vals[key]; !ok {
		return
	}
	delete(b.vals, key)
	b.keys = slices.DeleteFunc(b.keys, func(k string) bool { return k == key })
}

func (b *bucket) order() []string { return slices.Clone(b.keys) }

func (b *bucket) values() []map[string]any {
	out := make([]map[string]any, 0, len(b.keys))
	for _, k := range b.keys {
		out = append(out, b.vals[k])
	}
	return out
}

// Feedback computes the delta. since == nil reads the actor's stored cursor;
// advance moves it to the space's current seq.
func (s *Store) Feedback(slug, actor string, since *int64, advance bool) (Feedback, error) {
	out := Feedback{
		Annotations: []Annotation{}, Replies: []map[string]any{},
		CardsEdited:  []map[string]any{},
		CardsDeleted: []map[string]any{}, CardsMoved: []map[string]any{},
		LinksAdded: []map[string]any{}, LinksRemoved: []map[string]any{},
	}
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return out, err
	}

	from := int64(0)
	if since != nil {
		from = *since
	} else {
		// An actor with no cursor starts at zero and sees everything.
		var stored int64
		if err := s.read.QueryRow(
			"SELECT seq FROM actor_cursor WHERE space_id = ? AND actor = ?",
			space.ID, actor).Scan(&stored); err == nil {
			from = stored
		}
	}

	index, err := cardIndex(s.read, space.ID)
	if err != nil {
		return out, err
	}
	// Unresolved annotations come back regardless of the cursor; resolved ones
	// never do. That is what makes an open comment impossible to miss.
	out.Annotations, err = s.queryAnnotations(space.ID,
		"SELECT "+annotationColumns+" FROM annotation WHERE space_id = ? AND resolved = 0"+
			" ORDER BY rowid", space.ID)
	if err != nil {
		return out, err
	}

	edited, moved, deleted := newBucket(), newBucket(), newBucket()
	added, removed := newBucket(), newBucket()

	title := func(subjectID string, event Event) string {
		if card, ok := index[subjectID]; ok {
			return card.Title
		}
		return stringOf(event.Payload["title"])
	}

	events, err := s.Events(space.ID, from, 1_000_000)
	if err != nil {
		return out, err
	}
	for _, event := range events {
		// SPEC §10: never read your own writes back.
		if event.Actor == actor {
			continue
		}
		subject := event.SubjectID
		payload := event.Payload
		switch event.Type {
		case "card.updated":
			row := edited.setdefault(subject, map[string]any{
				"id": subject, "title": title(subject, event),
				"changed": []string{}, "actor": event.Actor})
			// `changed` is the union across every card.updated in the window.
			union := map[string]bool{}
			for _, k := range row["changed"].([]string) {
				union[k] = true
			}
			if raw, ok := payload["changed"].([]any); ok {
				for _, k := range raw {
					union[stringOf(k)] = true
				}
			}
			keys := make([]string, 0, len(union))
			for k := range union {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			row["changed"] = keys
			row["actor"] = event.Actor
		case "card.moved":
			moved.set(subject, map[string]any{
				"id": subject, "title": title(subject, event), "actor": event.Actor})
		case "card.deleted":
			deleted.set(subject, map[string]any{
				"id": subject, "title": title(subject, event), "actor": event.Actor})
		case "link.created":
			fromNode, toNode, label := payload["from"], payload["to"], payload["label"]
			if len(payload) == 0 {
				// No payload: recover the endpoints from the link itself.
				var raw string
				if err := s.read.QueryRow(
					"SELECT edge_json FROM link WHERE id = ?", subject).Scan(&raw); err == nil {
					if edge, err := decodeObject([]byte(raw)); err == nil {
						fromNode, toNode, label = edge["fromNode"], edge["toNode"], edge["label"]
					}
				}
			}
			row := map[string]any{"id": subject, "from": fromNode, "to": toNode,
				"actor": event.Actor}
			if label != nil {
				row["label"] = label
			}
			added.set(subject, row)
		case "link.deleted":
			removed.set(subject, map[string]any{"id": subject, "actor": event.Actor})
		case "annotation.resolved":
			// The own-event filter above already guarantees the resolver is the
			// other side. A resolve without a reply is the acknowledgment itself
			// (SPEC §4.1) and lands in no bucket; an answer is a message and is
			// delivered once, like any cursor-governed delta.
			reply := stringOf(payload["reply"])
			if reply == "" {
				continue
			}
			row, err := scanAnnotation(s.read.QueryRow(
				"SELECT "+annotationColumns+" FROM annotation WHERE id = ?", subject).Scan)
			if err == sql.ErrNoRows {
				continue
			}
			if err != nil {
				return out, err
			}
			// One entry per resolve event, not per annotation: reopening and
			// resolving again is a second, distinct message.
			cardTitle := ""
			if card, ok := index[row.CardID]; ok {
				cardTitle = card.Title
			}
			out.Replies = append(out.Replies, map[string]any{
				"id": subject, "card_id": row.CardID, "card_title": cardTitle,
				"body": row.Body, "motivation": row.Motivation,
				"creator": row.Creator, "creator_kind": row.CreatorKind,
				"reply": reply, "actor": event.Actor, "resolved_at": event.TS})
		}
	}

	// One row per subject, strongest signal wins: a deletion supersedes an edit or
	// a move, an edit supersedes a move.
	for _, subject := range deleted.order() {
		edited.remove(subject)
		moved.remove(subject)
	}
	for _, subject := range edited.order() {
		moved.remove(subject)
	}
	// A link created and removed inside the same window appears in neither bucket.
	for _, subject := range added.order() {
		if _, ok := removed.get(subject); ok {
			added.remove(subject)
			removed.remove(subject)
		}
	}

	out.Cursor = space.Seq
	if advance {
		if err := s.withWrite(func(t *tx) error {
			_, err := t.Exec(
				"INSERT INTO actor_cursor (space_id, actor, seq) VALUES (?,?,?)"+
					" ON CONFLICT(space_id, actor) DO UPDATE SET seq = excluded.seq",
				space.ID, actor, out.Cursor)
			return err
		}); err != nil {
			return out, err
		}
	}

	out.CardsEdited = edited.values()
	out.CardsDeleted = deleted.values()
	out.CardsMoved = moved.values()
	out.LinksAdded = added.values()
	out.LinksRemoved = removed.values()
	out.Summary = Summarize(out)
	return out, nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// Summarize is the grammar pinned by contracts/fixtures/feedback.*.json.
func Summarize(f Feedback) string {
	var parts []string
	if n := len(f.Annotations); n > 0 {
		part := plural(n, "open comment", "open comments")
		stale := 0
		for _, a := range f.Annotations {
			if a.Stale {
				stale++
			}
		}
		if stale > 0 {
			part += fmt.Sprintf(" (%d stale)", stale)
		}
		parts = append(parts, part)
	}
	if n := len(f.Replies); n > 0 {
		parts = append(parts, plural(n, "reply on resolve", "replies on resolve"))
	}
	if n := len(f.CardsEdited); n > 0 {
		parts = append(parts, plural(n, "card edited", "cards edited"))
	}
	if n := len(f.CardsDeleted); n > 0 {
		parts = append(parts, fmt.Sprintf("%d deleted", n))
	}
	if n := len(f.CardsMoved); n > 0 {
		parts = append(parts, fmt.Sprintf("%d moved", n))
	}
	if n := len(f.LinksAdded); n > 0 {
		parts = append(parts, plural(n, "new link", "new links"))
	}
	if n := len(f.LinksRemoved); n > 0 {
		parts = append(parts, plural(n, "link removed", "links removed"))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ") + "."
}
