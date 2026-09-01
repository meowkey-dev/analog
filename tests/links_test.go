// SPEC §3 link endpoints.
package conformance

import (
	"net/url"
	"testing"
)

// linksSpace starts a fresh server with a demo space and two cards; returns the
// server and their ids.
func linksSpace(t *testing.T) (*server, string, string) {
	t.Helper()
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	nodes := addCards(t, s, "demo", []any{
		map[string]any{"title": "A", "content": "a"},
		map[string]any{"title": "B", "content": "b"}}, nil)
	return s, asStr(asMap(nodes[0])["id"]), asStr(asMap(nodes[1])["id"])
}

func postLinks(t *testing.T, s *server, slug string, edges []any, actor url.Values) []any {
	t.Helper()
	r := s.post(t, "/api/spaces/"+slug+"/links", actor, map[string]any{"edges": edges})
	if r.status != 201 {
		t.Fatalf("post links: %d %s", r.status, r.str())
	}
	return r.arr()
}

func TestLinks_CreateReturnsJsonCanvasEdges(t *testing.T) {
	s, a, b := linksSpace(t)
	edge := asMap(postLinks(t, s, "demo", []any{map[string]any{
		"fromNode": a, "toNode": b, "fromSide": "right", "toSide": "left",
		"label": "contradicts"}}, agentP())[0])
	assertValid(t, edge, "Edge", false)
	if !hasPrefix(asStr(edge["id"]), "l_") {
		t.Errorf("id = %v", edge["id"])
	}
	if asStr(edge["fromNode"]) != a || asStr(edge["toNode"]) != b {
		t.Errorf("endpoints = %s", canonical(edge))
	}
	if asStr(edge["label"]) != "contradicts" {
		t.Errorf("label = %v", edge["label"])
	}
	if asStr(edge["sp_created_by"]) != "claude-code" {
		t.Errorf("sp_created_by = %v", edge["sp_created_by"])
	}
}

func TestLinks_BulkCreate(t *testing.T) {
	s, a, b := linksSpace(t)
	edges := postLinks(t, s, "demo", []any{
		map[string]any{"fromNode": a, "toNode": b, "label": "one"},
		map[string]any{"fromNode": b, "toNode": a, "label": "two"}}, agentP())
	if len(edges) != 2 ||
		asStr(asMap(edges[0])["label"]) != "one" || asStr(asMap(edges[1])["label"]) != "two" {
		t.Errorf("labels = %v", canonical(edges))
	}
	distinct := map[string]bool{}
	for _, e := range edges {
		distinct[asStr(asMap(e)["id"])] = true
	}
	if len(distinct) != 2 {
		t.Errorf("%d distinct ids for 2 edges", len(distinct))
	}
}

func TestLinks_AppearOnTheCanvas(t *testing.T) {
	s, a, b := linksSpace(t)
	edge := postLinks(t, s, "demo", []any{
		map[string]any{"fromNode": a, "toNode": b}}, agentP())[0]
	assertJSONEq(t, "canvas edges", []any{edge},
		s.get(t, "/api/spaces/demo/canvas", nil).obj()["edges"])
}

func TestLinks_DanglingEndpointsAre404(t *testing.T) {
	for _, bad := range []string{"from", "to"} {
		t.Run(bad, func(t *testing.T) {
			s, a, b := linksSpace(t)
			edge := map[string]any{"fromNode": a, "toNode": b}
			if bad == "from" {
				edge["fromNode"] = "c_nope"
			} else {
				edge["toNode"] = "c_nope"
			}
			r := s.post(t, "/api/spaces/demo/links", agentP(), map[string]any{"edges": []any{edge}})
			if r.status != 404 {
				t.Fatalf("%d %s", r.status, r.str())
			}
			if asStr(r.obj()["error"]) != "not_found" {
				t.Errorf("error = %v", r.obj()["error"])
			}
		})
	}
}

func TestLinks_ALinkToADeletedCardIs404(t *testing.T) {
	s, a, b := linksSpace(t)
	s.delete(t, "/api/spaces/demo/cards/"+b, humanP())
	r := s.post(t, "/api/spaces/demo/links", agentP(),
		map[string]any{"edges": []any{map[string]any{"fromNode": a, "toNode": b}}})
	if r.status != 404 {
		t.Fatalf("%d %s", r.status, r.str())
	}
}

func TestLinks_ARejectedBatchCreatesNothing(t *testing.T) {
	s, a, b := linksSpace(t)
	s.post(t, "/api/spaces/demo/links", agentP(), map[string]any{"edges": []any{
		map[string]any{"fromNode": a, "toNode": b, "label": "good"},
		map[string]any{"fromNode": a, "toNode": "c_nope", "label": "bad"}}})
	if got := asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["edges"]); len(got) != 0 {
		t.Errorf("canvas edges = %v", canonical(got))
	}
	for _, e := range eventsOf(t, s, "demo", "0") {
		if asStr(asMap(e)["type"]) == "link.created" {
			t.Fatal("a rejected batch emitted link.created")
		}
	}
}

func TestLinks_DeleteRemovesTheLinkFromTheCanvas(t *testing.T) {
	s, a, b := linksSpace(t)
	edge := asMap(postLinks(t, s, "demo", []any{
		map[string]any{"fromNode": a, "toNode": b}}, agentP())[0])
	if r := s.delete(t, "/api/spaces/demo/links/"+asStr(edge["id"]), humanP()); r.status != 204 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if got := asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["edges"]); len(got) != 0 {
		t.Errorf("canvas edges = %v", canonical(got))
	}
	if got := lastEventType(t, s, "demo"); got != "link.deleted" {
		t.Errorf("last event = %q", got)
	}
}

func TestLinks_DeletingTwiceIs404(t *testing.T) {
	s, a, b := linksSpace(t)
	edge := asMap(postLinks(t, s, "demo", []any{
		map[string]any{"fromNode": a, "toNode": b}}, agentP())[0])
	s.delete(t, "/api/spaces/demo/links/"+asStr(edge["id"]), humanP())
	if r := s.delete(t, "/api/spaces/demo/links/"+asStr(edge["id"]), humanP()); r.status != 404 {
		t.Fatalf("%d", r.status)
	}
}

func TestLinks_DeletingACardLeavesItsLinksAlone(t *testing.T) {
	// Soft delete means the edge still has both endpoints on the deleted canvas.
	s, a, b := linksSpace(t)
	edge := asMap(postLinks(t, s, "demo", []any{
		map[string]any{"fromNode": a, "toNode": b}}, agentP())[0])
	s.delete(t, "/api/spaces/demo/cards/"+a, humanP())
	full := s.get(t, "/api/spaces/demo/canvas", params("include_deleted", "true")).obj()
	found := false
	for _, e := range asArr(full["edges"]) {
		if asStr(asMap(e)["id"]) == asStr(edge["id"]) {
			found = true
		}
	}
	if !found {
		t.Error("the edge vanished from the deleted canvas")
	}
}

func TestLinks_DeletedLinksAreAbsentEvenWithIncludeDeleted(t *testing.T) {
	// include_deleted is a card-tombstone flag (node-only sp_deleted_at). A
	// deleted link has no wire shape marking it, so returning one would give the
	// client an edge indistinguishable from a live one.
	s, a, b := linksSpace(t)
	edge := asMap(postLinks(t, s, "demo", []any{
		map[string]any{"fromNode": a, "toNode": b}}, agentP())[0])
	if r := s.delete(t, "/api/spaces/demo/links/"+asStr(edge["id"]), humanP()); r.status != 204 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	full := s.get(t, "/api/spaces/demo/canvas", params("include_deleted", "true")).obj()
	for _, e := range asArr(full["edges"]) {
		if asStr(asMap(e)["id"]) == asStr(edge["id"]) {
			t.Error("a deleted link came back under include_deleted")
		}
	}
}
