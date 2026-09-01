// SPEC §2.2 / §10: actor is mandatory on every mutation and has no default.
//
// 'A misconfigured agent should fail loudly, not write anonymously.'
package conformance

import (
	"net/url"
	"testing"
)

type actorMutation struct {
	method string
	path   string
	body   any
}

// actorSpace builds the fixture surface every mutation operates on: a card, a
// self-link, and an annotation.
func actorSpace(t *testing.T) (*server, map[string]actorMutation) {
	t.Helper()
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	card := oneCard(t, s, "demo")
	link := asMap(s.post(t, "/api/spaces/demo/links", agentP(), map[string]any{
		"edges": []any{map[string]any{
			"fromNode": card["id"], "toNode": card["id"], "label": "self"}}}).arr()[0])
	ann := asMap(s.post(t, "/api/spaces/demo/annotations", humanP(), map[string]any{
		"card_id": card["id"], "body": "hi"}).body)

	c, l, a := asStr(card["id"]), asStr(link["id"]), asStr(ann["id"])
	mutations := map[string]actorMutation{
		"createSpace":       {"POST", "/api/spaces", map[string]any{"slug": "other", "title": "O"}},
		"updateSpace":       {"PATCH", "/api/spaces/demo", map[string]any{"title": "renamed"}},
		"deleteSpace":       {"DELETE", "/api/spaces/demo", nil},
		"importCanvas":      {"POST", "/api/spaces/demo/import", map[string]any{"nodes": []any{}, "edges": []any{}}},
		"createCards":       {"POST", "/api/spaces/demo/cards", map[string]any{"cards": []any{map[string]any{"title": "t", "content": "c"}}}},
		"updateCard":        {"PATCH", "/api/spaces/demo/cards/" + c, map[string]any{"x": 10}},
		"deleteCard":        {"DELETE", "/api/spaces/demo/cards/" + c, nil},
		"createLinks":       {"POST", "/api/spaces/demo/links", map[string]any{"edges": []any{map[string]any{"fromNode": card["id"], "toNode": card["id"]}}}},
		"deleteLink":        {"DELETE", "/api/spaces/demo/links/" + l, nil},
		"createAnnotation":  {"POST", "/api/spaces/demo/annotations", map[string]any{"card_id": card["id"], "body": "b"}},
		"resolveAnnotation": {"PATCH", "/api/spaces/demo/annotations/" + a, map[string]any{"resolved": true}},
	}
	return s, mutations
}

var actorOperations = []string{
	"createSpace", "updateSpace", "deleteSpace", "importCanvas", "createCards",
	"updateCard", "deleteCard", "createLinks", "deleteLink", "createAnnotation",
	"resolveAnnotation",
}

func TestActor_MutationsRejectAMissingActor(t *testing.T) {
	s, mutations := actorSpace(t)
	for _, op := range actorOperations {
		for _, tc := range []struct {
			name   string
			params [][2]string
		}{
			{"neither", nil},
			{"no-actor_kind", [][2]string{{"actor", "claude-code"}}},
			{"no-actor", [][2]string{{"actor_kind", "agent"}}},
			{"empty-actor", [][2]string{{"actor", ""}, {"actor_kind", "agent"}}},
		} {
			t.Run(op+"/"+tc.name, func(t *testing.T) {
				m := mutations[op]
				q := params()
				for _, kv := range tc.params {
					q.Set(kv[0], kv[1])
				}
				r := s.do(t, m.method, m.path, q, nil, "", m.body)
				if r.status != 400 {
					t.Fatalf("%s accepted %v: %d %s", op, tc.params, r.status, r.str())
				}
				if asStr(r.obj()["error"]) != "actor_required" {
					t.Errorf("error = %v", r.obj()["error"])
				}
			})
		}
	}
}

func TestActor_MutationsRejectAnUnknownActorKind(t *testing.T) {
	s, mutations := actorSpace(t)
	for _, op := range actorOperations {
		t.Run(op, func(t *testing.T) {
			m := mutations[op]
			r := s.do(t, m.method, m.path, params("actor", "x", "actor_kind", "robot"),
				nil, "", m.body)
			if r.status != 400 {
				t.Fatalf("%s accepted actor_kind=robot: %d %s", op, r.status, r.str())
			}
		})
	}
}

func TestActor_MediaUploadRequiresAnActor(t *testing.T) {
	s, _ := actorSpace(t)
	r := uploadTo(t, s, "/api/spaces/demo/media", "a.png", "image/png",
		[]byte("\x89PNG"), url.Values{}, nil)
	if r.status != 400 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if asStr(r.obj()["error"]) != "actor_required" {
		t.Errorf("error = %v", r.obj()["error"])
	}
}

func TestActor_FeedbackRequiresAnActor(t *testing.T) {
	s, _ := actorSpace(t)
	r := s.get(t, "/api/spaces/demo/feedback", nil)
	if r.status != 400 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if asStr(r.obj()["error"]) != "actor_required" {
		t.Errorf("error = %v", r.obj()["error"])
	}
}

func TestActor_ReadsDoNotRequireAnActor(t *testing.T) {
	s, _ := actorSpace(t)
	for _, path := range []string{
		"/api/spaces", "/api/spaces/demo", "/api/spaces/demo/canvas",
		"/api/spaces/demo/annotations", "/api/spaces/demo/events",
	} {
		t.Run(path, func(t *testing.T) {
			if r := s.get(t, path, nil); r.status != 200 {
				t.Errorf("%s: %d", path, r.status)
			}
		})
	}
}

func TestActor_ActorIsRecordedOnWhatItWrites(t *testing.T) {
	s, _ := actorSpace(t)
	makeSpace(t, s, "attrib", "Attrib", "")
	card := oneCard(t, s, "attrib")
	if asStr(card["sp_created_by"]) != "claude-code" {
		t.Errorf("sp_created_by = %v", card["sp_created_by"])
	}
	log := eventsOf(t, s, "attrib", "0")
	ev := asMap(log[len(log)-1])
	if asStr(ev["actor"]) != "claude-code" || asStr(ev["actor_kind"]) != "agent" {
		t.Errorf("attribution = %s", canonical(ev))
	}
}
