// SPEC §2.4 — replace and branch.
//
// Note the event count in branch mode. A branching PATCH is not one mutation: it
// creates a card and an auto-link, so it emits card.created + link.created. What it
// must NOT do is write the superseded card's content again — schema.sql note 4 says
// its rev freezes, which is the whole reason branch mode never produces stale pins.
package conformance

import (
	"testing"
)

// nodeIn finds one node of a canvas by id.
func nodeIn(t *testing.T, s *server, slug, nodeID string, paramsKv ...string) map[string]any {
	t.Helper()
	q := params(paramsKv...)
	canvas := s.get(t, "/api/spaces/"+slug+"/canvas", q).obj()
	for _, n := range asArr(canvas["nodes"]) {
		if asStr(asMap(n)["id"]) == nodeID {
			return asMap(n)
		}
	}
	t.Fatalf("node %s not on the canvas of %s", nodeID, slug)
	return nil
}

// --- replace (default) -------------------------------------------------------

func TestRevision_ReplaceMutatesInPlace(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	card := oneCard(t, s, "demo", "content", `"v1"`)

	updated := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), agentP(),
		map[string]any{"text": "v2"}).obj()
	if asStr(updated["id"]) != asStr(card["id"]) {
		t.Errorf("id changed to %v", updated["id"])
	}
	if asStr(updated["text"]) != "v2" {
		t.Errorf("text = %v", updated["text"])
	}
	if got := num(t, updated["sp_rev"]); got != 2 {
		t.Errorf("sp_rev = %v", got)
	}
	if _, has := updated["sp_superseded_by"]; has {
		t.Error("sp_superseded_by present in replace mode")
	}
	if got := len(asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["nodes"])); got != 1 {
		t.Errorf("%d nodes, want 1", got)
	}
	if got := asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["edges"]); len(got) != 0 {
		t.Errorf("edges = %v", canonical(got))
	}
}

func TestRevision_ReplaceKeepsTheOldContentOnlyInTheEventLog(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	card := oneCard(t, s, "demo", "content", `"v1"`)
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), agentP(), map[string]any{"text": "v2"})
	log := eventsOf(t, s, "demo", "0")
	ev := asMap(log[len(log)-1])
	if asStr(ev["type"]) != "card.updated" {
		t.Fatalf("last event = %q", ev["type"])
	}
	assertJSONEq(t, "changed", []any{"text"}, asMap(ev["payload"])["changed"])
}

// --- branch ------------------------------------------------------------------

// branchedSpace starts a branch-mode space with one svg card, revised once.
func branchedSpace(t *testing.T) (*server, map[string]any, map[string]any) {
	t.Helper()
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "branch")
	old := oneCard(t, s, "demo", "title", `"Chart"`, "content", `"v1"`, "kind", `"svg"`)
	new := s.patch(t, "/api/spaces/demo/cards/"+asStr(old["id"]), agentP(),
		map[string]any{"text": "v2"})
	if new.status != 200 {
		t.Fatalf("%d %s", new.status, new.str())
	}
	return s, old, new.obj()
}

func TestRevision_BranchReturnsTheNewCard(t *testing.T) {
	_, old, new := branchedSpace(t)
	if asStr(new["id"]) == asStr(old["id"]) {
		t.Fatal("branch mode returned the same card")
	}
	if asStr(new["text"]) != "v2" {
		t.Errorf("text = %v", new["text"])
	}
	if got := num(t, new["sp_rev"]); got != 1 {
		t.Errorf("sp_rev = %v, want 1: a fresh card starts at rev 1", got)
	}
	if asStr(new["sp_title"]) != "Chart" {
		t.Errorf("sp_title = %v, want inherited from the card it revises", new["sp_title"])
	}
	if asStr(new["sp_kind"]) != "svg" {
		t.Errorf("sp_kind = %v", new["sp_kind"])
	}
	if _, has := new["sp_superseded_by"]; has {
		t.Error("sp_superseded_by present on the new card")
	}
}

func TestRevision_BranchMarksTheOldCardAndFreezesIt(t *testing.T) {
	s, old, new := branchedSpace(t)
	stub := nodeIn(t, s, "demo", asStr(old["id"]))
	if asStr(stub["sp_superseded_by"]) != asStr(new["id"]) {
		t.Errorf("sp_superseded_by = %v", stub["sp_superseded_by"])
	}
	if asStr(stub["text"]) != "v1" {
		t.Error("the old content stays readable")
	}
	if num(t, stub["sp_rev"]) != num(t, old["sp_rev"]) {
		t.Error("the supersede pointer must not bump rev")
	}
}

func TestRevision_BranchKeepsBothCardsVisible(t *testing.T) {
	s, old, new := branchedSpace(t)
	ids := map[string]bool{}
	for _, n := range asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["nodes"]) {
		ids[asStr(asMap(n)["id"])] = true
	}
	if len(ids) != 2 || !ids[asStr(old["id"])] || !ids[asStr(new["id"])] {
		t.Errorf("visible cards = %v", ids)
	}
}

func TestRevision_BranchDoesNotStackTheNewCardOnTheOldOne(t *testing.T) {
	_, old, new := branchedSpace(t)
	if num(t, new["x"]) == num(t, old["x"]) && num(t, new["y"]) == num(t, old["y"]) {
		t.Errorf("new card stacked at (%v, %v)", new["x"], new["y"])
	}
}

func TestRevision_BranchAutoLinksOldToNewWithLabelRevised(t *testing.T) {
	s, old, new := branchedSpace(t)
	edges := asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["edges"])
	if len(edges) != 1 {
		t.Fatalf("%d edges, want 1", len(edges))
	}
	edge := asMap(edges[0])
	if asStr(edge["fromNode"]) != asStr(old["id"]) || asStr(edge["toNode"]) != asStr(new["id"]) {
		t.Errorf("edge = %s", canonical(edge))
	}
	if asStr(edge["label"]) != "revised" {
		t.Errorf("label = %v", edge["label"])
	}
}

func TestRevision_BranchEmitsACreateAndALinkAndNothingElse(t *testing.T) {
	s, _, _ := branchedSpace(t)
	var types []string
	for _, e := range eventsOf(t, s, "demo", "0") {
		types = append(types, asStr(asMap(e)["type"]))
	}
	want := []string{"space.created", "card.created", "card.created", "link.created"}
	if !equalStrings(types, want...) {
		t.Fatalf("event types = %v, want %v", types, want)
	}
	for _, ty := range types {
		if ty == "card.updated" {
			t.Error("the superseded card is never written again")
		}
	}
}

func TestRevision_BranchReportsAsANewCardNotAnEdit(t *testing.T) {
	// A different agent sees a creation, so it will not think its card was rewritten.
	s, _, _ := branchedSpace(t)
	fb := s.get(t, "/api/spaces/demo/feedback", params("actor", "codex")).obj()
	if got := asArr(fb["cards_edited"]); len(got) != 0 {
		t.Errorf("cards_edited = %v", canonical(got))
	}
	var labels []string
	for _, l := range asArr(fb["links_added"]) {
		labels = append(labels, asStr(asMap(l)["label"]))
	}
	if !equalStrings(labels, "revised") {
		t.Errorf("links_added labels = %v", labels)
	}
}

// --- annotations across a branch (SPEC §2.4) --------------------------------

func TestRevision_AnnotationsStayOnTheCardTheyWereMadeOn(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "branch")
	old := oneCard(t, s, "demo", "title", `"Chart"`, "content", `"v1"`)
	ann := asMap(s.post(t, "/api/spaces/demo/annotations", humanP(), map[string]any{
		"card_id": old["id"], "body": "y-axis starts at 40",
		"selector": jlit(t, `{"type": "point", "x": 0.3, "y": 0.6}`)}).body)
	new := s.patch(t, "/api/spaces/demo/cards/"+asStr(old["id"]), agentP(),
		map[string]any{"text": "v2"}).obj()

	kept := asArr(s.get(t, "/api/spaces/demo/annotations", nil).arr())
	if len(kept) != 1 || asStr(asMap(kept[0])["card_id"]) != asStr(old["id"]) {
		t.Fatalf("annotations = %v, want one, not copied forward", canonical(kept))
	}
	if asStr(asMap(kept[0])["id"]) != asStr(ann["id"]) {
		t.Errorf("annotation id = %v", asMap(kept[0])["id"])
	}
	_ = new
}

func TestRevision_BranchModeNeverProducesAStaleAnnotation(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "branch")
	old := oneCard(t, s, "demo", "content", `"v1"`)
	s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": old["id"], "body": "b"})
	for i := 0; i < 3; i++ {
		old = s.patch(t, "/api/spaces/demo/cards/"+asStr(old["id"]), agentP(),
			map[string]any{"text": "v" + string(rune('2'+i))}).obj()
	}
	for _, a := range asArr(s.get(t, "/api/spaces/demo/annotations", nil).arr()) {
		if asBool(asMap(a)["stale"]) {
			t.Errorf("%v is stale in branch mode", asMap(a)["id"])
		}
	}
}

func TestRevision_FeedbackStillDeliversAnnotationsOnSupersededCards(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "branch")
	old := oneCard(t, s, "demo", "title", `"Chart"`, "content", `"v1"`)
	s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": old["id"], "body": "fix the axis"})
	s.patch(t, "/api/spaces/demo/cards/"+asStr(old["id"]), agentP(), map[string]any{"text": "v2"})

	fb := s.get(t, "/api/spaces/demo/feedback", params("actor", "claude-code")).obj()
	var bodies []string
	for _, a := range asArr(fb["annotations"]) {
		bodies = append(bodies, asStr(asMap(a)["body"]))
	}
	if !equalStrings(bodies, "fix the axis") {
		t.Fatalf("annotations = %v", bodies)
	}
	if asBool(asMap(asArr(fb["annotations"])[0])["stale"]) {
		t.Error("annotation is stale in branch mode")
	}
}

// --- per-call override -------------------------------------------------------

func TestRevision_ModeQueryOverridesAReplaceSpace(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	old := oneCard(t, s, "demo", "content", `"v1"`)
	new := s.patch(t, "/api/spaces/demo/cards/"+asStr(old["id"]),
		mergeParams(agentP(), "mode", "branch"), map[string]any{"text": "v2"}).obj()
	if asStr(new["id"]) == asStr(old["id"]) {
		t.Fatal("mode=branch did not branch")
	}
	if got := nodeIn(t, s, "demo", asStr(old["id"]))["sp_superseded_by"]; asStr(got) != asStr(new["id"]) {
		t.Errorf("sp_superseded_by = %v", got)
	}
}

func TestRevision_ModeQueryOverridesABranchSpace(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "branch")
	old := oneCard(t, s, "demo", "content", `"v1"`)
	same := s.patch(t, "/api/spaces/demo/cards/"+asStr(old["id"]),
		mergeParams(agentP(), "mode", "replace"), map[string]any{"text": "v2"}).obj()
	if asStr(same["id"]) != asStr(old["id"]) {
		t.Fatal("mode=replace branched anyway")
	}
	if got := num(t, same["sp_rev"]); got != 2 {
		t.Errorf("sp_rev = %v", got)
	}
	if got := len(asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["nodes"])); got != 1 {
		t.Errorf("%d nodes, want 1", got)
	}
}

func TestRevision_AnUnknownModeIsRejected(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	card := oneCard(t, s, "demo")
	r := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]),
		mergeParams(agentP(), "mode", "merge"), map[string]any{"text": "v2"})
	if r.status != 400 {
		t.Fatalf("%d %s", r.status, r.str())
	}
}

func TestRevision_AMoveNeverBranches(t *testing.T) {
	// Dragging a card is not a content revision, whatever the space's mode is.
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "branch")
	card := oneCard(t, s, "demo")
	moved := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"x": 400, "y": 50}).obj()
	if asStr(moved["id"]) != asStr(card["id"]) {
		t.Fatal("a move branched")
	}
	if got := len(asArr(s.get(t, "/api/spaces/demo/canvas", nil).obj()["nodes"])); got != 1 {
		t.Errorf("%d nodes, want 1", got)
	}
	if got := lastEventType(t, s, "demo"); got != "card.moved" {
		t.Errorf("last event = %q", got)
	}
}

func TestRevision_IfMatchStillAppliesInBranchMode(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "branch")
	card := oneCard(t, s, "demo")
	r := s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), agentP(),
		map[string]any{"text": "v2"}, map[string]string{"If-Match": "7"})
	if r.status != 409 {
		t.Fatalf("%d %s", r.status, r.str())
	}
}

func TestRevision_BranchingASupersededCardIsRejected(t *testing.T) {
	// The chain has one head; revising a frozen card would fork it.
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "branch")
	old := oneCard(t, s, "demo", "content", `"v1"`)
	s.patch(t, "/api/spaces/demo/cards/"+asStr(old["id"]), agentP(), map[string]any{"text": "v2"})
	r := s.patch(t, "/api/spaces/demo/cards/"+asStr(old["id"]), agentP(), map[string]any{"text": "v3"})
	if r.status != 409 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if asStr(r.obj()["error"]) != "conflict" {
		t.Errorf("error = %v", r.obj()["error"])
	}
}

func TestRevision_AnnotationsOnASupersededCardNameTheReplacement(t *testing.T) {
	// AMENDMENTS #6: SPEC §2.4 says feedback follows the supersede chain, so the
	// agent needs the head of the chain without a second GET /canvas.
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "branch")
	old := oneCard(t, s, "demo", "title", `"Chart"`, "content", `"v1"`)
	s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": old["id"], "body": "fix the axis"})

	listed := asMap(s.get(t, "/api/spaces/demo/annotations", nil).arr()[0])
	if _, has := listed["card_superseded_by"]; has {
		t.Fatal("card_superseded_by present while the card is current")
	}

	new := s.patch(t, "/api/spaces/demo/cards/"+asStr(old["id"]), agentP(),
		map[string]any{"text": "v2"}).obj()

	listed = asMap(s.get(t, "/api/spaces/demo/annotations", nil).arr()[0])
	if asStr(listed["card_superseded_by"]) != asStr(new["id"]) {
		t.Errorf("card_superseded_by = %v", listed["card_superseded_by"])
	}
	if asStr(listed["card_id"]) != asStr(old["id"]) {
		t.Error("the annotation itself does not move")
	}

	fb := s.get(t, "/api/spaces/demo/feedback", params("actor", "codex")).obj()
	if asStr(asMap(asArr(fb["annotations"])[0])["card_superseded_by"]) != asStr(new["id"]) {
		t.Error("feedback does not follow the supersede chain")
	}
}
