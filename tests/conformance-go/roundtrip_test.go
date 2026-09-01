// A seeded database must reproduce contracts/fixtures/ through the API, exactly.
//
// This is the strongest test in the suite: it pins response *shape* (no stray keys),
// ordering, soft-delete projection, cursor seeding and delta computation in one go.
package conformance

import (
	"net/url"
	"sort"
	"strings"
	"testing"
)

func TestRoundtrip_SpaceMatchesFixture(t *testing.T) {
	s := startSeededServer(t)
	r := s.get(t, "/api/spaces/redesign", nil)
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	assertJSONEq(t, "space", fixture(t, "space.json"), r.body)
}

func TestRoundtrip_SpaceListContainsOnlyTheSeededSpace(t *testing.T) {
	s := startSeededServer(t)
	r := s.get(t, "/api/spaces", nil)
	if r.status != 200 {
		t.Fatal(r.status)
	}
	assertJSONEq(t, "spaces", []any{fixture(t, "space.json")}, r.body)
}

func TestRoundtrip_CanvasMatchesFixture(t *testing.T) {
	s := startSeededServer(t)
	r := s.get(t, "/api/spaces/redesign/canvas", nil)
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	assertJSONEq(t, "canvas", fixture(t, "canvas.json"), r.body)
}

func TestRoundtrip_CanvasIncludeDeletedMatchesFixture(t *testing.T) {
	s := startSeededServer(t)
	r := s.get(t, "/api/spaces/redesign/canvas", params("include_deleted", "true"))
	assertJSONEq(t, "canvas", fixture(t, "canvas.with-deleted.json"), r.body)
}

func TestRoundtrip_LiveCanvasNeverLeaksTheTombstone(t *testing.T) {
	s := startSeededServer(t)
	nodes := asArr(s.get(t, "/api/spaces/redesign/canvas", nil).obj()["nodes"])
	for _, n := range nodes {
		if _, leak := asMap(n)["sp_deleted_at"]; leak {
			t.Errorf("node %v leaks sp_deleted_at", asMap(n)["id"])
		}
	}
}

func TestRoundtrip_AnnotationsMatchFixture(t *testing.T) {
	s := startSeededServer(t)
	r := s.get(t, "/api/spaces/redesign/annotations", nil)
	assertJSONEq(t, "annotations", fixture(t, "annotations.json"), r.body)
}

func TestRoundtrip_AnnotationFilters(t *testing.T) {
	s := startSeededServer(t)
	open := s.get(t, "/api/spaces/redesign/annotations", params("resolved", "false")).arr()
	if got := ids(open); !equalStrings(got, "a_1", "a_2") {
		t.Errorf("open annotations = %v, want [a_1 a_2]", got)
	}
	done := s.get(t, "/api/spaces/redesign/annotations", params("resolved", "true")).arr()
	if got := ids(done); !equalStrings(got, "a_3") {
		t.Errorf("resolved annotations = %v, want [a_3]", got)
	}
	onChart := s.get(t, "/api/spaces/redesign/annotations", params("card_id", "c_chart")).arr()
	if got := ids(onChart); !equalStrings(got, "a_1") {
		t.Errorf("annotations on c_chart = %v, want [a_1]", got)
	}
}

func TestRoundtrip_EventsMatchFixture(t *testing.T) {
	s := startSeededServer(t)
	r := s.get(t, "/api/spaces/redesign/events", nil)
	assertJSONEq(t, "events", fixture(t, "events.json"), r.body)
}

func TestRoundtrip_FeedbackMatchesFixtureWithoutAnExplicitSince(t *testing.T) {
	// The seed parks claude-code's cursor at 12, so the default call is the
	// `since=12` fixture. This is the §4.1 contract end to end.
	s := startSeededServer(t)
	r := s.get(t, "/api/spaces/redesign/feedback",
		params("actor", "claude-code", "advance", "false"))
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	assertJSONEq(t, "feedback", fixture(t, "feedback.claude-code.since-12.json"), r.body)
}

func TestRoundtrip_FeedbackMatchesFixtureWithAnExplicitSince(t *testing.T) {
	s := startSeededServer(t)
	r := s.get(t, "/api/spaces/redesign/feedback",
		params("actor", "claude-code", "since", "12", "advance", "false"))
	assertJSONEq(t, "feedback", fixture(t, "feedback.claude-code.since-12.json"), r.body)
}

func TestRoundtrip_FeedbackMatchesTheHumanFixture(t *testing.T) {
	// The human has no stored cursor, so the default call starts at zero and the
	// agent's resolve-with-reply (event 18) arrives exactly once, as one reply.
	s := startSeededServer(t)
	r := s.get(t, "/api/spaces/redesign/feedback",
		params("actor", "human", "advance", "false"))
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	assertJSONEq(t, "feedback", fixture(t, "feedback.human.json"), r.body)
}

func TestRoundtrip_AdvanceConsumesTheCursor(t *testing.T) {
	s := startSeededServer(t)
	first := s.get(t, "/api/spaces/redesign/feedback", params("actor", "claude-code"))
	assertJSONEq(t, "first call", fixture(t, "feedback.claude-code.since-12.json"), first.body)

	second := s.get(t, "/api/spaces/redesign/feedback", params("actor", "claude-code")).obj()
	if got := num(t, second["cursor"]); got != 19 {
		t.Errorf("cursor = %v, want 19", got)
	}
	for _, bucket := range []string{"cards_edited", "cards_deleted", "cards_moved",
		"links_added", "links_removed", "replies"} {
		if got := asArr(second[bucket]); len(got) != 0 {
			t.Errorf("%s = %v, want empty", bucket, canonical(got))
		}
	}
	// Annotations are cursor-independent: they come back every single time.
	if got := ids(asArr(second["annotations"])); !equalStrings(got, "a_1", "a_2") {
		t.Errorf("annotations = %v, want [a_1 a_2]", got)
	}
	if got := asStr(second["summary"]); got != "2 open comments (1 stale)." {
		t.Errorf("summary = %q", got)
	}
}

func TestRoundtrip_PeekingDoesNotConsume(t *testing.T) {
	s := startSeededServer(t)
	for i := 0; i < 3; i++ {
		r := s.get(t, "/api/spaces/redesign/feedback",
			params("actor", "claude-code", "advance", "false"))
		assertJSONEq(t, "peek", fixture(t, "feedback.claude-code.since-12.json"), r.body)
	}
}

func TestRoundtrip_AnUnknownActorStartsAtZeroAndSeesEverything(t *testing.T) {
	s := startSeededServer(t)
	fb := s.get(t, "/api/spaces/redesign/feedback",
		params("actor", "codex", "advance", "false")).obj()

	if got := ids(asArr(fb["cards_edited"])); !sameMembers(got, "c_opt_b", "c_chart") {
		t.Errorf("cards_edited = %v", got)
	}
	if got := ids(asArr(fb["cards_deleted"])); !sameMembers(got, "c_opt_d") {
		t.Errorf("cards_deleted = %v", got)
	}
	if got := ids(asArr(fb["links_added"])); !sameMembers(got, "l_1", "l_2", "l_3", "l_4") {
		t.Errorf("links_added = %v", got)
	}
	if got := ids(asArr(fb["replies"])); !equalStrings(got, "a_3") {
		t.Errorf("replies = %v", got)
	}
}

func TestRoundtrip_TheMediaReferencedByTheFileNodeIsServed(t *testing.T) {
	s := startSeededServer(t)
	nodes := asArr(s.get(t, "/api/spaces/redesign/canvas", nil).obj()["nodes"])
	var fileNode map[string]any
	for _, n := range nodes {
		if asStr(asMap(n)["type"]) == "file" {
			fileNode = asMap(n)
		}
	}
	if fileNode == nil {
		t.Fatal("no file node in the canvas")
	}
	fileURL := asStr(fileNode["file"])
	r := s.get(t, fileURL, nil)
	if r.status != 200 {
		t.Fatalf("%s is unreachable: %d", fileURL, r.status)
	}
	if ct := r.header.Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
		t.Errorf("content-type = %q", ct)
	}
	pngMagic := []byte("\x89PNG\r\n\x1a\n")
	if len(r.raw) < 8 || string(r.raw[:8]) != string(pngMagic) {
		t.Errorf("body does not start with the png magic")
	}
}

func TestRoundtrip_SeededResponsesValidate(t *testing.T) {
	cases := []struct {
		path   string
		schema string
		many   bool
	}{
		{"/api/spaces/redesign", "Space", false},
		{"/api/spaces/redesign/canvas", "Canvas", false},
		{"/api/spaces/redesign/annotations", "Annotation", true},
	}
	for _, tc := range cases {
		t.Run(tc.schema, func(t *testing.T) {
			s := startSeededServer(t)
			r := s.get(t, tc.path, url.Values{})
			assertValid(t, r.body, tc.schema, tc.many)
		})
	}
}

// --- small shared helpers -------------------------------------------------------

func ids(rows []any) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, asStr(asMap(row)["id"]))
	}
	return out
}

// sameMembers is set equality: order ignored.
func sameMembers(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	g, w := append([]string(nil), got...), append([]string(nil), want...)
	sort.Strings(g)
	sort.Strings(w)
	for i := range g {
		if g[i] != w[i] {
			return false
		}
	}
	return true
}

func equalStrings(got []string, want ...string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
