// SPEC §3 space endpoints.
package conformance

import (
	"fmt"
	"testing"
)

func TestSpaces_CreateReturns201AndAValidSpace(t *testing.T) {
	s := startServer(t)
	space := makeSpace(t, s, "redesign", "Nav redesign", "")
	assertValid(t, space, "Space", false)
	if asStr(space["slug"]) != "redesign" || asStr(space["title"]) != "Nav redesign" {
		t.Errorf("space = %s", canonical(space))
	}
	if asStr(space["revision_mode"]) != "replace" { // SPEC §2.4 default
		t.Errorf("revision_mode = %v", space["revision_mode"])
	}
	if got := num(t, space["seq"]); got != 1 {
		t.Errorf("seq = %v, want 1: seq 1 is the space.created event", got)
	}
	if !hasPrefix(asStr(space["id"]), "s_") {
		t.Errorf("id = %v", space["id"])
	}
}

func hasPrefix(s, prefix string) bool { return len(s) >= len(prefix) && s[:len(prefix)] == prefix }

func TestSpaces_CreateAcceptsBranchMode(t *testing.T) {
	s := startServer(t)
	if got := asStr(makeSpace(t, s, "b", "B", "branch")["revision_mode"]); got != "branch" {
		t.Errorf("revision_mode = %q", got)
	}
}

func TestSpaces_DuplicateSlugIs409(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "dup", "Demo", "")
	r := s.post(t, "/api/spaces", humanP(), map[string]any{"slug": "dup", "title": "again"})
	if r.status != 409 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if asStr(r.obj()["error"]) != "conflict" {
		t.Errorf("error = %v", r.obj()["error"])
	}
}

func TestSpaces_InvalidSlugsAreRejected(t *testing.T) {
	s := startServer(t)
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'a'
	}
	for _, slug := range []string{"Has-Caps", "has space", "has_underscore", "",
		string(long), "café"} {
		t.Run(fmt.Sprintf("%q", slug), func(t *testing.T) {
			r := s.post(t, "/api/spaces", humanP(), map[string]any{"slug": slug, "title": "T"})
			if r.status != 400 {
				t.Fatalf("%q was accepted: %d", slug, r.status)
			}
			if asStr(r.obj()["error"]) != "validation_failed" {
				t.Errorf("error = %v", r.obj()["error"])
			}
		})
	}
}

func TestSpaces_GetUnknownSpaceIs404(t *testing.T) {
	s := startServer(t)
	r := s.get(t, "/api/spaces/nope", nil)
	if r.status != 404 {
		t.Fatalf("%d", r.status)
	}
	if asStr(r.obj()["error"]) != "not_found" {
		t.Errorf("error = %v", r.obj()["error"])
	}
}

func TestSpaces_CountsTrackLiveRowsOnly(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "counts", "Counts", "")
	nodes := addCards(t, s, "counts", []any{
		map[string]any{"title": "A", "content": "a"},
		map[string]any{"title": "B", "content": "b"}}, nil)
	a, b := asMap(nodes[0]), asMap(nodes[1])
	s.post(t, "/api/spaces/counts/links", agentP(), map[string]any{
		"edges": []any{map[string]any{
			"fromNode": a["id"], "toNode": b["id"], "label": "x"}}})
	ann := asMap(s.post(t, "/api/spaces/counts/annotations", humanP(), map[string]any{
		"card_id": a["id"], "body": "look"}).body)

	assertJSONEq(t, "counts", jlit(t, `{"cards": 2, "links": 1, "open_annotations": 1}`),
		s.get(t, "/api/spaces/counts", nil).obj()["counts"])

	s.delete(t, "/api/spaces/counts/cards/"+asStr(b["id"]), humanP())
	s.patch(t, "/api/spaces/counts/annotations/"+asStr(ann["id"]), agentP(),
		map[string]any{"resolved": true})
	assertJSONEq(t, "counts", jlit(t, `{"cards": 1, "links": 1, "open_annotations": 0}`),
		s.get(t, "/api/spaces/counts", nil).obj()["counts"])
}

func TestSpaces_PatchUpdatesTitleAndRevisionMode(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "p", "P", "")
	r := s.patch(t, "/api/spaces/p", humanP(),
		map[string]any{"title": "New", "revision_mode": "branch"})
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if asStr(r.obj()["title"]) != "New" {
		t.Errorf("title = %v", r.obj()["title"])
	}
	if got := asStr(s.get(t, "/api/spaces/p", nil).obj()["revision_mode"]); got != "branch" {
		t.Errorf("revision_mode = %q", got)
	}
}

func TestSpaces_SpaceCreatedNamesTheSpace(t *testing.T) {
	// SPEC §3 / AMENDMENTS #5: a space's log opens with its own creation.
	s := startServer(t)
	makeSpace(t, s, "born", "Born", "")
	event := asMap(asArr(s.get(t, "/api/spaces/born/events", nil).obj()["events"])[0])
	if num(t, event["seq"]) != 1 || asStr(event["type"]) != "space.created" {
		t.Fatalf("first event: %s", canonical(event))
	}
	assertJSONEq(t, "payload", jlit(t, `{"slug": "born", "title": "Born"}`), event["payload"])
	if asStr(event["actor"]) != "human" || asStr(event["actor_kind"]) != "human" {
		t.Errorf("attribution: %s", canonical(event))
	}
}

func TestSpaces_DeleteRemovesTheSpaceAndItsContents(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "gone", "Gone", "")
	oneCard(t, s, "gone")
	if r := s.delete(t, "/api/spaces/gone", humanP()); r.status != 204 {
		t.Fatalf("delete: %d %s", r.status, r.str())
	}
	if r := s.get(t, "/api/spaces/gone", nil); r.status != 404 {
		t.Errorf("after delete: %d", r.status)
	}
	if got := s.get(t, "/api/spaces", nil).arr(); len(got) != 0 {
		t.Errorf("spaces after delete = %v", canonical(got))
	}
}

func TestSpaces_SpaceSeqIsTheEventCounter(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "seq", "Seq", "")
	if got := num(t, s.get(t, "/api/spaces/seq", nil).obj()["seq"]); got != 1 {
		t.Errorf("seq after create = %v", got) // space.created
	}
	oneCard(t, s, "seq")
	if got := num(t, s.get(t, "/api/spaces/seq", nil).obj()["seq"]); got != 2 {
		t.Errorf("seq after one card = %v", got)
	}
	oneCard(t, s, "seq")
	if got := num(t, s.get(t, "/api/spaces/seq", nil).obj()["seq"]); got != 3 {
		t.Errorf("seq after two cards = %v", got)
	}
}

func TestSpaces_SeqIsPerSpace(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "one", "One", "")
	makeSpace(t, s, "two", "Two", "")
	oneCard(t, s, "one")
	oneCard(t, s, "one")
	oneCard(t, s, "two")
	if got := num(t, s.get(t, "/api/spaces/one", nil).obj()["seq"]); got != 3 {
		t.Errorf("one's seq = %v, want 3", got) // created + 2 cards
	}
	if got := num(t, s.get(t, "/api/spaces/two", nil).obj()["seq"]); got != 2 {
		t.Errorf("two's seq = %v, want 2", got) // created + 1 card
	}
	log := asArr(s.get(t, "/api/spaces/two/events", nil).obj()["events"])
	if len(log) != 2 ||
		num(t, asMap(log[0])["seq"]) != 1 || asStr(asMap(log[0])["type"]) != "space.created" ||
		num(t, asMap(log[1])["seq"]) != 2 || asStr(asMap(log[1])["type"]) != "card.created" {
		t.Errorf("two's log = %v", canonical(log))
	}
}
