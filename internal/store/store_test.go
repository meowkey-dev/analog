package store

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The conformance suite in tests/ is the real judge of this package. What is here is what a black-box
// suite cannot see: that a rollback drops its events, that concurrent writers do not
// collide, and that numbers survive a round trip through the blob columns.

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := Open(filepath.Join(dir, "analog.db"), filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestSchemaAppliesOnceAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "analog.db")
	first, err := Open(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.CreateSpace("demo", "Demo", "", "kai", "human"); err != nil {
		t.Fatal(err)
	}
	first.Close()

	// Reopening must not wipe or re-apply the schema.
	second, err := Open(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	if _, err := second.Space("demo"); err != nil {
		t.Errorf("reopening lost the data: %v", err)
	}
}

// --- events --------------------------------------------------------------------

func TestARollbackDropsItsEvents(t *testing.T) {
	s := newTestStore(t)
	space, err := s.CreateSpace("demo", "Demo", "", "kai", "human")
	if err != nil {
		t.Fatal(err)
	}

	var published []Event
	s.SetPublisher(func(_ string, e Event) { published = append(published, e) })

	boom := fmt.Errorf("deliberate")
	err = s.withWrite(func(tx *tx) error {
		if _, err := tx.emit(space.ID, "card.created", "c_x", "kai", "human", nil); err != nil {
			return err
		}
		return boom
	})
	if err != boom {
		t.Fatalf("err = %v, want the rollback to surface", err)
	}
	if len(published) != 0 {
		t.Errorf("published %d events from a rolled-back transaction", len(published))
	}

	// And the seq the doomed transaction allocated goes back with it.
	after, err := s.Space("demo")
	if err != nil {
		t.Fatal(err)
	}
	if after.Seq != space.Seq {
		t.Errorf("seq = %d, want %d: a rollback must not burn a seq", after.Seq, space.Seq)
	}
}

func TestEventsPublishOnlyAfterCommit(t *testing.T) {
	s := newTestStore(t)
	space, err := s.CreateSpace("demo", "Demo", "", "kai", "human")
	if err != nil {
		t.Fatal(err)
	}

	var seenSeq int64 = -1
	s.SetPublisher(func(_ string, e Event) {
		// By the time a subscriber sees an event, reading the log must find it.
		events, err := s.Events(space.ID, e.Seq-1, 10)
		if err != nil || len(events) == 0 || events[0].Seq != e.Seq {
			t.Errorf("event %d was published before it was readable", e.Seq)
		}
		seenSeq = e.Seq
	})
	if _, err := s.CreateCards("demo", []CardDraft{{Title: "A", Content: "a"}}, nil,
		"claude-code", "agent"); err != nil {
		t.Fatal(err)
	}
	if seenSeq < 0 {
		t.Error("no event reached the publisher")
	}
}

// TestConcurrentWritersAllocateEverySeqExactlyOnce is the SQLITE_BUSY guard: Go's
// database/sql pools, and concurrent writers on SQLite would otherwise collide.
func TestConcurrentWritersAllocateEverySeqExactlyOnce(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateSpace("demo", "Demo", "", "kai", "human"); err != nil {
		t.Fatal(err)
	}

	const writers = 8
	const each = 5
	var wg sync.WaitGroup
	errs := make(chan error, writers*each)
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				_, err := s.CreateCards("demo",
					[]CardDraft{{Title: fmt.Sprintf("w%d-%d", w, i), Content: "c"}},
					nil, "claude-code", "agent")
				if err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("a concurrent write failed: %v", err)
	}

	space, err := s.Space("demo")
	if err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(space.ID, 0, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	// Per-space monotonic, no gaps and no repeats.
	if int64(len(events)) != space.Seq {
		t.Fatalf("%d events for seq %d", len(events), space.Seq)
	}
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Fatalf("events[%d].seq = %d, want %d", i, e.Seq, i+1)
		}
	}
}

// --- layout ----------------------------------------------------------------------

func TestTheFirstCardInAnEmptySpaceLandsAtTheOrigin(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateSpace("demo", "Demo", "", "kai", "human"); err != nil {
		t.Fatal(err)
	}
	nodes, err := s.CreateCards("demo", []CardDraft{{Title: "A", Content: "a"}}, nil,
		"claude-code", "agent")
	if err != nil {
		t.Fatal(err)
	}
	x, _ := numberOf(nodes[0]["x"])
	y, _ := numberOf(nodes[0]["y"])
	if x != 0 || y != 0 {
		t.Errorf("first card at (%v, %v), want (0, 0)", x, y)
	}
}

func TestABatchWrapsIntoANewColumn(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateSpace("demo", "Demo", "", "kai", "human"); err != nil {
		t.Fatal(err)
	}
	drafts := make([]CardDraft, 6)
	for i := range drafts {
		drafts[i] = CardDraft{Title: fmt.Sprint(i), Content: "c"}
	}
	nodes, err := s.CreateCards("demo", drafts, nil, "claude-code", "agent")
	if err != nil {
		t.Fatal(err)
	}
	columns := map[float64]int{}
	for _, n := range nodes {
		x, _ := numberOf(n["x"])
		columns[x]++
	}
	if len(columns) < 2 {
		t.Errorf("six cards made %d column(s); a batch must wrap past %dpx",
			len(columns), LayoutMaxColumn)
	}
}

func TestDeletedCardsAreExcludedFromTheBoundingBox(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateSpace("demo", "Demo", "", "kai", "human"); err != nil {
		t.Fatal(err)
	}
	first, err := s.CreateCards("demo", []CardDraft{{Title: "A", Content: "a"}}, nil,
		"claude-code", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCard("demo", stringOf(first[0]["id"]), "kai", "human"); err != nil {
		t.Fatal(err)
	}
	next, err := s.CreateCards("demo", []CardDraft{{Title: "B", Content: "b"}}, nil,
		"claude-code", "agent")
	if err != nil {
		t.Fatal(err)
	}
	x, _ := numberOf(next[0]["x"])
	if x != 0 {
		t.Errorf("x = %v; a tombstone must not push the next card sideways", x)
	}
}

func TestDeletedLinksStayDeletedEvenWithIncludeDeleted(t *testing.T) {
	// include_deleted resurrects card tombstones; a deleted link has no wire
	// shape that says so, so it must never come back looking like a live edge.
	s := newTestStore(t)
	if _, err := s.CreateSpace("demo", "Demo", "", "kai", "human"); err != nil {
		t.Fatal(err)
	}
	cards, err := s.CreateCards("demo", []CardDraft{{Title: "A"}, {Title: "B"}}, nil,
		"claude-code", "agent")
	if err != nil {
		t.Fatal(err)
	}
	a, b := stringOf(cards[0]["id"]), stringOf(cards[1]["id"])
	edges, err := s.CreateLinks("demo",
		[]Edge{{"fromNode": a, "toNode": b, "label": "deleted"}, {"fromNode": b, "toNode": a, "label": "kept"}},
		"claude-code", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteLink("demo", stringOf(edges[0]["id"]), "kai", "human"); err != nil {
		t.Fatal(err)
	}
	deleted, kept := stringOf(edges[0]["id"]), stringOf(edges[1]["id"])
	for _, includeDeleted := range []bool{false, true} {
		canvas, err := s.Canvas("demo", includeDeleted)
		if err != nil {
			t.Fatal(err)
		}
		ids := map[string]bool{}
		for _, e := range canvas.Edges {
			ids[stringOf(e["id"])] = true
		}
		if ids[deleted] {
			t.Errorf("Canvas(include_deleted=%v) returned a deleted link; it must stay gone",
				includeDeleted)
		}
		if !ids[kept] {
			t.Errorf("Canvas(include_deleted=%v) lost the live link", includeDeleted)
		}
	}

	// But soft-deleting an endpoint is not a cascade: the edge outlives the card.
	if err := s.DeleteCard("demo", a, "kai", "human"); err != nil {
		t.Fatal(err)
	}
	canvas, err := s.Canvas("demo", true)
	if err != nil {
		t.Fatal(err)
	}
	if len(canvas.Edges) != 1 || stringOf(canvas.Edges[0]["id"]) != kept {
		t.Errorf("Canvas(include_deleted=true) kept %v after deleting an endpoint; want [%s]",
			canvas.Edges, kept)
	}
}

// --- blob fidelity -----------------------------------------------------------------

// TestNumbersSurviveTheBlobColumns is Go-specific: decoding through float64 would
// turn an integer coordinate into 320.0 and lose precision on a large sp_meta value.
func TestNumbersSurviveTheBlobColumns(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateSpace("demo", "Demo", "", "kai", "human"); err != nil {
		t.Fatal(err)
	}
	raw := Node{
		"type": "text", "text": "x", "x": json.Number("0"), "y": json.Number("0"),
		"width": json.Number("320"), "height": json.Number("200"),
		"sp_meta": map[string]any{
			"big":   json.Number("9007199254740993"), // 2^53 + 1
			"ratio": json.Number("0.30000000000000004"),
		},
	}
	if _, err := s.CreateCards("demo", nil, []Node{raw}, "claude-code", "agent"); err != nil {
		t.Fatal(err)
	}
	canvas, err := s.Canvas("demo", false)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(canvas.Nodes[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"x":0`, `"width":320`, `"big":9007199254740993`,
		`"ratio":0.30000000000000004`} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("stored node lost %s\ngot %s", want, encoded)
		}
	}
}

// --- summary grammar -----------------------------------------------------------------

func TestSummarizeGrammar(t *testing.T) {
	row := []map[string]any{{"id": "x"}}
	two := []map[string]any{{"id": "x"}, {"id": "y"}}
	for _, tc := range []struct {
		name string
		in   Feedback
		want string
	}{
		{"nothing", Feedback{}, ""},
		{"one comment", Feedback{Annotations: []Annotation{{}}}, "1 open comment."},
		{"two comments, one stale",
			Feedback{Annotations: []Annotation{{Stale: true}, {}}},
			"2 open comments (1 stale)."},
		{"one reply", Feedback{Replies: row}, "1 reply on resolve."},
		{"two replies", Feedback{Replies: two}, "2 replies on resolve."},
		{"one edit", Feedback{CardsEdited: row}, "1 card edited."},
		{"two edits", Feedback{CardsEdited: two}, "2 cards edited."},
		{"deleted and moved", Feedback{CardsDeleted: row, CardsMoved: two},
			"1 deleted, 2 moved."},
		{"links", Feedback{LinksAdded: row, LinksRemoved: two},
			"1 new link, 2 links removed."},
		{"the fixture's sentence",
			Feedback{Annotations: []Annotation{{Stale: true}, {}}, CardsEdited: row,
				CardsDeleted: row, CardsMoved: row, LinksAdded: row},
			"2 open comments (1 stale), 1 card edited, 1 deleted, 1 moved, 1 new link."},
		{"the human fixture's sentence",
			Feedback{Annotations: []Annotation{{Stale: true}, {}}, Replies: row,
				CardsEdited: row, LinksAdded: two},
			"2 open comments (1 stale), 1 reply on resolve, 1 card edited, 2 new links."},
	} {
		if got := Summarize(tc.in); got != tc.want {
			t.Errorf("%s: Summarize = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// --- replies on resolve --------------------------------------------------------------

func TestFeedbackDeliversRepliesOnResolve(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateSpace("demo", "Demo", "", "kai", "human"); err != nil {
		t.Fatal(err)
	}
	cards, err := s.CreateCards("demo", []CardDraft{{Title: "Option A"}}, nil,
		"claude-code", "agent")
	if err != nil {
		t.Fatal(err)
	}
	card := stringOf(cards[0]["id"])

	ann, err := s.CreateAnnotation("demo", card, "fix the axis", nil,
		"editing", "kai", "human")
	if err != nil {
		t.Fatal(err)
	}
	reply := "rebased axis at 0"
	if _, err := s.ResolveAnnotation("demo", ann.ID, true, &reply,
		"claude-code", "agent"); err != nil {
		t.Fatal(err)
	}

	zero := int64(0)
	fb, err := s.Feedback("demo", "kai", &zero, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fb.Replies) != 1 {
		t.Fatalf("Feedback.Replies = %v, want one entry", fb.Replies)
	}
	entry := fb.Replies[0]
	if stringOf(entry["id"]) != ann.ID || stringOf(entry["card_id"]) != card ||
		stringOf(entry["card_title"]) != "Option A" ||
		stringOf(entry["body"]) != "fix the axis" ||
		stringOf(entry["motivation"]) != "editing" ||
		stringOf(entry["creator"]) != "kai" ||
		stringOf(entry["creator_kind"]) != "human" ||
		stringOf(entry["reply"]) != "rebased axis at 0" ||
		stringOf(entry["actor"]) != "claude-code" {
		t.Errorf("reply entry = %v, want the resolved comment with its answer", entry)
	}
	if stringOf(entry["resolved_at"]) == "" {
		t.Error("reply entry is missing resolved_at")
	}

	// Delivered exactly once: consuming the cursor empties the bucket.
	if _, err := s.Feedback("demo", "kai", nil, true); err != nil {
		t.Fatal(err)
	}
	fb, err = s.Feedback("demo", "kai", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fb.Replies) != 0 {
		t.Errorf("replies survived the cursor: %v", fb.Replies)
	}

	// Nobody reads their own reply back.
	fb, err = s.Feedback("demo", "claude-code", &zero, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fb.Replies) != 0 {
		t.Errorf("claude-code read its own resolve back: %v", fb.Replies)
	}

	// A resolve without a reply is the acknowledgment itself, not a message.
	ann2, err := s.CreateAnnotation("demo", card, "also this", nil,
		"commenting", "kai", "human")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveAnnotation("demo", ann2.ID, true, nil,
		"claude-code", "agent"); err != nil {
		t.Fatal(err)
	}
	// ...and so is an explicitly empty one.
	empty := ""
	if _, err := s.ResolveAnnotation("demo", ann2.ID, true, &empty,
		"claude-code", "agent"); err != nil {
		t.Fatal(err)
	}

	// Reopen is silent; resolving again with a new reply is a second message.
	if _, err := s.ResolveAnnotation("demo", ann.ID, false, nil,
		"kai", "human"); err != nil {
		t.Fatal(err)
	}
	again := "and moved the legend"
	if _, err := s.ResolveAnnotation("demo", ann.ID, true, &again,
		"claude-code", "agent"); err != nil {
		t.Fatal(err)
	}

	fb, err = s.Feedback("demo", "kai", &zero, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(fb.Replies) != 2 {
		t.Fatalf("replays = %v, want the two answered resolves", fb.Replies)
	}
	if stringOf(fb.Replies[0]["reply"]) != "rebased axis at 0" ||
		stringOf(fb.Replies[1]["reply"]) != "and moved the legend" {
		t.Errorf("replies are not in event order: %v", fb.Replies)
	}
}
