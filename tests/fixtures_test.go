// The fixtures are valid, and they exercise what contracts/README.md claims.
//
// These assertions are the reason the fixtures can be trusted by other consumers
// with no server running. If one fails, the amendment process in
// contracts/README.md applies.
package conformance

import (
	"strings"
	"testing"
)

func TestFixtures_SpaceMatchesSchema(t *testing.T) {
	assertValid(t, fixture(t, "space.json"), "Space", false)
}

func TestFixtures_CanvasMatchesSchema(t *testing.T) {
	for _, doc := range []struct {
		name string
		doc  any
	}{
		{"live", fixture(t, "canvas.json")},
		{"with-deleted", fixture(t, "canvas.with-deleted.json")},
	} {
		t.Run(doc.name, func(t *testing.T) {
			assertValid(t, doc.doc, "Canvas", false)
		})
	}
}

func TestFixtures_CanvasIsValidJsonCanvas10(t *testing.T) {
	// SPEC §2.1: the wire format is JSON Canvas 1.0. The python harness leans on
	// the openjsoncanvas library; the ids-round-trip assertion below is what that
	// check actually verifies here, alongside the Canvas schema above.
	canvas := asMap(fixture(t, "canvas.json"))
	nodeIDs, edgeIDs := map[string]bool{}, map[string]bool{}
	for _, n := range asArr(canvas["nodes"]) {
		nodeIDs[asStr(asMap(n)["id"])] = true
	}
	for _, e := range asArr(canvas["edges"]) {
		edgeIDs[asStr(asMap(e)["id"])] = true
	}
	for _, n := range asArr(canvas["nodes"]) {
		if !nodeIDs[asStr(asMap(n)["id"])] {
			t.Fatalf("node id %v missing from the node set", asMap(n)["id"])
		}
	}
	for _, e := range asArr(canvas["edges"]) {
		if !edgeIDs[asStr(asMap(e)["id"])] {
			t.Fatalf("edge id %v missing from the edge set", asMap(e)["id"])
		}
	}
}

func TestFixtures_AnnotationsMatchSchema(t *testing.T) {
	assertValid(t, fixture(t, "annotations.json"), "Annotation", true)
}

func TestFixtures_EventsMatchSchema(t *testing.T) {
	assertValid(t, asMap(fixture(t, "events.json"))["events"], "Event", true)
}

func TestFixtures_FeedbackMatchesSchema(t *testing.T) {
	assertValid(t, fixture(t, "feedback.claude-code.since-12.json"), "Feedback", false)
	assertValid(t, fixture(t, "feedback.human.json"), "Feedback", false)
}

// --- the counts contracts/README.md advertises -------------------------------

func TestFixtures_FixtureInventory(t *testing.T) {
	canvas := asMap(fixture(t, "canvas.json"))
	annotations := fixture(t, "annotations.json")
	events := asMap(fixture(t, "events.json"))
	space := asMap(fixture(t, "space.json"))
	if len(asArr(canvas["nodes"])) != 6 || len(asArr(canvas["edges"])) != 4 ||
		len(asArr(annotations)) != 3 || len(asArr(events["events"])) != 19 {
		t.Fatalf("counts: %d nodes, %d edges, %d annotations, %d events",
			len(asArr(canvas["nodes"])), len(asArr(canvas["edges"])),
			len(asArr(annotations)), len(asArr(events["events"])))
	}
	assertJSONEq(t, "counts", jlit(t, `{"cards": 6, "links": 4, "open_annotations": 2}`), space["counts"])
}

func TestFixtures_EventSeqsAreContiguousFromOne(t *testing.T) {
	events := asMap(fixture(t, "events.json"))["events"].([]any)
	space := asMap(fixture(t, "space.json"))
	for i, e := range events {
		if got := num(t, asMap(e)["seq"]); got != float64(i+1) {
			t.Fatalf("event %d has seq %v", i, got)
		}
	}
	cursor := num(t, asMap(fixture(t, "events.json"))["cursor"])
	if cursor != 19 || cursor != num(t, space["seq"]) {
		t.Errorf("cursor %v != last seq or space seq", cursor)
	}
}

func TestFixtures_EveryEventSubjectExists(t *testing.T) {
	withDeleted := asMap(fixture(t, "canvas.with-deleted.json"))
	known := map[string]bool{}
	for _, n := range asArr(withDeleted["nodes"]) {
		known[asStr(asMap(n)["id"])] = true
	}
	for _, e := range asArr(withDeleted["edges"]) {
		known[asStr(asMap(e)["id"])] = true
	}
	for _, a := range asArr(fixture(t, "annotations.json")) {
		known[asStr(asMap(a)["id"])] = true
	}
	for _, e := range asArr(asMap(fixture(t, "events.json"))["events"]) {
		subject := asStr(asMap(e)["subject_id"])
		if !known[subject] {
			t.Errorf("event %v names unknown subject %q", asMap(e)["seq"], subject)
		}
	}
}

// --- the rules the fixtures exist to pin (contracts/README.md) ---------------

func TestFixtures_OwnEventsAreFilteredFromTheAuthorsFeedback(t *testing.T) {
	events := asMap(fixture(t, "events.json"))["events"].([]any)
	fb := asMap(fixture(t, "feedback.claude-code.since-12.json"))
	own := map[string]bool{}
	for _, e := range events {
		ev := asMap(e)
		if num(t, ev["seq"]) > 12 && asStr(ev["actor"]) == "claude-code" {
			own[asStr(ev["subject_id"])] = true
		}
	}
	if len(own) != 2 {
		t.Fatalf("own events = %v, want exactly the two after seq 12", own)
	}
	reported := map[string]bool{}
	for _, bucket := range []string{"cards_edited", "cards_deleted", "cards_moved", "links_added"} {
		for _, row := range asArr(fb[bucket]) {
			reported[asStr(asMap(row)["id"])] = true
		}
	}
	for id := range reported {
		if own[id] {
			t.Errorf("own event %s was reported back to its author", id)
		}
	}
	for _, bucket := range []string{"replies", "cards_edited", "cards_deleted", "cards_moved",
		"links_added", "links_removed"} {
		for _, row := range asArr(fb[bucket]) {
			if asStr(asMap(row)["actor"]) == "claude-code" {
				t.Errorf("%s carries the author's own write", bucket)
			}
		}
	}
}

func TestFixtures_UnresolvedAnnotationsIgnoreTheCursor(t *testing.T) {
	// a_1 was created at seq 12 — at or before the cursor — and still appears.
	events := asMap(fixture(t, "events.json"))["events"].([]any)
	created := map[string]float64{}
	for _, e := range events {
		ev := asMap(e)
		if asStr(ev["type"]) == "annotation.created" {
			created[asStr(ev["subject_id"])] = num(t, ev["seq"])
		}
	}
	if created["a_1"] != 12 {
		t.Fatalf("a_1 created at seq %v, want 12", created["a_1"])
	}
	fb := asMap(fixture(t, "feedback.claude-code.since-12.json"))
	found := false
	for _, a := range asArr(fb["annotations"]) {
		if asStr(asMap(a)["id"]) == "a_1" {
			found = true
		}
	}
	if !found {
		t.Error("a_1 did not appear despite being at the cursor")
	}
}

func TestFixtures_AReplyOnResolveReachesTheOtherActorOnce(t *testing.T) {
	// AMENDMENTS #9 / issue #22: event 18 resolved a_3 with an answer.
	//
	// For human — the resolver's counterpart — that is one `replies` entry, reply
	// included. For claude-code the resolve is its own event: nobody reads their own
	// reply back. This is the whole reason the bucket exists.
	events := asMap(fixture(t, "events.json"))["events"].([]any)
	var resolve map[string]any
	for _, e := range events {
		if asStr(asMap(e)["type"]) == "annotation.resolved" {
			resolve = asMap(e)
		}
	}
	if asStr(resolve["subject_id"]) != "a_3" || asStr(resolve["actor"]) != "claude-code" {
		t.Fatalf("resolve event: %+v", resolve)
	}
	assertJSONEq(t, "resolve payload", jlit(t, `{"reply": "added position:sticky"}`), resolve["payload"])

	humanFeedback := asMap(fixture(t, "feedback.human.json"))
	replies := asArr(humanFeedback["replies"])
	if len(replies) != 1 {
		t.Fatalf("human replies = %v, want exactly one", canonical(replies))
	}
	reply := asMap(replies[0])
	if asStr(reply["id"]) != "a_3" || asStr(reply["reply"]) != "added position:sticky" ||
		asStr(reply["actor"]) != "claude-code" {
		t.Errorf("reply row: %s", canonical(reply))
	}
	if asStr(reply["resolved_at"]) != asStr(resolve["ts"]) {
		t.Errorf("reply resolved_at %v != event ts %v", reply["resolved_at"], resolve["ts"])
	}
	for _, a := range asArr(fixture(t, "annotations.json")) {
		if asStr(asMap(a)["id"]) == "a_3" && asStr(reply["body"]) != asStr(asMap(a)["body"]) {
			t.Errorf("reply body %v != annotation body %v", reply["body"], asMap(a)["body"])
		}
	}
	if len(asArr(asMap(fixture(t, "feedback.claude-code.since-12.json"))["replies"])) != 0 {
		t.Error("claude-code must not read its own reply back")
	}
	if !strings.Contains(asStr(humanFeedback["summary"]), "1 reply on resolve") {
		t.Errorf("summary %q lacks the reply count", humanFeedback["summary"])
	}
	if strings.Contains(asStr(asMap(fixture(t, "feedback.claude-code.since-12.json"))["summary"]), "reply") {
		t.Error("claude-code's summary mentions replies")
	}
}

func TestFixtures_ResolvedAnnotationsAreExcluded(t *testing.T) {
	resolved := false
	for _, a := range asArr(fixture(t, "annotations.json")) {
		if asStr(asMap(a)["id"]) == "a_3" {
			resolved = asBool(asMap(a)["resolved"])
		}
	}
	if !resolved {
		t.Fatal("a_3 is not resolved in the fixture")
	}
	for _, a := range asArr(asMap(fixture(t, "feedback.claude-code.since-12.json"))["annotations"]) {
		if asStr(asMap(a)["id"]) == "a_3" {
			t.Error("a_3 appeared in feedback despite being resolved")
		}
	}
}

func TestFixtures_StalenessIsCardRevLessThanCurrentRev(t *testing.T) {
	nodes := asMap(fixture(t, "canvas.with-deleted.json"))["nodes"].([]any)
	rev := map[string]float64{}
	for _, n := range nodes {
		node := asMap(n)
		if r, ok := node["sp_rev"]; ok {
			rev[asStr(node["id"])] = num(t, r)
		} else {
			rev[asStr(node["id"])] = 1
		}
	}
	stale := map[string]bool{}
	for _, a := range asArr(fixture(t, "annotations.json")) {
		ann := asMap(a)
		want := num(t, ann["card_rev"]) < rev[asStr(ann["card_id"])]
		if asBool(ann["stale"]) != want {
			t.Errorf("%s: stale %v, want %v", ann["id"], ann["stale"], want)
		}
		if asBool(ann["stale"]) {
			stale[asStr(ann["id"])] = true
		}
	}
	if len(stale) != 1 || !stale["a_1"] {
		t.Errorf("stale set = %v, want {a_1}", stale)
	}
}

func TestFixtures_MovedIsNotEdited(t *testing.T) {
	// Event 15 moved c_opt_a without bumping rev; it must not land in cards_edited.
	events := asMap(fixture(t, "events.json"))["events"].([]any)
	var moved map[string]any
	for _, e := range events {
		if asStr(asMap(e)["type"]) == "card.moved" {
			moved = asMap(e)
		}
	}
	if num(t, moved["seq"]) != 15 || asStr(moved["subject_id"]) != "c_opt_a" {
		t.Fatalf("moved event: %+v", moved)
	}
	if _, has := asMap(moved["payload"])["rev"]; has {
		t.Error("card.moved payload carries rev")
	}
	fb := asMap(fixture(t, "feedback.claude-code.since-12.json"))
	inMoved, inEdited := false, false
	for _, row := range asArr(fb["cards_moved"]) {
		if asStr(asMap(row)["id"]) == "c_opt_a" {
			inMoved = true
		}
	}
	for _, row := range asArr(fb["cards_edited"]) {
		if asStr(asMap(row)["id"]) == "c_opt_a" {
			inEdited = true
		}
	}
	if !inMoved || inEdited {
		t.Errorf("c_opt_a: moved=%v edited=%v", inMoved, inEdited)
	}
}

func TestFixtures_SoftDeleteKeepsTheCardVisibleToAgents(t *testing.T) {
	live := map[string]bool{}
	for _, n := range asArr(asMap(fixture(t, "canvas.json"))["nodes"]) {
		live[asStr(asMap(n)["id"])] = true
	}
	all := map[string]bool{}
	var deleted map[string]any
	for _, n := range asArr(asMap(fixture(t, "canvas.with-deleted.json"))["nodes"]) {
		all[asStr(asMap(n)["id"])] = true
		if asStr(asMap(n)["id"]) == "c_opt_d" {
			deleted = asMap(n)
		}
	}
	if len(all)-len(live) != 1 {
		t.Fatalf("tombstone count: %d live, %d total", len(live), len(all))
	}
	if _, ok := deleted["sp_deleted_at"]; !ok {
		t.Error("include_deleted must expose the tombstone")
	}
	if got := ids(asArr(asMap(fixture(t, "feedback.claude-code.since-12.json"))["cards_deleted"])); !sameMembers(got, "c_opt_d") {
		t.Errorf("cards_deleted = %v", got)
	}
}

func TestFixtures_AllFourRenderPathsArePresent(t *testing.T) {
	// SPEC §5 card rendering: md, svg, html text nodes plus a file node.
	canvas := asMap(fixture(t, "canvas.json"))
	kinds := map[string]bool{}
	files := 0
	for _, n := range asArr(canvas["nodes"]) {
		node := asMap(n)
		if k, ok := node["sp_kind"]; ok {
			kinds[asStr(k)] = true
		}
		if asStr(node["type"]) == "file" {
			files++
			if !strings.HasPrefix(asStr(node["file"]), "/api/spaces/redesign/media/") {
				t.Errorf("file node url %q", node["file"])
			}
			if _, ok := node["sp_kind"]; ok {
				t.Error("sp_kind is meaningful only on text nodes")
			}
		}
	}
	for _, k := range []string{"md", "svg", "html"} {
		if !kinds[k] {
			t.Errorf("no %s node in the canvas", k)
		}
	}
	if files != 1 {
		t.Errorf("%d file nodes, want 1", files)
	}
}

func TestFixtures_HtmlCardCarriesAScriptForTheSandboxTest(t *testing.T) {
	// SPEC §5: it must run inside the iframe and never in the parent frame.
	for _, n := range asArr(asMap(fixture(t, "canvas.json"))["nodes"]) {
		node := asMap(n)
		if asStr(node["sp_kind"]) == "html" {
			if !strings.Contains(asStr(node["text"]), "<script>") {
				t.Error("the html card carries no <script>")
			}
			return
		}
	}
	t.Fatal("no html card in the fixture")
}

func TestFixtures_SelectorsCoverBothV1Shapes(t *testing.T) {
	sawNil := false
	shapes := map[string]bool{}
	for _, a := range asArr(fixture(t, "annotations.json")) {
		selector := asMap(a)["selector"]
		if selector == nil {
			sawNil = true
			continue
		}
		sel := asMap(selector)
		shapes[asStr(sel["type"])] = true
		for k, v := range sel {
			if k == "type" {
				continue
			}
			f := num(t, v)
			if f < 0 || f > 1 {
				t.Errorf("%s.%s = %v outside [0,1]", asStr(asMap(a)["id"]), k, f)
			}
		}
	}
	if !sawNil {
		t.Error("no whole-card selector in the fixture")
	}
	if !shapes["point"] || !shapes["rect"] {
		t.Errorf("selector shapes = %v, want point and rect", shapes)
	}
}

func TestFixtures_DeltasAgreeWithTheEventLogAfterSeq12(t *testing.T) {
	// The fixture feedback is exactly what a correct implementation would compute.
	events := asMap(fixture(t, "events.json"))["events"].([]any)
	fb := asMap(fixture(t, "feedback.claude-code.since-12.json"))
	byType := map[string]map[string]bool{}
	for _, e := range events {
		ev := asMap(e)
		if num(t, ev["seq"]) > 12 && asStr(ev["actor"]) != "claude-code" {
			t := asStr(ev["type"])
			if byType[t] == nil {
				byType[t] = map[string]bool{}
			}
			byType[t][asStr(ev["subject_id"])] = true
		}
	}
	buckets := map[string]string{
		"cards_edited": "card.updated", "cards_deleted": "card.deleted",
		"cards_moved": "card.moved", "links_added": "link.created",
		"links_removed": "link.deleted",
	}
	for bucket, eventType := range buckets {
		got := map[string]bool{}
		for _, row := range asArr(fb[bucket]) {
			got[asStr(asMap(row)["id"])] = true
		}
		want := byType[eventType]
		if len(got) != len(want) {
			t.Errorf("%s: %v != events %v", bucket, got, want)
			continue
		}
		for id := range got {
			if !want[id] {
				t.Errorf("%s: %s not in the event log", bucket, id)
			}
		}
	}
	if num(t, fb["cursor"]) != num(t, asMap(fixture(t, "events.json"))["cursor"]) {
		t.Error("feedback cursor != events cursor")
	}
}
