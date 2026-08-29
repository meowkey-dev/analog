// Package store is database access and event emission.
//
// All business logic lives here. SPEC §4: "if you find yourself writing a rule in one
// of them, it belongs in the server" — the MCP server and the CLI are thin proxies, so
// this package is the only place a rule is written down.
package store

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/meowkey-dev/analog/internal/apierr"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

// Schema is the frozen schema, exported so the seed command can apply it.
func Schema() string { return schemaSQL }

var slugRE = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)

var (
	kinds       = []string{"md", "html", "svg", "plain"}
	motivations = []string{"commenting", "assessing", "editing"}
)

// geometryKeys are the only keys a card.moved may touch.
var geometryKeys = map[string]bool{"x": true, "y": true, "width": true, "height": true}

// immutableKeys are set by the server, never by a client patch.
var immutableKeys = map[string]bool{
	"id": true, "sp_rev": true, "sp_created_by": true, "sp_superseded_by": true,
}

const (
	DefaultWidth  = 320
	DefaultHeight = 200
	LayoutGap     = 40
	// LayoutMaxColumn: a batch wraps into a new column past this height rather than
	// growing one very tall column. SPEC §5 asks for "a column, top-down"; five
	// cards of it is a 1200px strip you have to zoom out to read.
	LayoutMaxColumn = 900
)

// Now is the canvas timestamp format: milliseconds, Z, no offset. The token store
// uses second precision instead — see internal/auth. Do not unify them without
// checking what reads each.
func Now() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// Event is one row of the per-space log.
type Event struct {
	Seq       int64          `json:"seq"`
	TS        string         `json:"ts"`
	Type      string         `json:"type"`
	SubjectID string         `json:"subject_id"`
	Actor     string         `json:"actor"`
	ActorKind string         `json:"actor_kind"`
	Payload   map[string]any `json:"payload,omitempty"`
}

// Store owns the database.
//
// Two pools, because SQLite in WAL mode allows many readers and one writer, and
// Go's database/sql would otherwise hand concurrent writers a SQLITE_BUSY. Reads
// inside a write transaction go through the transaction, so they still see
// uncommitted state.
type Store struct {
	read      *sql.DB
	write     *sql.DB
	MediaRoot string

	// Publish is called once per event, after the transaction that produced it
	// commits. A rollback drops the pending events instead. Set by the server to
	// the SSE broker; nil is fine.
	mu      sync.RWMutex
	publish func(spaceID string, event Event)
}

// Open connects to the database at path, applying the schema if it is empty.
func Open(path, mediaRoot string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	read, err := openPool(path, 0)
	if err != nil {
		return nil, err
	}
	// One writer. SQLite serialises writes anyway; capping the pool makes that
	// explicit rather than letting busy_timeout absorb the contention.
	write, err := openPool(path, 1)
	if err != nil {
		read.Close()
		return nil, err
	}
	s := &Store{read: read, write: write, MediaRoot: mediaRoot}
	if err := s.ensureSchema(); err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

func openPool(path string, maxOpen int) (*sql.DB, error) {
	// Pragmas ride on the DSN so every pooled connection gets them, not just the
	// first one a query happens to land on.
	dsn := fmt.Sprintf(
		"file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)",
		path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if maxOpen > 0 {
		db.SetMaxOpenConns(maxOpen)
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *Store) ensureSchema() error {
	var one int
	err := s.write.QueryRow(
		"SELECT 1 FROM sqlite_master WHERE type='table' AND name='space'").Scan(&one)
	if err == nil {
		return nil
	}
	if err != sql.ErrNoRows {
		return err
	}
	_, err = s.write.Exec(schemaSQL)
	return err
}

// SetPublisher installs the post-commit event sink.
func (s *Store) SetPublisher(fn func(spaceID string, event Event)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publish = fn
}

func (s *Store) Close() error {
	var first error
	for _, db := range []*sql.DB{s.read, s.write} {
		if db != nil {
			if err := db.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

// --- queriers ----------------------------------------------------------------

// querier is the subset of *sql.DB and *sql.Tx the read helpers need, so one
// helper serves both a plain read and a read inside a write transaction.
type querier interface {
	Query(string, ...any) (*sql.Rows, error)
	QueryRow(string, ...any) *sql.Row
	Exec(string, ...any) (sql.Result, error)
}

// tx is a write transaction plus the events it has emitted but not yet published.
//
// Pending events belong to the transaction rather than to the Store: in Python they
// were once a class attribute, which made two concurrent requests share a list.
// Scoping them here removes that whole class of bug.
type tx struct {
	*sql.Tx
	store   *Store
	pending []pendingEvent
}

type pendingEvent struct {
	spaceID string
	event   Event
}

// withWrite runs fn inside BEGIN IMMEDIATE, then publishes the events it emitted.
// A rollback drops them.
func (s *Store) withWrite(fn func(*tx) error) error {
	sqlTx, err := s.write.Begin()
	if err != nil {
		return err
	}
	t := &tx{Tx: sqlTx, store: s}
	if err := fn(t); err != nil {
		_ = sqlTx.Rollback()
		return err
	}
	if err := sqlTx.Commit(); err != nil {
		return err
	}
	s.mu.RLock()
	publish := s.publish
	s.mu.RUnlock()
	if publish != nil {
		for _, p := range t.pending {
			publish(p.spaceID, p.event)
		}
	}
	return nil
}

// emit allocates the next seq for this space and appends one row.
//
// The UPDATE ... RETURNING is what makes seq per-space monotonic with no gaps: it
// runs inside the same write transaction as the change it describes.
func (t *tx) emit(spaceID, typ, subjectID, actor, actorKind string, payload map[string]any) (Event, error) {
	var seq int64
	err := t.QueryRow("UPDATE space SET seq = seq + 1 WHERE id = ? RETURNING seq",
		spaceID).Scan(&seq)
	if err != nil {
		return Event{}, err
	}
	ts := Now()
	var raw any
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return Event{}, err
		}
		raw = string(encoded)
	}
	_, err = t.Exec(
		"INSERT INTO event (space_id, seq, ts, type, subject_id, actor, actor_kind,"+
			" payload) VALUES (?,?,?,?,?,?,?,?)",
		spaceID, seq, ts, typ, subjectID, actor, actorKind, raw)
	if err != nil {
		return Event{}, err
	}
	event := Event{Seq: seq, TS: ts, Type: typ, SubjectID: subjectID,
		Actor: actor, ActorKind: actorKind, Payload: payload}
	t.pending = append(t.pending, pendingEvent{spaceID, event})
	return event, nil
}

// --- json helpers ------------------------------------------------------------

// decodeObject parses a JSON object preserving numeric literals exactly.
//
// Card and edge blobs carry arbitrary sp_* keys and coordinates the fixtures pin as
// integers. Decoding into json.Number and re-encoding reproduces the original
// literal, so a round trip cannot turn 320 into 320.0 or lose precision on a large
// integer in sp_meta.
func decodeObject(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	return out, nil
}

func mustEncode(v any) string {
	out, err := json.Marshal(v)
	if err != nil {
		// Only unencodable Go values reach this, and everything stored here came
		// from JSON in the first place.
		panic(fmt.Sprintf("encoding %T: %v", v, err))
	}
	return string(out)
}

// numberOf coerces a decoded JSON value to a float, whatever shape it arrived in.
func numberOf(v any) (float64, bool) {
	switch n := v.(type) {
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

func nullString(s sql.NullString) any {
	if !s.Valid {
		return nil
	}
	return s.String
}

var _ = apierr.NotFound
