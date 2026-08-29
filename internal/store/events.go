package store

import (
	"database/sql"
)

// EventPage is what GET /events returns.
type EventPage struct {
	Events []Event `json:"events"`
	Cursor int64   `json:"cursor"`
}

// Events reads the log for one space id, oldest first.
func (s *Store) Events(spaceID string, since int64, limit int) ([]Event, error) {
	rows, err := s.read.Query(
		"SELECT seq, ts, type, subject_id, actor, actor_kind, payload FROM event"+
			" WHERE space_id = ? AND seq > ? ORDER BY seq LIMIT ?",
		spaceID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []Event{}
	for rows.Next() {
		var e Event
		var payload sql.NullString
		if err := rows.Scan(&e.Seq, &e.TS, &e.Type, &e.SubjectID, &e.Actor,
			&e.ActorKind, &payload); err != nil {
			return nil, err
		}
		if payload.Valid {
			decoded, err := decodeObject([]byte(payload.String))
			if err != nil {
				return nil, err
			}
			e.Payload = decoded
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListEvents is the slug-addressed page. The cursor is the last seq returned, or
// `since` when the page is empty, so a caller can poll without losing its place.
func (s *Store) ListEvents(slug string, since int64, limit int) (EventPage, error) {
	space, err := s.spaceRow(s.read, slug)
	if err != nil {
		return EventPage{}, err
	}
	events, err := s.Events(space.ID, since, limit)
	if err != nil {
		return EventPage{}, err
	}
	cursor := since
	if len(events) > 0 {
		cursor = events[len(events)-1].Seq
	}
	return EventPage{Events: events, Cursor: cursor}, nil
}
