// SPEC §4.1 — the get_feedback contract, and §10's non-arbitrary decisions.
//
// Delta computation lives in the server (contracts/README.md), so this is where the
// rules are pinned. roundtrip_test.go checks the same rules against the frozen
// fixture; this file checks the edges the fixture cannot reach.
package conformance

import (
	"net/url"
	"testing"
)

// codexP is a third agent, with its own cursor.
func codexP() url.Values { return actorParams("codex", "agent") }

func feedbackSpace(t *testing.T) *server {
	t.Helper()
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	return s
}

// feedback is the one call every test here makes, with the Feedback schema
// checked every time — the python helper did the same.
func feedback(t *testing.T, s *server, actor string, kv ...string) map[string]any {
	t.Helper()
	q := params("actor", actor)
	for i := 0; i+1 < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}
	r := s.get(t, "/api/spaces/demo/feedback", q)
	if r.status != 200 {
		t.Fatalf("feedback: %d %s", r.status, r.str())
	}
	assertValid(t, r.body, "Feedback", false)
	return r.obj()
}

func emptyBuckets(t *testing.T, fb map[string]any, buckets ...string) {
	t.Helper()
	for _, bucket := range buckets {
		if got := asArr(fb[bucket]); len(got) != 0 {
			t.Errorf("%s = %v, want empty", bucket, canonical(got))
		}
	}
}

// --- own-event filtering (SPEC §10) -----------------------------------------

func TestFeedback_AnAgentNeverReadsItsOwnWritesBack(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), agentP(), map[string]any{"text": "v2"})

	fb := feedback(t, s, "claude-code")
	emptyBuckets(t, fb, "cards_edited", "cards_moved")
	if asStr(fb["summary"]) != "" {
		t.Errorf("summary = %q, want empty", fb["summary"])
	}
}

func TestFeedback_AnotherAgentsWritesAreFeedback(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo", "title", `"Option B"`)
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), codexP(), map[string]any{"text": "v2"})

	fb := feedback(t, s, "claude-code")
	assertJSONEq(t, "cards_edited", []any{map[string]any{
		"id": card["id"], "title": "Option B", "changed": []any{"text"}, "actor": "codex"}},
		fb["cards_edited"])
}

func TestFeedback_CursorsAreIndependentPerActor(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(), map[string]any{"text": "v2"})

	if got := len(asArr(feedback(t, s, "claude-code")["cards_edited"])); got != 1 { // consumes
		t.Fatalf("claude-code saw %d edits", got)
	}
	emptyBuckets(t, feedback(t, s, "claude-code"), "cards_edited")
	if got := len(asArr(feedback(t, s, "codex")["cards_edited"])); got != 1 {
		t.Errorf("codex saw %d edits, want 1: codex has its own cursor", got)
	}
}

// --- annotations are cursor-independent (SPEC §10) --------------------------

func TestFeedback_UnresolvedAnnotationsComeBackEveryCall(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": card["id"], "body": "fix the axis"})
	for i := 0; i < 3; i++ {
		fb := feedback(t, s, "claude-code")
		var bodies []string
		for _, a := range asArr(fb["annotations"]) {
			bodies = append(bodies, asStr(asMap(a)["body"]))
		}
		if !equalStrings(bodies, "fix the axis") {
			t.Fatalf("call %d: annotations = %v", i, bodies)
		}
	}
}

func TestFeedback_ResolvedAnnotationsDisappear(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	ann := asMap(s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": card["id"], "body": "b"}).body)
	s.patch(t, "/api/spaces/demo/annotations/"+asStr(ann["id"]), agentP(),
		map[string]any{"resolved": true, "reply": "done"})
	emptyBuckets(t, feedback(t, s, "claude-code"), "annotations")
}

func TestFeedback_AnAgentSeesItsOwnAnnotationsToo(t *testing.T) {
	// Own-event filtering governs deltas, not the annotation list.
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	s.post(t, "/api/spaces/demo/annotations", agentP(),
		map[string]any{"card_id": card["id"], "body": "self note"})
	var bodies []string
	for _, a := range asArr(feedback(t, s, "claude-code")["annotations"]) {
		bodies = append(bodies, asStr(asMap(a)["body"]))
	}
	if !equalStrings(bodies, "self note") {
		t.Errorf("annotations = %v", bodies)
	}
}

func TestFeedback_AnnotationsCarryCardTitleAndStaleness(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo", "title", `"Render time"`)
	s.post(t, "/api/spaces/demo/annotations", humanP(), map[string]any{
		"card_id": card["id"], "body": "b",
		"selector": jlit(t, `{"type": "point", "x": 0.3, "y": 0.6}`)})
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), codexP(), map[string]any{"text": "v2"})

	ann := asMap(asArr(feedback(t, s, "claude-code")["annotations"])[0])
	if asStr(ann["card_title"]) != "Render time" {
		t.Errorf("card_title = %v", ann["card_title"])
	}
	if !asBool(ann["stale"]) {
		t.Error("annotation is not stale after a codex edit")
	}
	assertJSONEq(t, "selector", jlit(t, `{"type": "point", "x": 0.3, "y": 0.6}`), ann["selector"])
}

func TestFeedback_AnnotationsOnDeletedCardsStillSurface(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo", "title", `"Option D"`)
	s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": card["id"], "body": "why?"})
	s.delete(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP())
	var bodies []string
	for _, a := range asArr(feedback(t, s, "claude-code")["annotations"]) {
		bodies = append(bodies, asStr(asMap(a)["body"]))
	}
	if !equalStrings(bodies, "why?") {
		t.Errorf("annotations = %v", bodies)
	}
}

// --- replies on resolve (AMENDMENTS #9, issue #22) ---------------------------
//
// The incident: the human answered a comment by resolving it with a reply, and
// the answer was stored where nobody reads it. It is now a cursor-governed
// bucket, delivered exactly once to the resolver's counterpart.

func TestFeedback_AReplyOnResolveReachesTheOtherSideOnce(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	ann := asMap(s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": card["id"], "body": "can you rebase the axis?"}).body)
	s.patch(t, "/api/spaces/demo/annotations/"+asStr(ann["id"]), agentP(),
		map[string]any{"resolved": true, "reply": "rebased axis at 0"})

	fb := feedback(t, s, "codex")
	replies := asArr(fb["replies"])
	if len(replies) != 1 {
		t.Fatalf("replies = %v, want exactly one", canonical(replies))
	}
	reply := asMap(replies[0])
	if asStr(reply["id"]) != asStr(ann["id"]) ||
		asStr(reply["reply"]) != "rebased axis at 0" ||
		asStr(reply["actor"]) != "claude-code" ||
		asStr(reply["body"]) != "can you rebase the axis?" ||
		asStr(reply["card_title"]) != "Card" {
		t.Errorf("reply row = %s", canonical(reply))
	}
	if !contains(fb["summary"], "1 reply on resolve") {
		t.Errorf("summary = %q", fb["summary"])
	}
	// Cursor-governed: consumed with everything else.
	emptyBuckets(t, feedback(t, s, "codex"), "replies")
}

func contains(v any, substr string) bool {
	s, _ := v.(string)
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i+len(substr) <= len(s); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}

func TestFeedback_NobodyReadsTheirOwnReplyBack(t *testing.T) {
	// The human's reply reaches the agent; the agent's own reaches the human.
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	ann := asMap(s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": card["id"], "body": "b"}).body)
	s.patch(t, "/api/spaces/demo/annotations/"+asStr(ann["id"]), agentP(),
		map[string]any{"resolved": true, "reply": "done"})

	fb := feedback(t, s, "claude-code")
	emptyBuckets(t, fb, "replies")
	if contains(fb["summary"], "reply") {
		t.Errorf("summary = %q", fb["summary"])
	}
}

func TestFeedback_ResolvingWithoutAReplyIsStillPureAcknowledgment(t *testing.T) {
	// Only an answer is a message. No reply key, an explicit null and "" land nowhere.
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	var anns []string
	for _, body := range []string{"no reply key", "explicit null", "empty string"} {
		ann := asMap(s.post(t, "/api/spaces/demo/annotations", humanP(),
			map[string]any{"card_id": card["id"], "body": body}).body)
		anns = append(anns, asStr(ann["id"]))
	}
	s.patch(t, "/api/spaces/demo/annotations/"+anns[0], agentP(), map[string]any{"resolved": true})
	s.patch(t, "/api/spaces/demo/annotations/"+anns[1], agentP(),
		map[string]any{"resolved": true, "reply": nil})
	s.patch(t, "/api/spaces/demo/annotations/"+anns[2], agentP(),
		map[string]any{"resolved": true, "reply": ""})
	emptyBuckets(t, feedback(t, s, "codex"), "replies")
}

func TestFeedback_ReopeningAndResolvingAgainDeliversAgain(t *testing.T) {
	// One entry per resolve event: a re-resolved answer is a second message.
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	ann := asMap(s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": card["id"], "body": "b"}).body)
	target := "/api/spaces/demo/annotations/" + asStr(ann["id"])
	s.patch(t, target, agentP(), map[string]any{"resolved": true, "reply": "first"})
	s.patch(t, target, humanP(), map[string]any{"resolved": false})
	s.patch(t, target, agentP(), map[string]any{"resolved": true, "reply": "second"})

	var replies []string
	for _, r := range asArr(feedback(t, s, "codex")["replies"]) {
		replies = append(replies, asStr(asMap(r)["reply"]))
	}
	if !equalStrings(replies, "first", "second") {
		t.Errorf("replies = %v, want [first second]", replies)
	}
}

// --- cursor mechanics --------------------------------------------------------

func TestFeedback_AFreshActorStartsAtZero(t *testing.T) {
	s := feedbackSpace(t)
	oneCard(t, s, "demo", "title", `"A"`)
	if got := len(asArr(feedback(t, s, "codex")["cards_edited"])); got != 0 {
		t.Errorf("fresh actor saw %d edits", got)
	}
	fb := feedback(t, s, "codex")
	if got := num(t, fb["cursor"]); got != 2 {
		t.Errorf("cursor = %v, want 2: space.created plus one card", got)
	}
}

func TestFeedback_AdvanceFalseDoesNotConsume(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(), map[string]any{"text": "v2"})
	for i := 0; i < 3; i++ {
		if got := len(asArr(feedback(t, s, "claude-code", "advance", "false")["cards_edited"])); got != 1 {
			t.Fatalf("peek %d saw %d edits", i, got)
		}
	}
	if got := len(asArr(feedback(t, s, "claude-code")["cards_edited"])); got != 1 {
		t.Fatalf("consuming call saw %d edits", got)
	}
	emptyBuckets(t, feedback(t, s, "claude-code"), "cards_edited")
}

func TestFeedback_ExplicitSinceOverridesTheStoredCursor(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(), map[string]any{"text": "v2"})
	feedback(t, s, "claude-code") // consume
	if got := len(asArr(feedback(t, s, "claude-code", "since", "0", "advance", "false")["cards_edited"])); got != 1 {
		t.Errorf("since=0 saw %d edits, want 1", got)
	}
}

func TestFeedback_CursorIsAlwaysTheSpacesCurrentSeq(t *testing.T) {
	s := feedbackSpace(t)
	oneCard(t, s, "demo")
	oneCard(t, s, "demo")
	if got := num(t, feedback(t, s, "claude-code", "advance", "false")["cursor"]); got != 3 {
		t.Errorf("cursor = %v, want 3", got)
	}
	if got := num(t, feedback(t, s, "claude-code", "since", "0", "advance", "false")["cursor"]); got != 3 {
		t.Errorf("cursor with since=0 = %v, want 3", got)
	}
}

func TestFeedback_FeedbackOnAnUnknownSpaceIs404(t *testing.T) {
	s := startServer(t)
	if r := s.get(t, "/api/spaces/nope/feedback", params("actor", "x")); r.status != 404 {
		t.Fatalf("%d", r.status)
	}
}

// --- bucketing ---------------------------------------------------------------

func TestFeedback_MovesAreBucketedAwayFromEdits(t *testing.T) {
	// SPEC §4.1: 'the human dragging things around is usually noise'.
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo", "title", `"Option A"`)
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"x": 40, "y": 90})
	fb := feedback(t, s, "claude-code")
	assertJSONEq(t, "cards_moved", []any{map[string]any{
		"id": card["id"], "title": "Option A", "actor": "human"}}, fb["cards_moved"])
	emptyBuckets(t, fb, "cards_edited")
}

func TestFeedback_RepeatedEventsOnOneCardCollapseToOneRow(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	for i := 0; i < 4; i++ {
		s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
			map[string]any{"x": i})
	}
	if got := len(asArr(feedback(t, s, "claude-code")["cards_moved"])); got != 1 {
		t.Errorf("cards_moved has %d rows, want 1", got)
	}
}

func TestFeedback_ChangedKeysAreUnionedAcrossEdits(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(), map[string]any{"text": "v2"})
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(), map[string]any{"sp_title": "New"})
	edited := asArr(feedback(t, s, "claude-code")["cards_edited"])
	assertJSONEq(t, "changed", []any{"sp_title", "text"}, asMap(edited[0])["changed"])
}

func TestFeedback_ADeletionSupersedesAnEditOrAMove(t *testing.T) {
	// Telling an agent a card was edited and then deleted is noise; it is gone.
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo", "title", `"Option D"`)
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(), map[string]any{"text": "v2"})
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(), map[string]any{"x": 10})
	s.delete(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP())

	fb := feedback(t, s, "claude-code")
	if got := ids(asArr(fb["cards_deleted"])); !equalStrings(got, asStr(card["id"])) {
		t.Errorf("cards_deleted = %v", got)
	}
	emptyBuckets(t, fb, "cards_edited", "cards_moved")
}

func TestFeedback_AnEditSupersedesAMove(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(), map[string]any{"x": 10})
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(), map[string]any{"text": "v2"})
	fb := feedback(t, s, "claude-code")
	if got := len(asArr(fb["cards_edited"])); got != 1 {
		t.Errorf("cards_edited = %d rows, want 1", got)
	}
	emptyBuckets(t, fb, "cards_moved")
}

func TestFeedback_ALinkCreatedAndRemovedInTheWindowReportsAsNeither(t *testing.T) {
	s := feedbackSpace(t)
	nodes := addCards(t, s, "demo", []any{
		map[string]any{"title": "A", "content": "a"},
		map[string]any{"title": "B", "content": "b"}}, nil)
	a, b := asMap(nodes[0]), asMap(nodes[1])
	link := asMap(s.post(t, "/api/spaces/demo/links", humanP(), map[string]any{
		"edges": []any{map[string]any{
			"fromNode": a["id"], "toNode": b["id"], "label": "x"}}}).arr()[0])
	s.delete(t, "/api/spaces/demo/links/"+asStr(link["id"]), humanP())
	fb := feedback(t, s, "claude-code")
	emptyBuckets(t, fb, "links_added", "links_removed")
}

func TestFeedback_LinksReportEndpointsAndLabel(t *testing.T) {
	s := feedbackSpace(t)
	nodes := addCards(t, s, "demo", []any{
		map[string]any{"title": "A", "content": "a"},
		map[string]any{"title": "B", "content": "b"}}, nil)
	a, b := asMap(nodes[0]), asMap(nodes[1])
	link := asMap(s.post(t, "/api/spaces/demo/links", humanP(), map[string]any{
		"edges": []any{map[string]any{
			"fromNode": a["id"], "toNode": b["id"], "label": "depends on"}}}).arr()[0])
	assertJSONEq(t, "links_added", []any{map[string]any{
		"id": link["id"], "from": a["id"], "to": b["id"], "label": "depends on",
		"actor": "human"}}, feedback(t, s, "claude-code")["links_added"])
}

func TestFeedback_LinkRemovalIsReported(t *testing.T) {
	s := feedbackSpace(t)
	nodes := addCards(t, s, "demo", []any{
		map[string]any{"title": "A", "content": "a"},
		map[string]any{"title": "B", "content": "b"}}, nil)
	a, b := asMap(nodes[0]), asMap(nodes[1])
	link := asMap(s.post(t, "/api/spaces/demo/links", agentP(), map[string]any{
		"edges": []any{map[string]any{"fromNode": a["id"], "toNode": b["id"]}}}).arr()[0])
	feedback(t, s, "claude-code") // consume its own writes
	s.delete(t, "/api/spaces/demo/links/"+asStr(link["id"]), humanP())
	assertJSONEq(t, "links_removed", []any{map[string]any{
		"id": link["id"], "actor": "human"}}, feedback(t, s, "claude-code")["links_removed"])
}

// --- summary -----------------------------------------------------------------
//
// Pinned by contracts/fixtures/feedback.claude-code.since-12.json:
//
//	"2 open comments (1 stale), 1 card edited, 1 deleted, 1 moved, 1 new link."
//
// and by contracts/fixtures/feedback.human.json:
//
//	"2 open comments (1 stale), 1 reply on resolve, 1 card edited, 3 new links."
//
// Parts, in this order, omitting any that are zero, joined with ", " and closed
// with a full stop:
//
//	{n} open comment[s][ ({k} stale)] · {n} repl(y|ies) on resolve
//	· {n} card[s] edited · {n} deleted · {n} moved · {n} new link[s]
//	· {n} link[s] removed
//
// Empty string when every bucket is empty.

func TestFeedback_SummaryIsEmptyWhenNothingChanged(t *testing.T) {
	s := feedbackSpace(t)
	if got := asStr(feedback(t, s, "claude-code")["summary"]); got != "" {
		t.Errorf("summary = %q, want empty", got)
	}
}

func TestFeedback_SummarySingularAndPlural(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": card["id"], "body": "one"})
	if got := asStr(feedback(t, s, "claude-code", "advance", "false")["summary"]); got != "1 open comment." {
		t.Errorf("summary = %q", got)
	}
	s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": card["id"], "body": "two"})
	if got := asStr(feedback(t, s, "claude-code", "advance", "false")["summary"]); got != "2 open comments." {
		t.Errorf("summary = %q", got)
	}
}

func TestFeedback_SummaryCountsStale(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": card["id"], "body": "one"})
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), codexP(), map[string]any{"text": "v2"})
	want := "1 open comment (1 stale), 1 card edited."
	if got := asStr(feedback(t, s, "claude-code", "advance", "false")["summary"]); got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

func TestFeedback_SummaryReproducesTheFixtureGrammar(t *testing.T) {
	s := feedbackSpace(t)
	nodes := addCards(t, s, "demo", []any{
		map[string]any{"title": "A", "content": "A"},
		map[string]any{"title": "B", "content": "B"},
		map[string]any{"title": "C", "content": "C"},
		map[string]any{"title": "D", "content": "D"}}, nil)
	a, b, _, d := asMap(nodes[0]), asMap(nodes[1]), asMap(nodes[2]), asMap(nodes[3])
	feedback(t, s, "claude-code") // consume own creations

	s.post(t, "/api/spaces/demo/annotations", humanP(), map[string]any{"card_id": a["id"], "body": "1"})
	s.post(t, "/api/spaces/demo/annotations", humanP(), map[string]any{"card_id": b["id"], "body": "2"})
	s.patch(t, "/api/spaces/demo/cards/"+asStr(b["id"]), humanP(), map[string]any{"text": "v2"})
	s.delete(t, "/api/spaces/demo/cards/"+asStr(d["id"]), humanP())
	s.patch(t, "/api/spaces/demo/cards/"+asStr(a["id"]), humanP(), map[string]any{"x": 1})
	s.post(t, "/api/spaces/demo/links", humanP(), map[string]any{
		"edges": []any{map[string]any{
			"fromNode": a["id"], "toNode": asMap(nodes[2])["id"], "label": "depends on"}}})

	want := "2 open comments (1 stale), 1 card edited, 1 deleted, 1 moved, 1 new link."
	if got := asStr(feedback(t, s, "claude-code")["summary"]); got != want {
		t.Errorf("summary = %q, want %q", got, want)
	}
}

func TestFeedback_SummaryReportsRemovedLinks(t *testing.T) {
	s := feedbackSpace(t)
	nodes := addCards(t, s, "demo", []any{
		map[string]any{"title": "A", "content": "a"},
		map[string]any{"title": "B", "content": "b"}}, nil)
	a, b := asMap(nodes[0]), asMap(nodes[1])
	link := asMap(s.post(t, "/api/spaces/demo/links", agentP(),
		map[string]any{"edges": []any{map[string]any{
			"fromNode": a["id"], "toNode": b["id"]}}}).arr()[0])
	feedback(t, s, "claude-code")
	s.delete(t, "/api/spaces/demo/links/"+asStr(link["id"]), humanP())
	if got := asStr(feedback(t, s, "claude-code")["summary"]); got != "1 link removed." {
		t.Errorf("summary = %q", got)
	}
}

func TestFeedback_SummarySlotsRepliesAfterComments(t *testing.T) {
	s := feedbackSpace(t)
	card := oneCard(t, s, "demo")
	ann := asMap(s.post(t, "/api/spaces/demo/annotations", humanP(),
		map[string]any{"card_id": card["id"], "body": "b"}).body)
	s.patch(t, "/api/spaces/demo/annotations/"+asStr(ann["id"]), agentP(),
		map[string]any{"resolved": true, "reply": "done"})
	if got := asStr(feedback(t, s, "codex")["summary"]); got != "1 reply on resolve." {
		t.Errorf("summary = %q", got)
	}
}
