package store

import (
	"database/sql"

	"github.com/meowkey-dev/analog/internal/apierr"
	"github.com/meowkey-dev/analog/internal/ids"
)

// Counts is the summary carried on every Space.
//
// No omitempty: a space with no cards must still report `"cards": 0`.
type Counts struct {
	Cards           int `json:"cards"`
	Links           int `json:"links"`
	OpenAnnotations int `json:"open_annotations"`
}

type Space struct {
	ID           string `json:"id"`
	Slug         string `json:"slug"`
	Title        string `json:"title"`
	RevisionMode string `json:"revision_mode"`
	Seq          int64  `json:"seq"`
	CreatedAt    string `json:"created_at"`
	Counts       Counts `json:"counts"`
}

// spaceRow is the table row, before counts are attached.
type spaceRow struct {
	ID           string
	Slug         string
	Title        string
	RevisionMode string
	Seq          int64
	CreatedAt    string
}

func scanSpace(scan func(...any) error) (spaceRow, error) {
	var r spaceRow
	err := scan(&r.ID, &r.Slug, &r.Title, &r.RevisionMode, &r.Seq, &r.CreatedAt)
	return r, err
}

const spaceColumns = "id, slug, title, revision_mode, seq, created_at"

func (s *Store) spaceRow(q querier, slug string) (spaceRow, error) {
	row := q.QueryRow("SELECT "+spaceColumns+" FROM space WHERE slug = ?", slug)
	r, err := scanSpace(row.Scan)
	if err == sql.ErrNoRows {
		return r, apierr.NotFound("no space with slug '" + slug + "'")
	}
	return r, err
}

// SpaceID resolves a slug, for callers that only need the id.
func (s *Store) SpaceID(slug string) (string, error) {
	r, err := s.spaceRow(s.read, slug)
	return r.ID, err
}

func (s *Store) spaceDict(q querier, r spaceRow) (Space, error) {
	out := Space{ID: r.ID, Slug: r.Slug, Title: r.Title,
		RevisionMode: r.RevisionMode, Seq: r.Seq, CreatedAt: r.CreatedAt}
	counts := []struct {
		query string
		into  *int
	}{
		{"SELECT count(*) FROM card WHERE space_id = ? AND deleted_at IS NULL", &out.Counts.Cards},
		{"SELECT count(*) FROM link WHERE space_id = ? AND deleted_at IS NULL", &out.Counts.Links},
		{"SELECT count(*) FROM annotation WHERE space_id = ? AND resolved = 0", &out.Counts.OpenAnnotations},
	}
	for _, c := range counts {
		if err := q.QueryRow(c.query, r.ID).Scan(c.into); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *Store) Space(slug string) (Space, error) {
	r, err := s.spaceRow(s.read, slug)
	if err != nil {
		return Space{}, err
	}
	return s.spaceDict(s.read, r)
}

func (s *Store) ListSpaces() ([]Space, error) {
	rows, err := s.read.Query("SELECT " + spaceColumns + " FROM space ORDER BY rowid")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var raw []spaceRow
	for rows.Next() {
		r, err := scanSpace(rows.Scan)
		if err != nil {
			return nil, err
		}
		raw = append(raw, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Always an array, never null: the contract says GET /spaces returns a list.
	out := make([]Space, 0, len(raw))
	for _, r := range raw {
		space, err := s.spaceDict(s.read, r)
		if err != nil {
			return nil, err
		}
		out = append(out, space)
	}
	return out, nil
}

func (s *Store) CreateSpace(slug, title, revisionMode, actor, actorKind string) (Space, error) {
	if !slugRE.MatchString(slug) {
		return Space{}, apierr.ValidationFailed(
			`slug must match ^[a-z0-9-]{1,64}$`, apierr.Detail{"slug": slug})
	}
	if revisionMode == "" {
		revisionMode = "replace"
	}
	if revisionMode != "replace" && revisionMode != "branch" {
		return Space{}, apierr.ValidationFailed("revision_mode must be 'replace' or 'branch'")
	}
	var exists int
	err := s.read.QueryRow("SELECT 1 FROM space WHERE slug = ?", slug).Scan(&exists)
	if err == nil {
		return Space{}, apierr.Conflict("a space with slug '" + slug + "' already exists")
	}
	if err != sql.ErrNoRows {
		return Space{}, err
	}

	spaceID := ids.SpaceID()
	err = s.withWrite(func(t *tx) error {
		if _, err := t.Exec(
			"INSERT INTO space (id, slug, title, revision_mode, seq, created_at)"+
				" VALUES (?,?,?,?,0,?)",
			spaceID, slug, title, revisionMode, Now()); err != nil {
			return err
		}
		_, err := t.emit(spaceID, "space.created", spaceID, actor, actorKind,
			map[string]any{"slug": slug, "title": title})
		return err
	})
	if err != nil {
		return Space{}, err
	}
	return s.Space(slug)
}

// SpacePatch carries only the keys the caller sent; a nil field is "not supplied".
type SpacePatch struct {
	Title        *string `json:"title"`
	RevisionMode *string `json:"revision_mode"`
}

func (s *Store) UpdateSpace(slug string, patch SpacePatch) (Space, error) {
	r, err := s.spaceRow(s.read, slug)
	if err != nil {
		return Space{}, err
	}
	title, mode := r.Title, r.RevisionMode
	if patch.Title != nil {
		title = *patch.Title
	}
	if patch.RevisionMode != nil {
		mode = *patch.RevisionMode
	}
	if mode != "replace" && mode != "branch" {
		return Space{}, apierr.ValidationFailed("revision_mode must be 'replace' or 'branch'")
	}
	err = s.withWrite(func(t *tx) error {
		_, err := t.Exec("UPDATE space SET title = ?, revision_mode = ? WHERE id = ?",
			title, mode, r.ID)
		return err
	})
	if err != nil {
		return Space{}, err
	}
	return s.Space(slug)
}

func (s *Store) DeleteSpace(slug, actor, actorKind string) error {
	r, err := s.spaceRow(s.read, slug)
	if err != nil {
		return err
	}
	return s.withWrite(func(t *tx) error {
		// Emitted for live subscribers, then taken by the cascade: a per-space log
		// cannot outlive its space. See schema.sql note 5.
		if _, err := t.emit(r.ID, "space.deleted", r.ID, actor, actorKind,
			map[string]any{"slug": slug}); err != nil {
			return err
		}
		_, err := t.Exec("DELETE FROM space WHERE id = ?", r.ID)
		return err
	})
}
