// Package api is the HTTP surface. SPEC §3, contracts/openapi.json.
//
// Handlers do argument plumbing and nothing else; every rule lives in internal/store.
package api

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/meowkey-dev/analog/internal/apierr"
	"github.com/meowkey-dev/analog/internal/auth"
	"github.com/meowkey-dev/analog/internal/config"
	"github.com/meowkey-dev/analog/internal/sse"
	"github.com/meowkey-dev/analog/internal/store"
)

// Version is what /health reports. It matches contracts/openapi.json info.version.
const Version = "0.3.0"

// API is the prefix every documented operation sits behind.
const API = config.APIPrefix

// publicPaths are reachable without a token: it is how a client discovers whether
// it needs one.
var publicPaths = map[string]bool{API + "/health": true}

type Server struct {
	Store  *store.Store
	Tokens *auth.Store
	Broker *sse.Broker

	handler http.Handler
	// patterns is the routing table, recorded as it is built.
	patterns []string
	// Web is the built SPA to serve, or nil for an API-only server.
	Web fs.FS
}

// New wires a server. The store's publisher is pointed at the broker, so events
// reach subscribers once the transaction that produced them commits.
func New(st *store.Store, tokens *auth.Store, web fs.FS) *Server {
	s := &Server{Store: st, Tokens: tokens, Broker: sse.NewBroker(), Web: web}
	st.SetPublisher(s.Broker.Publish)

	mux := http.NewServeMux()
	s.routes(mux)
	// CORS is the outermost layer: a 401 still needs CORS headers, or the browser
	// reports an opaque network error instead of the real reason.
	s.handler = s.cors(s.authenticate(mux))
	return s
}

// Patterns is the routing table this server registered, for the contract check.
func (s *Server) Patterns() []string { return s.patterns }

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.handler.ServeHTTP(w, r)
}

// --- identity ----------------------------------------------------------------

type identityKey struct{}

func identityOf(r *http.Request) *auth.Identity {
	id, _ := r.Context().Value(identityKey{}).(*auth.Identity)
	return id
}

func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		gated := strings.HasPrefix(path, API) && !publicPaths[path] &&
			r.Method != http.MethodOptions && // never gate a CORS preflight
			s.Tokens.Enabled()
		if !gated {
			next.ServeHTTP(w, r)
			return
		}
		identity := s.Tokens.Resolve(auth.Bearer(r.Header.Get("Authorization")))
		if identity == nil {
			w.Header().Set("WWW-Authenticate", "Bearer")
			apierr.Unauthorized(
				"a bearer token is required; see `analog token add`").Write(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(
			context.WithValue(r.Context(), identityKey{}, identity)))
	})
}

// actorOf is SPEC §2.2: mandatory, no default, so a misconfigured agent fails loudly.
//
// With tokens configured the declared actor must also match the one the token
// identifies. The params stay required rather than being inferred: the contract says
// they are, and an agent writing under the wrong name should fail loudly rather than
// be silently corrected.
func actorOf(r *http.Request) (string, string, error) {
	q := r.URL.Query()
	name := q.Get("actor")
	if name == "" {
		name = r.Header.Get("X-Analog-Actor")
	}
	kind := q.Get("actor_kind")
	if kind == "" {
		kind = r.Header.Get("X-Analog-Actor-Kind")
	}
	if name == "" || kind == "" {
		return "", "", apierr.ActorRequired(
			"actor and actor_kind are required on every mutation")
	}
	if kind != "human" && kind != "agent" {
		return "", "", apierr.ValidationFailed("actor_kind must be 'human' or 'agent'",
			apierr.Detail{"actor_kind": kind})
	}

	identity := identityOf(r)
	if identity == nil || (name == identity.Actor && kind == identity.ActorKind) {
		return name, kind, nil
	}
	var message string
	if name == identity.Actor {
		// Much the commonest case: ANALOG_ACTOR_KIND defaults to "agent" and the
		// operator is a human. Saying "not 'kai', not 'kai'" helps nobody.
		message = "this token writes as actor_kind='" + identity.ActorKind +
			"', not '" + kind + "' — set ANALOG_ACTOR_KIND=" + identity.ActorKind
	} else {
		message = "this token writes as '" + identity.Actor + "' (" + identity.ActorKind +
			"), not '" + name + "' (" + kind + ")"
	}
	return "", "", apierr.Forbidden(message, apierr.Detail{
		"token_actor": identity.Actor, "token_actor_kind": identity.ActorKind,
		"requested_actor": name, "requested_actor_kind": kind,
	})
}

// --- responses ---------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, body any) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	// Card text is routinely HTML. Escaping it would still parse, but the wire
	// should say what the client sent.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(body); err != nil {
		fail(w, apierr.ValidationFailed("could not encode the response"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(buf.String()))
}

// fail renders any error as the contract's Error body. An error that is not an
// *apierr.Error is a bug rather than a contract case, so it becomes a 500 with the
// same shape instead of net/http's plain-text default.
func fail(w http.ResponseWriter, err error) {
	if e, ok := err.(*apierr.Error); ok {
		e.Write(w)
		return
	}
	(&apierr.Error{Status: http.StatusInternalServerError, Code: "internal",
		Message: err.Error()}).Write(w)
}

// decode reads a JSON body, remapping a malformed one to the contract's 400.
// Whatever net/http produces natively is not the Error schema.
func decode(r *http.Request, into any) error {
	dec := json.NewDecoder(r.Body)
	dec.UseNumber()
	if err := dec.Decode(into); err != nil {
		return apierr.ValidationFailed("request did not match the schema",
			apierr.Detail{"errors": []string{err.Error()}})
	}
	return nil
}
