// SPEC §3 card endpoints, §2.1 node shape, §5 auto-layout.
package conformance

import (
	"fmt"
	"testing"
)

// SPEC §5, and DECISIONS.md on why a batch wraps at all. Stated here rather than
// imported: this suite describes the behaviour, so reading the constant out of the
// implementation would let the two drift together and still pass.
const layoutMaxColumn = 900.0

// cardsSpace starts every test's fresh server with a "demo" space, mirroring the
// python fixture.
func cardsSpace(t *testing.T) *server {
	t.Helper()
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	return s
}

// --- creation ----------------------------------------------------------------

func TestCards_DraftsBecomeJsonCanvasTextNodes(t *testing.T) {
	s := cardsSpace(t)
	node := asMap(addCards(t, s, "demo", []any{
		map[string]any{"title": "Option E", "content": "## E", "kind": "md"}}, nil)[0])
	assertValid(t, node, "Node", false)
	if !hasPrefix(asStr(node["id"]), "c_") {
		t.Errorf("id = %v", node["id"])
	}
	if asStr(node["type"]) != "text" || asStr(node["text"]) != "## E" ||
		asStr(node["sp_kind"]) != "md" || asStr(node["sp_title"]) != "Option E" {
		t.Errorf("node = %s", canonical(node))
	}
	if asStr(node["sp_created_by"]) != "claude-code" {
		t.Errorf("sp_created_by = %v", node["sp_created_by"])
	}
	if got := num(t, node["sp_rev"]); got != 1 {
		t.Errorf("sp_rev = %v", got)
	}
	if _, has := node["sp_deleted_at"]; has {
		t.Error("sp_deleted_at present on a live node")
	}
}

func TestCards_KindDefaultsToMd(t *testing.T) {
	s := cardsSpace(t)
	node := asMap(addCards(t, s, "demo", []any{
		map[string]any{"title": "T", "content": "c"}}, nil)[0])
	if got := asStr(node["sp_kind"]); got != "md" {
		t.Errorf("sp_kind = %q", got)
	}
}

func TestCards_AllKindsAccepted(t *testing.T) {
	for _, kind := range []string{"md", "html", "svg", "plain"} {
		t.Run(kind, func(t *testing.T) {
			s := cardsSpace(t)
			node := asMap(addCards(t, s, "demo", []any{
				map[string]any{"title": kind, "content": "x", "kind": kind}}, nil)[0])
			if got := asStr(node["sp_kind"]); got != kind {
				t.Errorf("sp_kind = %q, want %q", got, kind)
			}
		})
	}
}

func TestCards_UnknownKindIsRejected(t *testing.T) {
	s := cardsSpace(t)
	r := s.post(t, "/api/spaces/demo/cards", agentP(),
		map[string]any{"cards": []any{map[string]any{"title": "T", "content": "c", "kind": "pdf"}}})
	if r.status != 400 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	err := asStr(r.obj()["error"])
	if err != "unsupported_kind" && err != "validation_failed" {
		t.Errorf("error = %q", err)
	}
}

func TestCards_BulkCreateReturnsNodesInRequestOrder(t *testing.T) {
	s := cardsSpace(t)
	cards := make([]any, 0, 4)
	for _, title := range []string{"A", "B", "C", "D"} {
		cards = append(cards, map[string]any{"title": title, "content": title})
	}
	nodes := addCards(t, s, "demo", cards, nil)
	for i, want := range []string{"A", "B", "C", "D"} {
		if got := asStr(asMap(nodes[i])["sp_title"]); got != want {
			t.Errorf("node %d title = %q, want %q", i, got, want)
		}
	}
	unique := map[string]bool{}
	for _, n := range nodes {
		unique[asStr(asMap(n)["id"])] = true
	}
	if len(unique) != 4 {
		t.Errorf("%d distinct ids for 4 cards", len(unique))
	}
}

func TestCards_MetaIsStoredVerbatim(t *testing.T) {
	s := cardsSpace(t)
	node := asMap(addCards(t, s, "demo", []any{map[string]any{
		"title": "T", "content": "c",
		"meta": jlit(t, `{"source": "run-17", "nested": {"n": [1, 2]}}`)}}, nil)[0])
	assertJSONEq(t, "sp_meta", jlit(t, `{"source": "run-17", "nested": {"n": [1, 2]}}`),
		node["sp_meta"])
}

func TestCards_RawNodesAreAcceptedAndReattributed(t *testing.T) {
	s := cardsSpace(t)
	r := s.post(t, "/api/spaces/demo/cards", agentP(), map[string]any{"nodes": []any{
		jlit(t, `{"id": "c_client_chosen", "type": "text", "x": 5, "y": 6, "width": 100,
		  "height": 50, "text": "raw", "sp_kind": "plain", "sp_title": "Raw",
		  "sp_created_by": "someone-else", "sp_rev": 99}`)}})
	if r.status != 201 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	node := r.arr()[0].(map[string]any)
	if asStr(node["id"]) == "c_client_chosen" {
		t.Error("clients must not choose ids")
	}
	for key, want := range map[string]float64{"x": 5, "y": 6, "width": 100, "height": 50} {
		if got := num(t, node[key]); got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
	if asStr(node["sp_created_by"]) != "claude-code" {
		t.Error("attribution comes from actor")
	}
	if got := num(t, node["sp_rev"]); got != 1 {
		t.Errorf("sp_rev = %v", got)
	}
}

func TestCards_CreatingInAnUnknownSpaceIs404(t *testing.T) {
	s := startServer(t)
	r := s.post(t, "/api/spaces/nope/cards", agentP(),
		map[string]any{"cards": []any{map[string]any{"title": "T", "content": "c"}}})
	if r.status != 404 {
		t.Fatalf("%d", r.status)
	}
}

// --- auto-layout (SPEC §5) ---------------------------------------------------

func TestCards_FirstCardLandsAtTheOrigin(t *testing.T) {
	s := cardsSpace(t)
	node := oneCard(t, s, "demo")
	if num(t, node["x"]) != 0 || num(t, node["y"]) != 0 {
		t.Errorf("position = (%v, %v)", node["x"], node["y"])
	}
	if num(t, node["width"]) <= 0 || num(t, node["height"]) <= 0 {
		t.Errorf("size = (%v, %v)", node["width"], node["height"])
	}
}

func TestCards_OmittedGeometryGoesRightOfTheBoundingBoxTopDown(t *testing.T) {
	s := cardsSpace(t)
	first := asMap(addCards(t, s, "demo", []any{map[string]any{
		"title": "pinned", "content": "c", "x": 0, "y": 0, "width": 320, "height": 200}}, nil)[0])
	nodes := addCards(t, s, "demo", []any{
		map[string]any{"title": "a", "content": "a"},
		map[string]any{"title": "b", "content": "b"}}, nil)
	a, b := asMap(nodes[0]), asMap(nodes[1])
	rightEdge := num(t, first["x"]) + num(t, first["width"])
	if num(t, a["x"]) < rightEdge || num(t, b["x"]) < rightEdge {
		t.Errorf("x values %v, %v < right edge %v", a["x"], b["x"], rightEdge)
	}
	if num(t, a["x"]) != num(t, b["x"]) {
		t.Error("a batch stacks in one column")
	}
	if num(t, b["y"]) < num(t, a["y"])+num(t, a["height"]) {
		t.Error("not top-down, or overlapping")
	}
}

func TestCards_ExplicitGeometryWins(t *testing.T) {
	s := cardsSpace(t)
	node := asMap(addCards(t, s, "demo", []any{map[string]any{
		"title": "T", "content": "c", "x": -40, "y": 12, "width": 999, "height": 111}}, nil)[0])
	for key, want := range map[string]float64{"x": -40, "y": 12, "width": 999, "height": 111} {
		if got := num(t, node[key]); got != want {
			t.Errorf("%s = %v, want %v", key, got, want)
		}
	}
}

func TestCards_LayoutIgnoresDeletedCards(t *testing.T) {
	s := cardsSpace(t)
	doomed := asMap(addCards(t, s, "demo", []any{map[string]any{
		"title": "far", "content": "c", "x": 5000, "y": 0, "width": 320, "height": 200}}, nil)[0])
	s.delete(t, "/api/spaces/demo/cards/"+asStr(doomed["id"]), humanP())
	if got := num(t, oneCard(t, s, "demo")["x"]); got >= 5000 {
		t.Errorf("new card x = %v, the deleted card still anchors the layout", got)
	}
}

// --- patch: moved vs updated (schema.sql implementer notes 1 and 2) ----------

func lastEventType(t *testing.T, s *server, slug string) string {
	t.Helper()
	events := eventsOf(t, s, slug, "0")
	return asStr(asMap(events[len(events)-1])["type"])
}

func TestCards_GeometryOnlyPatchMovesWithoutBumpingRev(t *testing.T) {
	s := cardsSpace(t)
	card := oneCard(t, s, "demo")
	r := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"x": 100, "y": 200})
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if got := num(t, r.obj()["sp_rev"]); got != 1 {
		t.Errorf("sp_rev = %v", got)
	}
	if num(t, r.obj()["x"]) != 100 || num(t, r.obj()["y"]) != 200 {
		t.Errorf("position = (%v, %v)", r.obj()["x"], r.obj()["y"])
	}
	if got := lastEventType(t, s, "demo"); got != "card.moved" {
		t.Errorf("last event = %q", got)
	}
}

func TestCards_ResizeIsAMoveNotAnEdit(t *testing.T) {
	// Normalized selectors survive a resize, so a resize must not stale annotations.
	s := cardsSpace(t)
	card := oneCard(t, s, "demo")
	r := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"width": 640, "height": 400})
	if got := num(t, r.obj()["sp_rev"]); got != 1 {
		t.Errorf("sp_rev = %v", got)
	}
	if got := lastEventType(t, s, "demo"); got != "card.moved" {
		t.Errorf("last event = %q", got)
	}
}

func TestCards_ANoOpMoveStillEmitsOneMoveEvent(t *testing.T) {
	// Fixture event 15 moves c_opt_a from [0,0] to [0,0]: classification is by the
	// keys in the patch, not by whether the values differ.
	s := cardsSpace(t)
	card := oneCard(t, s, "demo")
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"x": card["x"], "y": card["y"]})
	ev := asMap(eventsOf(t, s, "demo", "0")[len(eventsOf(t, s, "demo", "0"))-1])
	if asStr(ev["type"]) != "card.moved" {
		t.Fatalf("last event = %q", ev["type"])
	}
	want := fmt.Sprintf("[%s, %s]", card["x"], card["y"])
	assertJSONEq(t, "payload.from", jlit(t, want), asMap(ev["payload"])["from"])
	assertJSONEq(t, "payload.to", jlit(t, want), asMap(ev["payload"])["to"])
}

func TestCards_ContentPatchUpdatesAndBumpsRev(t *testing.T) {
	s := cardsSpace(t)
	card := oneCard(t, s, "demo")
	r := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"text": "rewritten"})
	if got := num(t, r.obj()["sp_rev"]); got != 2 {
		t.Errorf("sp_rev = %v", got)
	}
	if asStr(r.obj()["text"]) != "rewritten" {
		t.Errorf("text = %v", r.obj()["text"])
	}
	ev := asMap(eventsOf(t, s, "demo", "0")[len(eventsOf(t, s, "demo", "0"))-1])
	if asStr(ev["type"]) != "card.updated" {
		t.Fatalf("last event = %q", ev["type"])
	}
	assertJSONEq(t, "changed", []any{"text"}, asMap(ev["payload"])["changed"])
	if got := num(t, asMap(ev["payload"])["rev"]); got != 2 {
		t.Errorf("payload rev = %v", got)
	}
}

func TestCards_NonGeometryPatchesAreEdits(t *testing.T) {
	for _, patch := range []string{
		`{"text": "x"}`, `{"sp_title": "New"}`, `{"sp_kind": "plain"}`,
		`{"sp_meta": {"a": 1}}`, `{"color": "4"}`,
	} {
		t.Run(patch, func(t *testing.T) {
			s := cardsSpace(t)
			card := oneCard(t, s, "demo")
			r := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
				jlit(t, patch))
			if got := num(t, r.obj()["sp_rev"]); got != 2 {
				t.Errorf("sp_rev = %v for %s", got, patch)
			}
			if got := lastEventType(t, s, "demo"); got != "card.updated" {
				t.Errorf("last event = %q for %s", got, patch)
			}
		})
	}
}

func TestCards_MixedPatchIsAnEdit(t *testing.T) {
	s := cardsSpace(t)
	card := oneCard(t, s, "demo")
	r := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"x": 50, "text": "new"})
	if got := num(t, r.obj()["sp_rev"]); got != 2 {
		t.Errorf("sp_rev = %v", got)
	}
	ev := asMap(eventsOf(t, s, "demo", "0")[len(eventsOf(t, s, "demo", "0"))-1])
	if asStr(ev["type"]) != "card.updated" {
		t.Fatalf("last event = %q", ev["type"])
	}
	assertJSONEq(t, "changed", []any{"text", "x"}, asMap(ev["payload"])["changed"])
}

func TestCards_PatchPreservesUnmentionedKeys(t *testing.T) {
	s := cardsSpace(t)
	card := asMap(addCards(t, s, "demo", []any{map[string]any{
		"title": "Keep", "content": "c", "meta": jlit(t, `{"k": 1}`)}}, nil)[0])
	patched := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"text": "new"}).obj()
	if asStr(patched["sp_title"]) != "Keep" {
		t.Errorf("sp_title = %v", patched["sp_title"])
	}
	assertJSONEq(t, "sp_meta", jlit(t, `{"k": 1}`), patched["sp_meta"])
	if asStr(patched["sp_created_by"]) != "claude-code" {
		t.Error("the original author is not overwritten")
	}
}

func TestCards_PatchCannotChangeTheId(t *testing.T) {
	s := cardsSpace(t)
	card := oneCard(t, s, "demo")
	patched := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"id": "c_hijack", "text": "x"}).obj()
	if asStr(patched["id"]) != asStr(card["id"]) {
		t.Errorf("id changed to %v", patched["id"])
	}
}

func TestCards_EmptyPatchIsRejected(t *testing.T) {
	s := cardsSpace(t)
	card := oneCard(t, s, "demo")
	r := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(), map[string]any{})
	if r.status != 400 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if asStr(r.obj()["error"]) != "validation_failed" {
		t.Errorf("error = %v", r.obj()["error"])
	}
}

func TestCards_PatchingAnUnknownCardIs404(t *testing.T) {
	s := cardsSpace(t)
	if r := s.patch(t, "/api/spaces/demo/cards/c_nope", humanP(),
		map[string]any{"text": "x"}); r.status != 404 {
		t.Fatalf("%d", r.status)
	}
}

// --- If-Match (SPEC §3) ------------------------------------------------------

func TestCards_IfMatchOnTheCurrentRevSucceeds(t *testing.T) {
	s := cardsSpace(t)
	card := oneCard(t, s, "demo")
	r := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"text": "x"}, map[string]string{"If-Match": "1"})
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if got := num(t, r.obj()["sp_rev"]); got != 2 {
		t.Errorf("sp_rev = %v", got)
	}
}

func TestCards_IfMatchMismatchIs409WithTheCurrentNode(t *testing.T) {
	s := cardsSpace(t)
	card := oneCard(t, s, "demo")
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"text": "first"})
	r := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"text": "second"}, map[string]string{"If-Match": "1"})
	if r.status != 409 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	body := r.obj()
	if asStr(body["error"]) != "conflict" {
		t.Errorf("error = %v", body["error"])
	}
	current := asMap(body["current"])
	if num(t, current["sp_rev"]) != 2 {
		t.Errorf("current sp_rev = %v", current["sp_rev"])
	}
	if asStr(current["text"]) != "first" {
		t.Error("the losing write must not have applied")
	}
	assertValid(t, current, "Node", false)
}

func TestCards_AbsentIfMatchIsLastWriteWins(t *testing.T) {
	s := cardsSpace(t)
	card := oneCard(t, s, "demo")
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"text": "first"})
	r := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), agentP(),
		map[string]any{"text": "second"})
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if asStr(r.obj()["text"]) != "second" {
		t.Errorf("text = %v", r.obj()["text"])
	}
}

// --- delete (SPEC §2.2 soft delete) ------------------------------------------

func TestCards_DeleteIsSoft(t *testing.T) {
	s := cardsSpace(t)
	card := oneCard(t, s, "demo", "title", `"Option D"`)
	if r := s.delete(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP()); r.status != 204 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	live := asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["nodes"])
	for _, n := range live {
		if asStr(asMap(n)["id"]) == asStr(card["id"]) {
			t.Error("deleted card is still live")
		}
	}
	kept := asArr(s.get(t, "/api/spaces/demo/canvas", params("include_deleted", "true")).obj()["nodes"])
	var tombstone map[string]any
	for _, n := range kept {
		if asStr(asMap(n)["id"]) == asStr(card["id"]) {
			tombstone = asMap(n)
		}
	}
	if tombstone == nil {
		t.Fatal("tombstone missing from include_deleted")
	}
	if asStr(tombstone["sp_deleted_at"]) == "" {
		t.Error("sp_deleted_at is empty")
	}
	ev := asMap(eventsOf(t, s, "demo", "0")[len(eventsOf(t, s, "demo", "0"))-1])
	if asStr(ev["type"]) != "card.deleted" {
		t.Fatalf("last event = %q", ev["type"])
	}
	if asStr(asMap(ev["payload"])["title"]) != "Option D" {
		t.Error("the agent needs the title, the card is gone")
	}
}

func TestCards_DeletingTwiceIs404(t *testing.T) {
	s := cardsSpace(t)
	card := oneCard(t, s, "demo")
	s.delete(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP())
	if r := s.delete(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP()); r.status != 404 {
		t.Fatalf("%d", r.status)
	}
}

func TestCards_PatchingADeletedCardIs404(t *testing.T) {
	s := cardsSpace(t)
	card := oneCard(t, s, "demo")
	s.delete(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP())
	if r := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"text": "x"}); r.status != 404 {
		t.Fatalf("%d", r.status)
	}
}

func TestCards_DeletingAnUnknownCardIs404(t *testing.T) {
	s := cardsSpace(t)
	if r := s.delete(t, "/api/spaces/demo/cards/c_nope", humanP()); r.status != 404 {
		t.Fatalf("%d", r.status)
	}
}

func TestCards_ALongBatchWrapsIntoANewColumn(t *testing.T) {
	// SPEC §5 says a column; five cards of one is a strip you have to zoom out to
	// read, so a batch wraps once the column passes LAYOUT_MAX_COLUMN.
	s := cardsSpace(t)
	cards := make([]any, 0, 6)
	for i := 0; i < 6; i++ {
		cards = append(cards, map[string]any{
			"title": fmt.Sprint(i), "content": "c", "height": 200, "width": 320})
	}
	nodes := addCards(t, s, "demo", cards, nil)
	columns := map[float64][]map[string]any{}
	var order []float64
	for _, raw := range nodes {
		node := asMap(raw)
		x := num(t, node["x"])
		if _, seen := columns[x]; !seen {
			order = append(order, x)
		}
		columns[x] = append(columns[x], node)
	}
	if len(columns) <= 1 {
		t.Fatal("six cards must not be one column")
	}
	for _, x := range order {
		column := columns[x]
		top, bottom := column[0]["y"], column[0]["y"]
		for _, n := range column {
			if num(t, n["y"]) < num(t, top) {
				top = n["y"]
			}
			if num(t, n["y"])+num(t, n["height"]) > num(t, bottom)+num(t, n["height"]) {
				bottom = n["y"]
			}
		}
		bottomEdge := 0.0
		for _, n := range column {
			if e := num(t, n["y"]) + num(t, n["height"]); e > bottomEdge {
				bottomEdge = e
			}
		}
		if bottomEdge-num(t, top) > layoutMaxColumn+200 {
			t.Errorf("column at %v is too tall", x)
		}
		ys := make([]float64, 0, len(column))
		for _, n := range column {
			ys = append(ys, num(t, n["y"]))
		}
		sorted := append([]float64(nil), ys...)
		for i := 1; i < len(sorted); i++ {
			if sorted[i] < sorted[i-1] {
				t.Errorf("column at %v is not top-down", x)
				break
			}
		}
	}
	distinctY := map[float64]bool{}
	for _, raw := range nodes {
		distinctY[num(t, asMap(raw)["y"])] = true
	}
	if len(distinctY) >= len(nodes) {
		t.Error("columns do not reuse the same rows")
	}
}

func TestCards_WrappingColumnsDoNotOverlap(t *testing.T) {
	s := cardsSpace(t)
	cards := make([]any, 0, 8)
	for i := 0; i < 8; i++ {
		cards = append(cards, map[string]any{
			"title": fmt.Sprint(i), "content": "c", "height": 200, "width": 320})
	}
	nodes := addCards(t, s, "demo", cards, nil)
	type box struct{ x0, y0, x1, y1 float64 }
	boxes := make([]box, 0, len(nodes))
	for _, raw := range nodes {
		n := asMap(raw)
		boxes = append(boxes, box{num(t, n["x"]), num(t, n["y"]),
			num(t, n["x"]) + num(t, n["width"]), num(t, n["y"]) + num(t, n["height"])})
	}
	for i, a := range boxes {
		for _, b := range boxes[i+1:] {
			if a.x0 < b.x1 && b.x0 < a.x1 && a.y0 < b.y1 && b.y0 < a.y1 {
				t.Errorf("%v overlaps %v", a, b)
			}
		}
	}
}
