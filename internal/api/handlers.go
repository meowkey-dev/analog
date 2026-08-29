package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/meowkey-dev/analog/internal/apierr"
	"github.com/meowkey-dev/analog/internal/sse"
	"github.com/meowkey-dev/analog/internal/store"
)

// handle registers one route and remembers the pattern, so contract_test.go can
// check the routing table against contracts/openapi.json rather than trusting it.
func (s *Server) handle(mux *http.ServeMux, pattern string, h http.HandlerFunc) {
	s.patterns = append(s.patterns, pattern)
	mux.HandleFunc(pattern, h)
}

func (s *Server) routes(mux *http.ServeMux) {
	s.patterns = nil
	// --- connection ----------------------------------------------------------
	s.handle(mux, "GET "+API+"/health", s.health)
	s.handle(mux, "GET "+API+"/whoami", s.whoami)

	// --- spaces --------------------------------------------------------------
	s.handle(mux, "GET "+API+"/spaces", s.listSpaces)
	s.handle(mux, "POST "+API+"/spaces", s.createSpace)
	s.handle(mux, "GET "+API+"/spaces/{slug}", s.getSpace)
	s.handle(mux, "PATCH "+API+"/spaces/{slug}", s.updateSpace)
	s.handle(mux, "DELETE "+API+"/spaces/{slug}", s.deleteSpace)

	// --- canvas --------------------------------------------------------------
	s.handle(mux, "GET "+API+"/spaces/{slug}/canvas", s.getCanvas)
	s.handle(mux, "POST "+API+"/spaces/{slug}/import", s.importCanvas)

	// --- cards ---------------------------------------------------------------
	s.handle(mux, "POST "+API+"/spaces/{slug}/cards", s.createCards)
	s.handle(mux, "PATCH "+API+"/spaces/{slug}/cards/{card_id}", s.updateCard)
	s.handle(mux, "DELETE "+API+"/spaces/{slug}/cards/{card_id}", s.deleteCard)

	// --- links ---------------------------------------------------------------
	s.handle(mux, "POST "+API+"/spaces/{slug}/links", s.createLinks)
	s.handle(mux, "DELETE "+API+"/spaces/{slug}/links/{link_id}", s.deleteLink)

	// --- annotations ---------------------------------------------------------
	s.handle(mux, "GET "+API+"/spaces/{slug}/annotations", s.listAnnotations)
	s.handle(mux, "POST "+API+"/spaces/{slug}/annotations", s.createAnnotation)
	s.handle(mux, "PATCH "+API+"/spaces/{slug}/annotations/{annotation_id}", s.resolveAnnotation)

	// --- feedback, events ----------------------------------------------------
	s.handle(mux, "GET "+API+"/spaces/{slug}/feedback", s.getFeedback)
	s.handle(mux, "GET "+API+"/spaces/{slug}/events", s.listEvents)
	s.handle(mux, "GET "+API+"/spaces/{slug}/events/stream", s.streamEvents)

	// --- media ---------------------------------------------------------------
	s.handle(mux, "POST "+API+"/spaces/{slug}/media", s.uploadMedia)
	s.handle(mux, "GET "+API+"/spaces/{slug}/media/{filename}", s.getMedia)

	// Everything else is the SPA.
	s.handle(mux, "/", s.serveWeb)
}

// --- connection --------------------------------------------------------------

// health is unauthenticated on purpose: a client has to be able to find out whether
// this server exists and whether it wants a token before it has one.
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "service": "analog", "version": Version,
		"auth_required": s.Tokens.Enabled(),
	})
}

// whoami reports who this token writes as. A null identity means the server has no
// tokens at all.
func (s *Server) whoami(w http.ResponseWriter, r *http.Request) {
	identity := identityOf(r)
	if identity == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated": false, "actor": nil, "actor_kind": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated": true, "actor": identity.Actor, "actor_kind": identity.ActorKind})
}

// --- spaces --------------------------------------------------------------------

func (s *Server) listSpaces(w http.ResponseWriter, r *http.Request) {
	spaces, err := s.Store.ListSpaces()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, spaces)
}

func (s *Server) createSpace(w http.ResponseWriter, r *http.Request) {
	actor, kind, err := actorOf(r)
	if err != nil {
		fail(w, err)
		return
	}
	var body struct {
		Slug         string `json:"slug"`
		Title        string `json:"title"`
		RevisionMode string `json:"revision_mode"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	space, err := s.Store.CreateSpace(body.Slug, body.Title, body.RevisionMode, actor, kind)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, space)
}

func (s *Server) getSpace(w http.ResponseWriter, r *http.Request) {
	space, err := s.Store.Space(r.PathValue("slug"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, space)
}

func (s *Server) updateSpace(w http.ResponseWriter, r *http.Request) {
	if _, _, err := actorOf(r); err != nil {
		fail(w, err)
		return
	}
	var patch store.SpacePatch
	if err := decode(r, &patch); err != nil {
		fail(w, err)
		return
	}
	space, err := s.Store.UpdateSpace(r.PathValue("slug"), patch)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, space)
}

func (s *Server) deleteSpace(w http.ResponseWriter, r *http.Request) {
	actor, kind, err := actorOf(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.Store.DeleteSpace(r.PathValue("slug"), actor, kind); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- canvas ---------------------------------------------------------------------

func (s *Server) getCanvas(w http.ResponseWriter, r *http.Request) {
	includeDeleted, err := boolParam(r, "include_deleted", false)
	if err != nil {
		fail(w, err)
		return
	}
	canvas, err := s.Store.Canvas(r.PathValue("slug"), includeDeleted)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, canvas)
}

func (s *Server) importCanvas(w http.ResponseWriter, r *http.Request) {
	actor, kind, err := actorOf(r)
	if err != nil {
		fail(w, err)
		return
	}
	var canvas store.Canvas
	if err := decode(r, &canvas); err != nil {
		fail(w, err)
		return
	}
	result, err := s.Store.ImportCanvas(r.PathValue("slug"), canvas, actor, kind)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

// --- cards ----------------------------------------------------------------------

func (s *Server) createCards(w http.ResponseWriter, r *http.Request) {
	actor, kind, err := actorOf(r)
	if err != nil {
		fail(w, err)
		return
	}
	// Decoded in two steps so a draft can reject keys it does not know while the
	// envelope stays permissive, exactly as the pydantic models do.
	var body struct {
		Cards *[]json.RawMessage `json:"cards"`
		Nodes *[]store.Node      `json:"nodes"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	var drafts []store.CardDraft
	if body.Cards != nil {
		drafts = make([]store.CardDraft, 0, len(*body.Cards))
		for _, raw := range *body.Cards {
			var draft store.CardDraft
			dec := json.NewDecoder(strings.NewReader(string(raw)))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&draft); err != nil {
				fail(w, apierr.ValidationFailed("request did not match the schema",
					apierr.Detail{"errors": []string{err.Error()}}))
				return
			}
			drafts = append(drafts, draft)
		}
	}
	var nodes []store.Node
	if body.Nodes != nil {
		nodes = *body.Nodes
		if nodes == nil {
			nodes = []store.Node{}
		}
	}
	built, err := s.Store.CreateCards(r.PathValue("slug"), drafts, nodes, actor, kind)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, built)
}

func (s *Server) updateCard(w http.ResponseWriter, r *http.Request) {
	actor, kind, err := actorOf(r)
	if err != nil {
		fail(w, err)
		return
	}
	var patch store.Node
	if err := decode(r, &patch); err != nil {
		fail(w, err)
		return
	}
	if patch == nil {
		patch = store.Node{}
	}
	ifMatch, err := parseIfMatch(r.Header.Get("If-Match"))
	if err != nil {
		fail(w, err)
		return
	}
	node, err := s.Store.UpdateCard(r.PathValue("slug"), r.PathValue("card_id"), patch,
		actor, kind, r.URL.Query().Get("mode"), ifMatch)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func (s *Server) deleteCard(w http.ResponseWriter, r *http.Request) {
	actor, kind, err := actorOf(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.Store.DeleteCard(r.PathValue("slug"), r.PathValue("card_id"),
		actor, kind); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- links ----------------------------------------------------------------------

func (s *Server) createLinks(w http.ResponseWriter, r *http.Request) {
	actor, kind, err := actorOf(r)
	if err != nil {
		fail(w, err)
		return
	}
	var body struct {
		Edges []store.Edge `json:"edges"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	// exclude_none: a null side or label is not a value the store should see.
	edges := make([]store.Edge, 0, len(body.Edges))
	for _, edge := range body.Edges {
		out := store.Edge{}
		for k, v := range edge {
			if v != nil {
				out[k] = v
			}
		}
		edges = append(edges, out)
	}
	built, err := s.Store.CreateLinks(r.PathValue("slug"), edges, actor, kind)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, built)
}

func (s *Server) deleteLink(w http.ResponseWriter, r *http.Request) {
	actor, kind, err := actorOf(r)
	if err != nil {
		fail(w, err)
		return
	}
	if err := s.Store.DeleteLink(r.PathValue("slug"), r.PathValue("link_id"),
		actor, kind); err != nil {
		fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- annotations ------------------------------------------------------------------

func (s *Server) listAnnotations(w http.ResponseWriter, r *http.Request) {
	resolved, err := optionalBoolParam(r, "resolved")
	if err != nil {
		fail(w, err)
		return
	}
	annotations, err := s.Store.Annotations(r.PathValue("slug"), resolved,
		r.URL.Query().Get("card_id"))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, annotations)
}

func (s *Server) createAnnotation(w http.ResponseWriter, r *http.Request) {
	actor, kind, err := actorOf(r)
	if err != nil {
		fail(w, err)
		return
	}
	var body struct {
		CardID     string         `json:"card_id"`
		Body       string         `json:"body"`
		Selector   map[string]any `json:"selector"`
		Motivation string         `json:"motivation"`
	}
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	annotation, err := s.Store.CreateAnnotation(r.PathValue("slug"), body.CardID,
		body.Body, body.Selector, body.Motivation, actor, kind)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, annotation)
}

func (s *Server) resolveAnnotation(w http.ResponseWriter, r *http.Request) {
	actor, kind, err := actorOf(r)
	if err != nil {
		fail(w, err)
		return
	}
	// SPEC §4.1/§4.2: every caller surface only ever resolves, so a body with no
	// `resolved` key resolves. `resolved: false` reopens, silently.
	body := struct {
		Resolved *bool   `json:"resolved"`
		Reply    *string `json:"reply"`
	}{}
	if err := decode(r, &body); err != nil {
		fail(w, err)
		return
	}
	resolved := true
	if body.Resolved != nil {
		resolved = *body.Resolved
	}
	annotation, err := s.Store.ResolveAnnotation(r.PathValue("slug"),
		r.PathValue("annotation_id"), resolved, body.Reply, actor, kind)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, annotation)
}

// --- feedback, events -------------------------------------------------------------

func (s *Server) getFeedback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	actor := q.Get("actor")
	if actor == "" {
		actor = r.Header.Get("X-Analog-Actor")
	}
	if actor == "" {
		fail(w, apierr.ActorRequired("actor is required: a cursor is keyed by actor name"))
		return
	}
	since, err := optionalIntParam(r, "since")
	if err != nil {
		fail(w, err)
		return
	}
	advance, err := boolParam(r, "advance", true)
	if err != nil {
		fail(w, err)
		return
	}
	feedback, err := s.Store.Feedback(r.PathValue("slug"), actor, since, advance)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, feedback)
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	since, err := intParam(r, "since", 0)
	if err != nil {
		fail(w, err)
		return
	}
	limit, err := intParam(r, "limit", 200)
	if err != nil {
		fail(w, err)
		return
	}
	page, err := s.Store.ListEvents(r.PathValue("slug"), since, clamp(int(limit), 1, 1000))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, page)
}

func (s *Server) streamEvents(w http.ResponseWriter, r *http.Request) {
	spaceID, err := s.Store.SpaceID(r.PathValue("slug"))
	if err != nil {
		fail(w, err)
		return
	}
	since, err := intParam(r, "since", 0)
	if err != nil {
		fail(w, err)
		return
	}
	// Last-Event-ID wins when it is a number: a reconnecting browser resumes from
	// what it actually received, not from what the URL happened to say.
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			since = n
		}
	}
	sse.Stream(w, r, s.Store, s.Broker, spaceID, since)
}

// --- media --------------------------------------------------------------------------

func (s *Server) uploadMedia(w http.ResponseWriter, r *http.Request) {
	if _, _, err := actorOf(r); err != nil {
		fail(w, err)
		return
	}
	// A little headroom over the cap so an oversized upload is rejected by the
	// store's own message rather than by the multipart reader.
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		fail(w, apierr.ValidationFailed("expected a multipart upload with a `file` part",
			apierr.Detail{"errors": []string{err.Error()}}))
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		fail(w, apierr.ValidationFailed("expected a multipart upload with a `file` part"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, int64(maxUploadRead)))
	if err != nil {
		fail(w, err)
		return
	}
	media, err := s.Store.SaveMedia(r.PathValue("slug"),
		header.Header.Get("Content-Type"), data)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, media)
}

func (s *Server) getMedia(w http.ResponseWriter, r *http.Request) {
	path, contentType, err := s.Store.MediaPath(r.PathValue("slug"), r.PathValue("filename"))
	if err != nil {
		fail(w, err)
		return
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeFile(w, r, path)
}

// --- parameters ---------------------------------------------------------------------

// maxUploadRead is one byte past the cap, so the store still sees an oversized
// upload as oversized and can say so.
const maxUploadRead = 25*1024*1024 + 1

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// truthy accepts what a form-encoded boolean can look like, matching the framework
// the Python server used.
func truthy(raw string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	}
	return false, false
}

func boolParam(r *http.Request, name string, fallback bool) (bool, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, ok := truthy(raw)
	if !ok {
		return false, apierr.ValidationFailed("request did not match the schema",
			apierr.Detail{"errors": []string{name + " must be a boolean"}})
	}
	return value, nil
}

func optionalBoolParam(r *http.Request, name string) (*bool, error) {
	if r.URL.Query().Get(name) == "" {
		return nil, nil
	}
	value, err := boolParam(r, name, false)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func intParam(r *http.Request, name string, fallback int64) (int64, error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, apierr.ValidationFailed("request did not match the schema",
			apierr.Detail{"errors": []string{name + " must be an integer"}})
	}
	return value, nil
}

func optionalIntParam(r *http.Request, name string) (*int64, error) {
	if r.URL.Query().Get(name) == "" {
		return nil, nil
	}
	value, err := intParam(r, name, 0)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

// parseIfMatch reads the header as an sp_rev. Weak validators and quoting are both
// tolerated; anything that is not an integer is a contract-shaped 400.
func parseIfMatch(raw string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	value := strings.Trim(strings.TrimPrefix(strings.TrimSpace(raw), "W/"), `"`)
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, apierr.ValidationFailed("If-Match must be an integer sp_rev",
			apierr.Detail{"if_match": raw})
	}
	return &n, nil
}
