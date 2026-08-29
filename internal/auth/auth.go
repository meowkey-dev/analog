// Package auth implements per-actor bearer tokens.
//
// SPEC §3 sketched "a single shared bearer token from an env var" for the moment the
// server leaves localhost. That gatekeeps the server but not identity: anyone holding
// the shared token could still write as any actor, and the event log's whole value is
// that `actor` is trustworthy (§2.2, §10). So a token identifies exactly one actor,
// and the server takes `actor`/`actor_kind` from the token rather than believing the
// query string.
//
// The store is a JSON file, not a table: internal/store/schema.sql is a frozen
// contract, and credentials are operator state rather than canvas data. It holds
// SHA-256 digests, so a leaked file does not hand over working tokens.
//
// Auth is OFF when the file is absent or empty, which keeps a loopback dev server
// exactly as it was. RequireAuthForHost refuses to start an unauthenticated server on
// a non-loopback address, so nobody exposes an open canvas by accident.
package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	TokenPrefix  = "analog_"
	tokenBytes   = 32
	storeVersion = 1
)

type Identity struct {
	Actor     string
	ActorKind string
}

// Error is a configuration problem, not a failed request.
type Error struct{ msg string }

func (e *Error) Error() string { return e.msg }

func errf(format string, args ...any) *Error {
	return &Error{msg: fmt.Sprintf(format, args...)}
}

// NewToken mints a fresh secret. The prefix makes it recognisable in logs and leak
// scanners.
func NewToken() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(raw), nil
}

func Digest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// Now is second precision — deliberately not the millisecond canvas format in
// internal/store. Do not unify them without checking what reads each.
func Now() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }

// entry is one actor's record. Field order is the file's key order.
type entry struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	TokenSHA256 string `json:"token_sha256"`
	CreatedAt   string `json:"created_at"`
}

// Entry is an actor without any secret material.
type Entry struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	CreatedAt string `json:"created_at"`
}

type file struct {
	Version int     `json:"version"`
	Actors  []entry `json:"actors"`
}

// Store is the token file. Every method re-reads it, so issuing a token secures a
// server that is already running.
type Store struct{ Path string }

func NewStore(path string) *Store { return &Store{Path: path} }

func (s *Store) read() (file, error) {
	raw, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return file{Version: storeVersion, Actors: []entry{}}, nil
	}
	if err != nil {
		return file{}, err
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return file{Version: storeVersion, Actors: []entry{}}, nil
	}
	var data file
	if err := json.Unmarshal(raw, &data); err != nil {
		return file{}, errf("%s is not valid JSON: %v", s.Path, err)
	}
	if data.Version == 0 {
		data.Version = storeVersion
	}
	if data.Actors == nil {
		data.Actors = []entry{}
	}
	return data, nil
}

func (s *Store) write(data file) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	// Match the file a Python Analog wrote: two-space indent, a trailing newline,
	// and no <-style escaping of ordinary characters.
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		return err
	}
	tmp := strings.TrimSuffix(s.Path, filepath.Ext(s.Path)) + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, s.Path); err != nil {
		return err
	}
	// Tokens are credentials even as digests; don't leave them world-readable.
	return os.Chmod(s.Path, 0o600)
}

// Enabled reports whether any token exists. No tokens means auth is off.
func (s *Store) Enabled() bool {
	data, err := s.read()
	return err == nil && len(data.Actors) > 0
}

// Entries lists every actor, without any secret material.
func (s *Store) Entries() ([]Entry, error) {
	data, err := s.read()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(data.Actors))
	for _, e := range data.Actors {
		out = append(out, Entry{Name: e.Name, Kind: e.Kind, CreatedAt: e.CreatedAt})
	}
	return out, nil
}

// Resolve maps a token to the one actor it writes as.
func (s *Store) Resolve(token string) *Identity {
	if token == "" {
		return nil
	}
	data, err := s.read()
	if err != nil {
		return nil
	}
	wanted := []byte(Digest(token))
	for _, e := range data.Actors {
		// Constant-time: a timing oracle on a digest is cheap to avoid.
		if subtle.ConstantTimeCompare([]byte(e.TokenSHA256), wanted) == 1 {
			return &Identity{Actor: e.Name, ActorKind: e.Kind}
		}
	}
	return nil
}

// Issue mints a token for actor, replacing any existing one. The returned secret is
// the only time it is ever visible.
func (s *Store) Issue(actor, actorKind string) (string, error) {
	if actorKind != "human" && actorKind != "agent" {
		return "", errf("actor_kind must be 'human' or 'agent'")
	}
	if actor == "" || len(actor) > 64 {
		return "", errf("actor must be 1-64 characters")
	}
	token, err := NewToken()
	if err != nil {
		return "", err
	}
	data, err := s.read()
	if err != nil {
		return "", err
	}
	kept := make([]entry, 0, len(data.Actors)+1)
	for _, e := range data.Actors {
		if e.Name != actor {
			kept = append(kept, e)
		}
	}
	data.Actors = append(kept, entry{
		Name: actor, Kind: actorKind, TokenSHA256: Digest(token), CreatedAt: Now()})
	if err := s.write(data); err != nil {
		return "", err
	}
	return token, nil
}

// Revoke invalidates an actor's token, reporting whether there was one.
func (s *Store) Revoke(actor string) (bool, error) {
	data, err := s.read()
	if err != nil {
		return false, err
	}
	kept := make([]entry, 0, len(data.Actors))
	for _, e := range data.Actors {
		if e.Name != actor {
			kept = append(kept, e)
		}
	}
	if len(kept) == len(data.Actors) {
		return false, nil
	}
	data.Actors = kept
	return true, s.write(data)
}

// Bearer pulls the token out of an Authorization header.
func Bearer(header string) string {
	if header == "" {
		return ""
	}
	scheme, value, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "bearer") {
		return ""
	}
	return strings.TrimSpace(value)
}

func IsLoopback(host string) bool {
	if host == "localhost" || host == "" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// RequireAuthForHost refuses to serve an unauthenticated canvas to a network.
func RequireAuthForHost(host string, store *Store) error {
	if IsLoopback(host) || store.Enabled() {
		return nil
	}
	return errf(
		"refusing to bind %s with no tokens configured — an unauthenticated "+
			"Analog on a network is world-writable.\n"+
			"Issue one first:  analog token add <actor> --kind human\n"+
			"(store: %s)", host, store.Path)
}
