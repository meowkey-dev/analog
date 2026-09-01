// SPEC §3 / §10 — import is additive only. There is no whole-canvas replace.
package conformance

import (
	"net/url"
	"testing"
)

func importSpace(t *testing.T) *server {
	t.Helper()
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	return s
}

func doImport(t *testing.T, s *server, canvas any, actor url.Values) map[string]any {
	t.Helper()
	if actor == nil {
		actor = agentP()
	}
	r := s.post(t, "/api/spaces/demo/import", actor, canvas)
	if r.status != 201 {
		t.Fatalf("import: %d %s", r.status, r.str())
	}
	return r.obj()
}

func TestImport_ImportCreatesCardsAndLinks(t *testing.T) {
	s := importSpace(t)
	canvas := fixture(t, "canvas.json")
	result := doImport(t, s, canvas, nil)
	got := s.get(t, "/api/spaces/demo/canvas", nil).obj()
	if len(asArr(got["nodes"])) != 6 || len(asArr(got["edges"])) != 4 {
		t.Fatalf("imported canvas: %d nodes, %d edges", len(asArr(got["nodes"])), len(asArr(got["edges"])))
	}
	want := map[string]bool{}
	for _, n := range asArr(asMap(canvas)["nodes"]) {
		want[asStr(asMap(n)["id"])] = true
	}
	for _, e := range asArr(asMap(canvas)["edges"]) {
		want[asStr(asMap(e)["id"])] = true
	}
	idMap := asMap(result["id_map"])
	if len(idMap) != len(want) {
		t.Fatalf("id_map has %d entries, want %d", len(idMap), len(want))
	}
	for id := range want {
		if _, ok := idMap[id]; !ok {
			t.Errorf("id_map is missing %q", id)
		}
	}
}

func TestImport_IdsAreRemapped(t *testing.T) {
	s := importSpace(t)
	canvas := asMap(fixture(t, "canvas.json"))
	result := doImport(t, s, canvas, nil)
	live := map[string]bool{}
	for _, n := range asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["nodes"]) {
		live[asStr(asMap(n)["id"])] = true
	}
	idMap := asMap(result["id_map"])
	for _, n := range asArr(canvas["nodes"]) {
		original := asStr(asMap(n)["id"])
		if live[original] {
			t.Errorf("original id %q survived the import", original)
		}
		if !live[asStr(idMap[original])] {
			t.Errorf("id_map[%q] = %v is not on the canvas", original, idMap[original])
		}
	}
}

func TestImport_EdgesAreRewiredToTheNewIds(t *testing.T) {
	s := importSpace(t)
	canvas := asMap(fixture(t, "canvas.json"))
	result := doImport(t, s, canvas, nil)
	idMap := asMap(result["id_map"])
	edges := map[string]any{}
	for _, e := range asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["edges"]) {
		edges[asStr(asMap(e)["id"])] = asMap(e)
	}
	for _, raw := range asArr(canvas["edges"]) {
		original := asMap(raw)
		edge := asMap(edges[asStr(idMap[asStr(original["id"])])])
		if edge == nil {
			t.Fatalf("no imported edge for %v", original["id"])
		}
		if asStr(edge["fromNode"]) != asStr(idMap[asStr(original["fromNode"])]) {
			t.Errorf("fromNode not rewired for %v", original["id"])
		}
		if asStr(edge["toNode"]) != asStr(idMap[asStr(original["toNode"])]) {
			t.Errorf("toNode not rewired for %v", original["id"])
		}
		if asStr(edge["label"]) != asStr(original["label"]) {
			t.Errorf("label lost for %v", original["id"])
		}
	}
}

func TestImport_ContentAndGeometrySurviveTheRoundTrip(t *testing.T) {
	s := importSpace(t)
	canvas := asMap(fixture(t, "canvas.json"))
	result := doImport(t, s, canvas, nil)
	idMap := asMap(result["id_map"])
	nodes := map[string]any{}
	for _, n := range asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["nodes"]) {
		nodes[asStr(asMap(n)["id"])] = asMap(n)
	}
	for _, raw := range asArr(canvas["nodes"]) {
		original := asMap(raw)
		got := asMap(nodes[asStr(idMap[asStr(original["id"])])])
		for _, key := range []string{"type", "x", "y", "width", "height", "text", "file",
			"sp_kind", "sp_title"} {
			if want, present := original[key]; present {
				if !jsonEqual(got[key], want) {
					t.Errorf("%v.%s = %v, want %v", original["id"], key, got[key], want)
				}
			}
		}
	}
}

func TestImport_ImportReattributesAndResetsRev(t *testing.T) {
	s := importSpace(t)
	doImport(t, s, fixture(t, "canvas.json"), nil)
	for _, n := range asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["nodes"]) {
		node := asMap(n)
		if asStr(node["sp_created_by"]) != "claude-code" {
			t.Errorf("%v: sp_created_by = %v", node["id"], node["sp_created_by"])
		}
		if got := num(t, node["sp_rev"]); got != 1 {
			t.Errorf("%v: sp_rev = %v", node["id"], got)
		}
	}
}

func TestImport_ImportNeverDeletes(t *testing.T) {
	s := importSpace(t)
	existing := oneCard(t, s, "demo", "title", `"Mine"`)
	doImport(t, s, fixture(t, "canvas.json"), nil)
	live := map[string]bool{}
	for _, n := range asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["nodes"]) {
		live[asStr(asMap(n)["id"])] = true
	}
	if !live[asStr(existing["id"])] {
		t.Error("the pre-existing card was deleted")
	}
	if len(live) != 7 {
		t.Errorf("%d live nodes, want 7", len(live))
	}
}

func TestImport_ImportingTwiceDuplicatesRatherThanMerging(t *testing.T) {
	s := importSpace(t)
	doImport(t, s, fixture(t, "canvas.json"), nil)
	doImport(t, s, fixture(t, "canvas.json"), nil)
	if got := len(asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["nodes"])); got != 12 {
		t.Errorf("%d live nodes, want 12", got)
	}
}

func TestImport_ImportEmitsOneEventPerItem(t *testing.T) {
	s := importSpace(t)
	doImport(t, s, fixture(t, "canvas.json"), nil)
	created, linked := 0, 0
	types := eventsOf(t, s, "demo", "0")
	for _, e := range types {
		switch asStr(asMap(e)["type"]) {
		case "card.created":
			created++
		case "link.created":
			linked++
		}
	}
	if created != 6 || linked != 4 || len(types) != 11 {
		t.Errorf("events: %d created, %d linked, %d total (want 6/4/11)", created, linked, len(types))
	}
}

func TestImport_AnEdgeToAnUnknownNodeIsRejectedAtomically(t *testing.T) {
	s := importSpace(t)
	canvas := asMap(fixture(t, "canvas.json"))
	bad := map[string]any{
		"nodes": []any{asArr(canvas["nodes"])[0]},
		"edges": []any{map[string]any{
			"id":       "l_x",
			"fromNode": asStr(asMap(asArr(canvas["nodes"])[0])["id"]),
			"toNode":   "c_absent"}}}
	r := s.post(t, "/api/spaces/demo/import", agentP(), bad)
	if r.status != 400 && r.status != 404 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if got := asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["nodes"]); len(got) != 0 {
		t.Errorf("a rejected import created %d nodes", len(got))
	}
}

func TestImport_AnEdgeMayReferenceACardAlreadyInTheSpace(t *testing.T) {
	s := importSpace(t)
	nodes := addCards(t, s, "demo", []any{
		map[string]any{"title": "A", "content": "a"},
		map[string]any{"title": "B", "content": "b"}}, nil)
	result := doImport(t, s, map[string]any{"nodes": []any{}, "edges": []any{
		map[string]any{"id": "l_x", "fromNode": asMap(nodes[0])["id"],
			"toNode": asMap(nodes[1])["id"], "label": "joins"}}}, nil)
	edges := asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["edges"])
	if len(edges) != 1 {
		t.Fatalf("%d edges, want 1", len(edges))
	}
	if asStr(asMap(edges[0])["id"]) != asStr(asMap(result["id_map"])["l_x"]) {
		t.Errorf("edge id = %v, want the remapped l_x", asMap(edges[0])["id"])
	}
}

func TestImport_EmptyImportIsANoOp(t *testing.T) {
	s := importSpace(t)
	before := eventsOf(t, s, "demo", "0")
	result := doImport(t, s, map[string]any{"nodes": []any{}, "edges": []any{}}, nil)
	if len(asMap(result["id_map"])) != 0 {
		t.Errorf("id_map = %v", result["id_map"])
	}
	if got := eventsOf(t, s, "demo", "0"); len(got) != len(before) {
		t.Errorf("empty import emitted %d events", len(got)-len(before))
	}
}

func TestImport_ExportImportRoundTripsThroughASecondSpace(t *testing.T) {
	// `analog export | analog import` is the .canvas round trip in SPEC §4.2.
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	makeSpace(t, s, "copy", "Copy", "")
	addCards(t, s, "demo", []any{
		map[string]any{"title": "A", "content": "a"},
		map[string]any{"title": "B", "content": "b"}}, nil)
	exported := s.get(t, "/api/spaces/demo/canvas", nil).body

	if r := s.post(t, "/api/spaces/copy/import", agentP(), exported); r.status != 201 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	copied := s.get(t, "/api/spaces/copy/canvas", nil).obj()
	var titles, texts []string
	for _, n := range asArr(copied["nodes"]) {
		titles = append(titles, asStr(asMap(n)["sp_title"]))
		texts = append(texts, asStr(asMap(n)["text"]))
	}
	if !equalStrings(titles, "A", "B") || !equalStrings(texts, "a", "b") {
		t.Errorf("copied canvas: titles %v texts %v", titles, texts)
	}
}
