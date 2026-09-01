// SPEC §2.3 selectors and staleness, §3 annotation endpoints.
package conformance

import (
	"testing"
)

var (
	annotationPoint = jlitString(`{"type": "point", "x": 0.34, "y": 0.71}`)
	annotationRect  = jlitString(`{"type": "rect", "x": 0.1, "y": 0.2, "w": 0.3, "h": 0.25}`)
)

// jlitString defers parsing to a test context; these are used as literals.
func jlitString(s string) string { return s }

// annotationsSpace starts a fresh server with a demo space and one card named
// Option B.
func annotationsSpace(t *testing.T) (*server, map[string]any) {
	t.Helper()
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	return s, oneCard(t, s, "demo", "title", `"Option B"`)
}

// create posts one annotation as the human and requires 201.
func createAnnotation(t *testing.T, s *server, cardID string, body map[string]any) map[string]any {
	t.Helper()
	full := map[string]any{"card_id": cardID}
	for k, v := range body {
		full[k] = v
	}
	r := s.post(t, "/api/spaces/demo/annotations", humanP(), full)
	if r.status != 201 {
		t.Fatalf("create annotation: %d %s", r.status, r.str())
	}
	return r.obj()
}

func TestAnnotations_AllV1SelectorsRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selector string // json literal, or "" for none
	}{
		{"whole-card", ""},
		{"point", annotationPoint},
		{"rect", annotationRect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, card := annotationsSpace(t)
			body := map[string]any{"body": "look"}
			if tc.selector != "" {
				body["selector"] = jlit(t, tc.selector)
			}
			ann := createAnnotation(t, s, asStr(card["id"]), body)
			assertValid(t, ann, "Annotation", false)
			want := any(nil)
			if tc.selector != "" {
				want = jlit(t, tc.selector)
			}
			assertJSONEq(t, "selector", want, ann["selector"])
			if !hasPrefix(asStr(ann["id"]), "a_") {
				t.Errorf("id = %v", ann["id"])
			}
		})
	}
}

func TestAnnotations_SelectorDefaultsToTheWholeCard(t *testing.T) {
	s, card := annotationsSpace(t)
	if got := createAnnotation(t, s, asStr(card["id"]), map[string]any{"body": "b"})["selector"]; got != nil {
		t.Errorf("selector = %v, want none", got)
	}
}

func TestAnnotations_MotivationDefaultsToCommenting(t *testing.T) {
	s, card := annotationsSpace(t)
	if got := asStr(createAnnotation(t, s, asStr(card["id"]),
		map[string]any{"body": "b"})["motivation"]); got != "commenting" {
		t.Errorf("motivation = %q", got)
	}
}

func TestAnnotations_Motivations(t *testing.T) {
	for _, motivation := range []string{"commenting", "assessing", "editing"} {
		t.Run(motivation, func(t *testing.T) {
			s, card := annotationsSpace(t)
			ann := createAnnotation(t, s, asStr(card["id"]),
				map[string]any{"body": "b", "motivation": motivation})
			if asStr(ann["motivation"]) != motivation {
				t.Errorf("motivation = %q", ann["motivation"])
			}
		})
	}
}

func TestAnnotations_UnknownMotivationIsRejected(t *testing.T) {
	s, card := annotationsSpace(t)
	r := s.post(t, "/api/spaces/demo/annotations", humanP(), map[string]any{
		"card_id": card["id"], "body": "b", "motivation": "praising"})
	if r.status != 400 {
		t.Fatalf("%d %s", r.status, r.str())
	}
}

func TestAnnotations_CreationRecordsTheCreatorAndTheCardsRev(t *testing.T) {
	s, card := annotationsSpace(t)
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), agentP(), map[string]any{"text": "v2"})
	ann := createAnnotation(t, s, asStr(card["id"]), map[string]any{"body": "b"})
	if asStr(ann["creator"]) != "human" || asStr(ann["creator_kind"]) != "human" {
		t.Errorf("creator = %s", canonical(ann))
	}
	if got := num(t, ann["card_rev"]); got != 2 {
		t.Errorf("card_rev = %v", got)
	}
	if asBool(ann["stale"]) {
		t.Error("fresh annotation is stale")
	}
	if asStr(ann["card_title"]) != "Option B" {
		t.Errorf("card_title = %v", ann["card_title"])
	}
	if asBool(ann["resolved"]) || ann["resolved_reply"] != nil {
		t.Errorf("resolved = %v, reply = %v", ann["resolved"], ann["resolved_reply"])
	}
}

func TestAnnotations_AgentsCanAnnotateToo(t *testing.T) {
	s, card := annotationsSpace(t)
	ann := asMap(s.post(t, "/api/spaces/demo/annotations", agentP(), map[string]any{
		"card_id": card["id"], "body": "note to self"}).body)
	if asStr(ann["creator_kind"]) != "agent" || asStr(ann["creator"]) != "claude-code" {
		t.Errorf("creator = %s", canonical(ann))
	}
}

func TestAnnotations_AnnotatingAnUnknownCardIs404(t *testing.T) {
	s, _ := annotationsSpace(t)
	r := s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": "c_nope", "body": "b"})
	if r.status != 404 {
		t.Fatalf("%d %s", r.status, r.str())
	}
}

// --- staleness (SPEC §2.3) ---------------------------------------------------

func TestAnnotations_AnEditMakesTheAnnotationStale(t *testing.T) {
	s, card := annotationsSpace(t)
	ann := createAnnotation(t, s, asStr(card["id"]),
		map[string]any{"body": "b", "selector": jlit(t, annotationPoint)})
	if asBool(ann["stale"]) {
		t.Fatal("fresh annotation is stale")
	}
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), agentP(), map[string]any{"text": "rewritten"})
	if got := asBool(asMap(s.get(t, "/api/spaces/demo/annotations", nil).arr()[0])["stale"]); !got {
		t.Error("annotation did not go stale after an edit")
	}
}

func TestAnnotations_AMoveDoesNotMakeTheAnnotationStale(t *testing.T) {
	// schema.sql note 2: fractions survive a resize, so a move must not stale a pin.
	s, card := annotationsSpace(t)
	createAnnotation(t, s, asStr(card["id"]),
		map[string]any{"body": "b", "selector": jlit(t, annotationRect)})
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"x": 900, "y": 900, "width": 640, "height": 480})
	if got := asBool(asMap(s.get(t, "/api/spaces/demo/annotations", nil).arr()[0])["stale"]); got {
		t.Error("a move staled the pin")
	}
}

func TestAnnotations_StaleAnnotationsAreNotAutoResolved(t *testing.T) {
	// SPEC §10: only the human can tell 'fixed it' from 'rewrote around it'.
	s, card := annotationsSpace(t)
	createAnnotation(t, s, asStr(card["id"]), map[string]any{"body": "b"})
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), agentP(), map[string]any{"text": "v2"})
	ann := asMap(s.get(t, "/api/spaces/demo/annotations", nil).arr()[0])
	if !asBool(ann["stale"]) || asBool(ann["resolved"]) {
		t.Errorf("annotation = %s", canonical(ann))
	}
	if got := num(t, s.get(t, "/api/spaces/demo", nil).obj()["counts"].(map[string]any)["open_annotations"]); got != 1 {
		t.Errorf("open_annotations = %v", got)
	}
}

// --- resolve -----------------------------------------------------------------

func TestAnnotations_ResolveWithAReply(t *testing.T) {
	s, card := annotationsSpace(t)
	ann := createAnnotation(t, s, asStr(card["id"]),
		map[string]any{"body": "y-axis starts at 40"})
	r := s.patch(t, "/api/spaces/demo/annotations/"+asStr(ann["id"]), agentP(),
		map[string]any{"resolved": true, "reply": "rebased axis at 0"})
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if !asBool(r.obj()["resolved"]) || asStr(r.obj()["resolved_reply"]) != "rebased axis at 0" {
		t.Errorf("resolve result = %s", canonical(r.body))
	}
	ev := asMap(eventsOf(t, s, "demo", "0")[len(eventsOf(t, s, "demo", "0"))-1])
	if asStr(ev["type"]) != "annotation.resolved" {
		t.Fatalf("last event = %q", ev["type"])
	}
	if asStr(asMap(ev["payload"])["reply"]) != "rebased axis at 0" {
		t.Errorf("payload = %s", canonical(ev["payload"]))
	}
}

func TestAnnotations_ResolvedDefaultsToTrue(t *testing.T) {
	// The CLI and MCP surfaces only ever resolve; `resolve` with a reply is the norm.
	s, card := annotationsSpace(t)
	ann := createAnnotation(t, s, asStr(card["id"]), map[string]any{"body": "b"})
	r := s.patch(t, "/api/spaces/demo/annotations/"+asStr(ann["id"]), agentP(),
		map[string]any{"reply": "done"})
	if !asBool(r.obj()["resolved"]) {
		t.Errorf("resolved = %v", r.obj()["resolved"])
	}
}

func TestAnnotations_ReopeningEmitsNoResolveEvent(t *testing.T) {
	s, card := annotationsSpace(t)
	ann := createAnnotation(t, s, asStr(card["id"]), map[string]any{"body": "b"})
	s.patch(t, "/api/spaces/demo/annotations/"+asStr(ann["id"]), agentP(),
		map[string]any{"resolved": true})
	before := len(eventsOf(t, s, "demo", "0"))
	r := s.patch(t, "/api/spaces/demo/annotations/"+asStr(ann["id"]), humanP(),
		map[string]any{"resolved": false})
	if asBool(r.obj()["resolved"]) {
		t.Errorf("resolved = %v", r.obj()["resolved"])
	}
	after := len(eventsOf(t, s, "demo", "0"))
	if after != before {
		t.Errorf("reopening emitted %d events; there is no annotation.reopened type", after-before)
	}
}

func TestAnnotations_ResolvingAnUnknownAnnotationIs404(t *testing.T) {
	s, _ := annotationsSpace(t)
	if r := s.patch(t, "/api/spaces/demo/annotations/a_nope", agentP(),
		map[string]any{"resolved": true}); r.status != 404 {
		t.Fatalf("%d", r.status)
	}
}

func TestAnnotations_ListOrderIsCreationOrder(t *testing.T) {
	s, card := annotationsSpace(t)
	var created []string
	for i := 0; i < 3; i++ {
		ann := createAnnotation(t, s, asStr(card["id"]), map[string]any{"body": string(rune('0' + i))})
		created = append(created, asStr(ann["id"]))
	}
	if got := ids(s.get(t, "/api/spaces/demo/annotations", nil).arr()); !equalStrings(got, created...) {
		t.Errorf("list = %v, want %v", got, created)
	}
}
