// WP1: 'every mutation emits exactly one event'.
//
// The event log is the only reason attribution and deltas work, so it gets its own
// file rather than being checked incidentally elsewhere.
package conformance

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func eventsSpace(t *testing.T) *server {
	t.Helper()
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	return s
}

// --- exactly one event per mutation -----------------------------------------

func TestEvents_EachMutationEmitsExactlyOneEvent(t *testing.T) {
	s := eventsSpace(t)
	base := len(eventsOf(t, s, "demo", "0"))
	if base != 1 {
		t.Fatalf("base = %d, want 1: the space's own creation", base)
	}

	card := oneCard(t, s, "demo", "title", `"A"`)
	if got := len(eventsOf(t, s, "demo", "0")); got != base+1 {
		t.Fatalf("after card: %d", got)
	}
	other := oneCard(t, s, "demo", "title", `"B"`)
	if got := len(eventsOf(t, s, "demo", "0")); got != base+2 {
		t.Fatalf("after second card: %d", got)
	}
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(), map[string]any{"x": 5})
	if got := len(eventsOf(t, s, "demo", "0")); got != base+3 {
		t.Fatalf("after move: %d", got)
	}
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(), map[string]any{"text": "v2"})
	if got := len(eventsOf(t, s, "demo", "0")); got != base+4 {
		t.Fatalf("after edit: %d", got)
	}
	link := asMap(s.post(t, "/api/spaces/demo/links", agentP(), map[string]any{
		"edges": []any{map[string]any{
			"fromNode": card["id"], "toNode": other["id"]}}}).arr()[0])
	if got := len(eventsOf(t, s, "demo", "0")); got != base+5 {
		t.Fatalf("after link: %d", got)
	}
	ann := asMap(s.post(t, "/api/spaces/demo/annotations", humanP(), map[string]any{
		"card_id": card["id"], "body": "b"}).body)
	if got := len(eventsOf(t, s, "demo", "0")); got != base+6 {
		t.Fatalf("after annotation: %d", got)
	}
	s.patch(t, "/api/spaces/demo/annotations/"+asStr(ann["id"]), agentP(),
		map[string]any{"resolved": true})
	if got := len(eventsOf(t, s, "demo", "0")); got != base+7 {
		t.Fatalf("after resolve: %d", got)
	}
	s.delete(t, "/api/spaces/demo/links/"+asStr(link["id"]), humanP())
	if got := len(eventsOf(t, s, "demo", "0")); got != base+8 {
		t.Fatalf("after unlink: %d", got)
	}
	s.delete(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP())
	if got := len(eventsOf(t, s, "demo", "0")); got != base+9 {
		t.Fatalf("after delete: %d", got)
	}

	var types []string
	for _, e := range eventsOf(t, s, "demo", "0") {
		types = append(types, asStr(asMap(e)["type"]))
	}
	want := []string{"space.created",
		"card.created", "card.created", "card.moved", "card.updated", "link.created",
		"annotation.created", "annotation.resolved", "link.deleted", "card.deleted"}
	if !equalStrings(types, want...) {
		t.Errorf("event types = %v\n want = %v", types, want)
	}
}

func TestEvents_BulkCreateEmitsOneEventPerItem(t *testing.T) {
	s := eventsSpace(t)
	cards := make([]any, 0, 4)
	for _, title := range []string{"A", "B", "C", "D"} {
		cards = append(cards, map[string]any{"title": title, "content": title})
	}
	addCards(t, s, "demo", cards, nil)
	created := 0
	for _, e := range eventsOf(t, s, "demo", "0") {
		if asStr(asMap(e)["type"]) == "card.created" {
			created++
		}
	}
	if created != 4 {
		t.Errorf("%d card.created events, want 4", created)
	}
}

func TestEvents_AFailedMutationEmitsNothing(t *testing.T) {
	s := eventsSpace(t)
	card := oneCard(t, s, "demo")
	before := len(eventsOf(t, s, "demo", "0"))
	s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
		map[string]any{"text": "x"}, map[string]string{"If-Match": "99"})
	s.patch(t, "/api/spaces/demo/cards/c_nope", humanP(), map[string]any{"text": "x"})
	s.post(t, "/api/spaces/demo/cards", params("actor", "x"),
		map[string]any{"cards": []any{map[string]any{"title": "T", "content": "c"}}})
	if got := len(eventsOf(t, s, "demo", "0")); got != before {
		t.Errorf("failed mutations emitted %d events", got-before)
	}
}

// --- shape -------------------------------------------------------------------

func TestEvents_EventsValidateAndCarryAttribution(t *testing.T) {
	s := eventsSpace(t)
	oneCard(t, s, "demo")
	var ev map[string]any
	for _, e := range eventsOf(t, s, "demo", "0") {
		if asStr(asMap(e)["type"]) == "card.created" {
			ev = asMap(e)
		}
	}
	assertValid(t, ev, "Event", false)
	if asStr(ev["actor"]) != "claude-code" || asStr(ev["actor_kind"]) != "agent" {
		t.Errorf("attribution = %s", canonical(ev))
	}
	if !strings.HasSuffix(asStr(ev["ts"]), "Z") {
		t.Errorf("ts = %q", ev["ts"])
	}
}

func TestEvents_SeqStartsAtOneAndIsContiguous(t *testing.T) {
	s := eventsSpace(t)
	cards := make([]any, 0, 3)
	for _, title := range []string{"A", "B", "C"} {
		cards = append(cards, map[string]any{"title": title, "content": title})
	}
	addCards(t, s, "demo", cards, nil)
	log := eventsOf(t, s, "demo", "0")
	for i, e := range log {
		if got := num(t, asMap(e)["seq"]); got != float64(i+1) {
			t.Fatalf("event %d seq = %v", i, got)
		}
	}
	if asStr(asMap(log[0])["type"]) != "space.created" {
		t.Errorf("first event = %q", asMap(log[0])["type"])
	}
}

func TestEvents_SubjectIdPointsAtTheThingThatChanged(t *testing.T) {
	s := eventsSpace(t)
	card := oneCard(t, s, "demo")
	ann := asMap(s.post(t, "/api/spaces/demo/annotations", humanP(), map[string]any{
		"card_id": card["id"], "body": "b"}).body)
	byType := map[string]map[string]any{}
	for _, e := range eventsOf(t, s, "demo", "0") {
		byType[asStr(asMap(e)["type"])] = asMap(e)
	}
	if asStr(byType["card.created"]["subject_id"]) != asStr(card["id"]) {
		t.Errorf("card.created subject = %v", byType["card.created"]["subject_id"])
	}
	if asStr(byType["annotation.created"]["subject_id"]) != asStr(ann["id"]) {
		t.Errorf("annotation.created subject = %v", byType["annotation.created"]["subject_id"])
	}
	if asStr(asMap(byType["annotation.created"]["payload"])["card_id"]) != asStr(card["id"]) {
		t.Errorf("annotation.created payload = %s", canonical(byType["annotation.created"]["payload"]))
	}
}

func TestEvents_CardCreatedPayloadCarriesTheTitle(t *testing.T) {
	// The activity sidebar and cards_deleted both need a title without the card.
	s := eventsSpace(t)
	oneCard(t, s, "demo", "title", `"Option D"`)
	for _, e := range eventsOf(t, s, "demo", "0") {
		if asStr(asMap(e)["type"]) == "card.created" {
			if got := asStr(asMap(asMap(e)["payload"])["title"]); got != "Option D" {
				t.Errorf("payload title = %q", got)
			}
			return
		}
	}
	t.Fatal("no card.created event")
}

func TestEvents_LinkCreatedPayloadCarriesEndpointsAndLabel(t *testing.T) {
	s := eventsSpace(t)
	nodes := addCards(t, s, "demo", []any{
		map[string]any{"title": "A", "content": "a"},
		map[string]any{"title": "B", "content": "b"}}, nil)
	a, b := asMap(nodes[0]), asMap(nodes[1])
	s.post(t, "/api/spaces/demo/links", humanP(), map[string]any{
		"edges": []any{map[string]any{
			"fromNode": a["id"], "toNode": b["id"], "label": "depends on"}}})
	log := eventsOf(t, s, "demo", "0")
	assertJSONEq(t, "payload",
		map[string]any{"from": a["id"], "to": b["id"], "label": "depends on"},
		asMap(log[len(log)-1])["payload"])
}

// --- listing -----------------------------------------------------------------

func TestEvents_SinceIsExclusive(t *testing.T) {
	s := eventsSpace(t)
	cards := make([]any, 0, 3)
	for _, title := range []string{"A", "B", "C"} {
		cards = append(cards, map[string]any{"title": title, "content": title})
	}
	addCards(t, s, "demo", cards, nil)
	var seqs []float64
	for _, e := range eventsOf(t, s, "demo", "2") {
		seqs = append(seqs, num(t, asMap(e)["seq"]))
	}
	if len(seqs) != 2 || seqs[0] != 3 || seqs[1] != 4 {
		t.Errorf("since=2 returned %v, want [3 4]", seqs)
	}
	if got := eventsOf(t, s, "demo", "4"); len(got) != 0 {
		t.Errorf("since=4 returned %v", canonical(got))
	}
}

func TestEvents_LimitAndCursor(t *testing.T) {
	s := eventsSpace(t)
	cards := make([]any, 0, 5)
	for i := 0; i < 5; i++ {
		cards = append(cards, map[string]any{"title": fmt.Sprint(i), "content": "c"})
	}
	addCards(t, s, "demo", cards, nil)
	r := s.get(t, "/api/spaces/demo/events", params("since", "0", "limit", "2")).obj()
	var seqs []float64
	for _, e := range asArr(r["events"]) {
		seqs = append(seqs, num(t, asMap(e)["seq"]))
	}
	if !equalFloats(seqs, 1, 2) {
		t.Errorf("limited events = %v, want [1 2]", seqs)
	}
	if got := num(t, r["cursor"]); got != 2 {
		t.Errorf("cursor = %v, want 2: the cursor is resumable: pass it back as since", got)
	}
	rest := s.get(t, "/api/spaces/demo/events", params("since", fmt.Sprint(r["cursor"]))).obj()
	seqs = nil
	for _, e := range asArr(rest["events"]) {
		seqs = append(seqs, num(t, asMap(e)["seq"]))
	}
	if !equalFloats(seqs, 3, 4, 5, 6) {
		t.Errorf("resumed events = %v, want [3 4 5 6]", seqs)
	}
}

func TestEvents_CursorWhenNothingIsReturned(t *testing.T) {
	s := eventsSpace(t)
	r := s.get(t, "/api/spaces/demo/events", params("since", "7")).obj()
	if len(asArr(r["events"])) != 0 {
		t.Errorf("events = %v", canonical(r["events"]))
	}
	if got := num(t, r["cursor"]); got != 7 {
		t.Errorf("cursor = %v, want 7", got)
	}
}

func TestEvents_EventsForAnUnknownSpaceIs404(t *testing.T) {
	s := startServer(t)
	if r := s.get(t, "/api/spaces/nope/events", nil); r.status != 404 {
		t.Fatalf("%d", r.status)
	}
}

// --- SSE (openapi streamEvents) ---------------------------------------------

type sseFrame struct {
	id    float64
	event string
	data  map[string]any
}

// readFrames collects `want` SSE frames from a shared stream reader. The stream
// carries a context deadline, so a server that stops pushing surfaces here as a
// short read rather than a hang.
func readFrames(t *testing.T, reader *bufio.Reader, want int) []sseFrame {
	t.Helper()
	var out []sseFrame
	current := sseFrame{}
	flush := func() {
		if current.data != nil {
			out = append(out, current)
		}
		current = sseFrame{}
	}
	for len(out) < want {
		line, err := reader.ReadString('\n')
		if err != nil {
			if len(out) < want {
				t.Fatalf("stream ended after %d frames (want %d): %v", len(out), want, err)
			}
			break
		}
		line = strings.TrimRight(line, "\r\n")
		switch {
		case strings.HasPrefix(line, "id:"):
			current.id, _ = strconv.ParseFloat(strings.TrimSpace(line[3:]), 64)
		case strings.HasPrefix(line, "event:"):
			current.event = strings.TrimSpace(line[6:])
		case strings.HasPrefix(line, "data:"):
			dec := json.NewDecoder(strings.NewReader(strings.TrimSpace(line[5:])))
			dec.UseNumber()
			var data any
			if err := dec.Decode(&data); err == nil {
				current.data = asMap(data)
			}
		case line == "":
			flush()
		}
	}
	return out
}

// openStream opens the SSE stream the way a browser would, with Last-Event-ID and
// a deadline. Returns the response and a reader the test drives frame by frame.
func openStream(t *testing.T, base, slug, lastEventID string) (*http.Response, *bufio.Reader) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	req, err := http.NewRequestWithContext(ctx, "GET",
		base+"/api/spaces/"+slug+"/events/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", lastEventID)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp, bufio.NewReader(resp.Body)
}

func TestEvents_StreamReplaysTheBacklogThenPushesLiveEvents(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "D", "")
	card := asMap(addCards(t, s, "demo",
		[]any{map[string]any{"title": "A", "content": "a"}}, nil)[0])

	resp, reader := openStream(t, s.base, "demo", "0")
	if resp.StatusCode != 200 {
		t.Fatalf("stream status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("content-type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("cache-control = %q", cc)
	}

	backlog := readFrames(t, reader, 2)
	if len(backlog) != 2 ||
		backlog[0].event != "space.created" || backlog[1].event != "card.created" {
		t.Fatalf("backlog events = %+v", backlog)
	}
	if backlog[0].id != 1 || backlog[1].id != 2 {
		t.Errorf("backlog ids = [%v %v], want [1 2]", backlog[0].id, backlog[1].id)
	}
	if asStr(backlog[1].data["subject_id"]) != asStr(card["id"]) {
		t.Errorf("card.created subject = %v", backlog[1].data["subject_id"])
	}

	var once sync.Once
	go func() {
		once.Do(func() {
			time.Sleep(300 * time.Millisecond)
			s.patch(t, "/api/spaces/demo/cards/"+asStr(card["id"]), humanP(),
				map[string]any{"text": "v2"})
		})
	}()
	live := readFrames(t, reader, 1)
	if len(live) != 1 || live[0].event != "card.updated" {
		t.Fatalf("live events = %+v", live)
	}
	if live[0].id != 3 {
		t.Errorf("live id = %v, want 3", live[0].id)
	}
	if asStr(live[0].data["actor"]) != "human" {
		t.Errorf("live actor = %v", live[0].data["actor"])
	}
}

func TestEvents_StreamResumesFromLastEventID(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "D", "")
	cards := make([]any, 0, 3)
	for _, title := range []string{"A", "B", "C"} {
		cards = append(cards, map[string]any{"title": title, "content": title})
	}
	addCards(t, s, "demo", cards, nil)

	_, reader := openStream(t, s.base, "demo", "3")
	frames := readFrames(t, reader, 1)
	if len(frames) != 1 || frames[0].id != 4 {
		t.Fatalf("resumed frames = %+v, want first id 4: events 1-3 were already delivered", frames)
	}
}

func equalFloats(got []float64, want ...float64) bool {
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
