package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/meowkey-dev/analog/client"
)

// The Go rewrite of tests/unit/test_mcp.py: all ten tools callable against a mock,
// and get_feedback returning the §4.1 shape.

var specTools = []string{
	"add_cards", "await_feedback", "create_space", "delete_card", "get_feedback",
	"link_cards", "list_spaces", "read_space", "resolve_annotation", "update_card",
}

// --- the mock ------------------------------------------------------------------

type call struct {
	Name string
	Args []any
}

type mockAPI struct {
	t     *testing.T
	calls []call
	// feedbackQueue gives successive states; the last one repeats forever.
	feedbackQueue []client.Feedback
	canvasErr     error
}

func (m *mockAPI) record(name string, args ...any) { m.calls = append(m.calls, call{name, args}) }

func (m *mockAPI) names() []string {
	out := make([]string, 0, len(m.calls))
	for _, c := range m.calls {
		out = append(out, c.Name)
	}
	return out
}

func (m *mockAPI) last() call { return m.calls[len(m.calls)-1] }

func (m *mockAPI) ListSpaces() ([]client.Space, error) {
	m.record("ListSpaces")
	return []client.Space{fixtureSpace(m.t)}, nil
}

func (m *mockAPI) CreateSpace(slug, title, revisionMode string) (client.Space, error) {
	m.record("CreateSpace", slug, title, revisionMode)
	space := fixtureSpace(m.t)
	space.Slug, space.Title, space.RevisionMode = slug, title, revisionMode
	return space, nil
}

func (m *mockAPI) GetSpace(slug string) (client.Space, error) {
	m.record("GetSpace", slug)
	return fixtureSpace(m.t), nil
}

func (m *mockAPI) GetCanvas(slug string, includeDeleted bool) (client.Canvas, error) {
	m.record("GetCanvas", slug, includeDeleted)
	if m.canvasErr != nil {
		return client.Canvas{}, m.canvasErr
	}
	return fixtureCanvas(m.t), nil
}

func (m *mockAPI) ListAnnotations(slug string, resolved *bool, cardID string) ([]client.Annotation, error) {
	m.record("ListAnnotations", slug, resolved, cardID)
	var out []client.Annotation
	for _, a := range fixtureAnnotations(m.t) {
		if resolved == nil || a.Resolved == *resolved {
			out = append(out, a)
		}
	}
	return out, nil
}

func (m *mockAPI) CreateCards(slug string, cards []client.CardDraft) ([]client.Node, error) {
	m.record("CreateCards", slug, cards)
	base := fixtureCanvas(m.t).Nodes[0]
	out := make([]client.Node, 0, len(cards))
	for _, c := range cards {
		node := client.Node{}
		for k, v := range base {
			node[k] = v
		}
		node["sp_title"], node["text"], node["sp_kind"] = c.Title, c.Content, c.Kind
		out = append(out, node)
	}
	return out, nil
}

func (m *mockAPI) UpdateCard(slug, cardID string, patch map[string]any, mode string,
	ifMatch *int64) (client.Node, error) {
	m.record("UpdateCard", slug, cardID, patch, mode, ifMatch)
	node := client.Node{}
	for k, v := range fixtureCanvas(m.t).Nodes[0] {
		node[k] = v
	}
	for k, v := range patch {
		node[k] = v
	}
	node["sp_rev"] = 3
	return node, nil
}

func (m *mockAPI) DeleteCard(slug, cardID string) error {
	m.record("DeleteCard", slug, cardID)
	return nil
}

func (m *mockAPI) LinkCards(slug, fromID, toID, label string) (client.Edge, error) {
	m.record("LinkCards", slug, fromID, toID, label)
	return client.Edge{"id": "l_9", "fromNode": fromID, "toNode": toID, "label": label}, nil
}

func (m *mockAPI) GetFeedback(slug string, since *int64, advance bool) (client.Feedback, error) {
	m.record("GetFeedback", slug, since, advance)
	if len(m.feedbackQueue) > 0 {
		if len(m.feedbackQueue) > 1 {
			next := m.feedbackQueue[0]
			m.feedbackQueue = m.feedbackQueue[1:]
			return next, nil
		}
		return m.feedbackQueue[0], nil
	}
	return fixtureFeedback(m.t), nil
}

func (m *mockAPI) ResolveAnnotation(slug, annotationID string, reply *string,
	resolved bool) (client.Annotation, error) {
	m.record("ResolveAnnotation", slug, annotationID, reply)
	a := fixtureAnnotations(m.t)[0]
	a.ID, a.Resolved = annotationID, true
	if reply != nil {
		a.ResolvedReply = *reply
	}
	return a, nil
}

func (m *mockAPI) FindAnnotation(annotationID string) (string, client.Annotation, error) {
	m.record("FindAnnotation", annotationID)
	return "redesign", fixtureAnnotations(m.t)[0], nil
}

// --- fixtures ------------------------------------------------------------------

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find the repository root")
	return ""
}

func loadFixture[T any](t *testing.T, name string) T {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "contracts", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	var out T
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func fixtureSpace(t *testing.T) client.Space { return loadFixture[client.Space](t, "space.json") }
func fixtureCanvas(t *testing.T) client.Canvas {
	return loadFixture[client.Canvas](t, "canvas.json")
}
func fixtureAnnotations(t *testing.T) []client.Annotation {
	return loadFixture[[]client.Annotation](t, "annotations.json")
}
func fixtureFeedback(t *testing.T) client.Feedback {
	return loadFixture[client.Feedback](t, "feedback.claude-code.since-12.json")
}

func newTestServer(t *testing.T) (*Server, *mockAPI) {
	t.Helper()
	mock := &mockAPI{t: t}
	server := New(mock)
	server.sleep = func(time.Duration) {}
	return server, mock
}

// --- inventory -------------------------------------------------------------------

func TestExactlyTheTenToolsInSpec41(t *testing.T) {
	server, _ := newTestServer(t)
	var names []string
	for _, tool := range server.Tools() {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	if !reflect.DeepEqual(names, specTools) {
		t.Errorf("tools = %v, want %v", names, specTools)
	}
}

func TestEveryToolHasADescriptionAndASchema(t *testing.T) {
	server, _ := newTestServer(t)
	for _, tool := range server.Tools() {
		if strings.TrimSpace(tool.Description) == "" {
			t.Errorf("%s has no description", tool.Name)
		}
		if tool.InputSchema == nil || tool.InputSchema["type"] != "object" {
			t.Errorf("%s has no object input schema", tool.Name)
		}
	}
}

// --- each tool -------------------------------------------------------------------

func TestListSpaces(t *testing.T) {
	server, _ := newTestServer(t)
	result, err := server.Call("list_spaces", nil)
	if err != nil {
		t.Fatal(err)
	}
	spaces, ok := result.([]client.Space)
	if !ok || len(spaces) != 1 || spaces[0].Slug != "redesign" {
		t.Errorf("list_spaces = %#v", result)
	}
}

func TestCreateSpace(t *testing.T) {
	server, mock := newTestServer(t)
	result, err := server.Call("create_space", map[string]any{"slug": "demo", "title": "Demo"})
	if err != nil {
		t.Fatal(err)
	}
	if result.(client.Space).Slug != "demo" {
		t.Errorf("slug = %v", result)
	}
	if got := mock.last().Args; !reflect.DeepEqual(got, []any{"demo", "Demo", "replace"}) {
		t.Errorf("args = %v, want [demo Demo replace]", got)
	}
}

func TestCreateSpaceAcceptsBranch(t *testing.T) {
	server, mock := newTestServer(t)
	if _, err := server.Call("create_space", map[string]any{
		"slug": "demo", "title": "D", "revision_mode": "branch"}); err != nil {
		t.Fatal(err)
	}
	if mock.last().Args[2] != "branch" {
		t.Errorf("revision_mode = %v", mock.last().Args[2])
	}
}

func TestReadSpaceReturnsNodesEdgesAndOpenAnnotations(t *testing.T) {
	server, _ := newTestServer(t)
	result, err := server.Call("read_space", map[string]any{"slug": "redesign"})
	if err != nil {
		t.Fatal(err)
	}
	body := result.(map[string]any)
	for _, key := range []string{"space", "nodes", "edges", "annotations"} {
		if _, ok := body[key]; !ok {
			t.Errorf("read_space is missing %q", key)
		}
	}
	for _, a := range body["annotations"].([]client.Annotation) {
		if a.Resolved {
			t.Error("read_space must only carry open annotations")
		}
	}
}

func TestAddCardsTakesFriendlyDrafts(t *testing.T) {
	server, mock := newTestServer(t)
	_, err := server.Call("add_cards", map[string]any{
		"slug": "redesign",
		"cards": []any{map[string]any{
			"title": "Option E", "content": "lazy load", "kind": "md"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	drafts := mock.last().Args[1].([]client.CardDraft)
	if drafts[0].Title != "Option E" || drafts[0].Content != "lazy load" {
		t.Errorf("draft = %+v", drafts[0])
	}
	if drafts[0].X != nil || drafts[0].Y != nil {
		t.Error("omitted geometry must stay omitted, so the server places the card")
	}
}

func TestAddCardsPassesGeometryWhenGiven(t *testing.T) {
	server, mock := newTestServer(t)
	_, err := server.Call("add_cards", map[string]any{
		"slug":  "redesign",
		"cards": []any{map[string]any{"title": "T", "content": "c", "x": 40.0, "y": 0.0}},
	})
	if err != nil {
		t.Fatal(err)
	}
	draft := mock.last().Args[1].([]client.CardDraft)[0]
	if draft.X == nil || *draft.X != 40 {
		t.Errorf("x = %v, want 40", draft.X)
	}
	// An explicit zero is a coordinate, not an absence.
	if draft.Y == nil || *draft.Y != 0 {
		t.Errorf("y = %v, want an explicit 0", draft.Y)
	}
}

func TestUpdateCard(t *testing.T) {
	server, mock := newTestServer(t)
	result, err := server.Call("update_card", map[string]any{
		"slug": "redesign", "card_id": "c_opt_a",
		"patch": map[string]any{"text": "v2"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.(client.Node)["text"] != "v2" {
		t.Errorf("node = %v", result)
	}
	if mock.last().Args[1] != "c_opt_a" {
		t.Errorf("card_id = %v", mock.last().Args[1])
	}
}

func TestUpdateCardForwardsModeAndIfMatch(t *testing.T) {
	server, mock := newTestServer(t)
	if _, err := server.Call("update_card", map[string]any{
		"slug": "redesign", "card_id": "c_opt_a", "patch": map[string]any{"text": "v2"},
		"mode": "branch", "if_match": json.Number("3")}); err != nil {
		t.Fatal(err)
	}
	args := mock.last().Args
	if args[3] != "branch" {
		t.Errorf("mode = %v", args[3])
	}
	ifMatch := args[4].(*int64)
	if ifMatch == nil || *ifMatch != 3 {
		t.Errorf("if_match = %v", args[4])
	}
}

func TestDeleteCard(t *testing.T) {
	server, mock := newTestServer(t)
	result, err := server.Call("delete_card", map[string]any{
		"slug": "redesign", "card_id": "c_opt_a"})
	if err != nil {
		t.Fatal(err)
	}
	if result.(map[string]any)["deleted"] != "c_opt_a" {
		t.Errorf("result = %v", result)
	}
	if mock.last().Name != "DeleteCard" {
		t.Errorf("calls = %v", mock.names())
	}
}

func TestLinkCards(t *testing.T) {
	server, mock := newTestServer(t)
	result, err := server.Call("link_cards", map[string]any{
		"slug": "redesign", "from_card": "c_opt_b", "to_card": "c_opt_d",
		"label": "contradicts"})
	if err != nil {
		t.Fatal(err)
	}
	if result.(client.Edge)["label"] != "contradicts" {
		t.Errorf("edge = %v", result)
	}
	want := []any{"redesign", "c_opt_b", "c_opt_d", "contradicts"}
	if !reflect.DeepEqual(mock.last().Args, want) {
		t.Errorf("args = %v, want %v", mock.last().Args, want)
	}
}

func TestResolveAnnotationWithoutASlugLooksItUp(t *testing.T) {
	server, mock := newTestServer(t)
	result, err := server.Call("resolve_annotation", map[string]any{
		"annotation_id": "a_1", "reply": "rebased axis at 0"})
	if err != nil {
		t.Fatal(err)
	}
	a := result.(client.Annotation)
	if !a.Resolved || a.ResolvedReply != "rebased axis at 0" {
		t.Errorf("annotation = %+v", a)
	}
	want := []string{"FindAnnotation", "ResolveAnnotation"}
	if !reflect.DeepEqual(mock.names(), want) {
		t.Errorf("calls = %v, want %v", mock.names(), want)
	}
}

func TestResolveAnnotationWithASlugSkipsTheLookup(t *testing.T) {
	server, mock := newTestServer(t)
	if _, err := server.Call("resolve_annotation", map[string]any{
		"annotation_id": "a_1", "slug": "redesign"}); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(mock.names(), []string{"ResolveAnnotation"}) {
		t.Errorf("calls = %v, want just ResolveAnnotation", mock.names())
	}
}

// --- get_feedback (the contract) ----------------------------------------------------

func TestGetFeedbackReturnsTheSpec41Shape(t *testing.T) {
	server, _ := newTestServer(t)
	result, err := server.Call("get_feedback", map[string]any{"slug": "redesign"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.(client.Feedback), fixtureFeedback(t)) {
		t.Error("get_feedback did not return the fixture unchanged")
	}
}

func TestGetFeedbackDefaultsToTheServerSideCursor(t *testing.T) {
	// SPEC §10: agents stay stateless; a fresh session has nowhere to keep since=58.
	server, mock := newTestServer(t)
	if _, err := server.Call("get_feedback", map[string]any{"slug": "redesign"}); err != nil {
		t.Fatal(err)
	}
	args := mock.last().Args
	if args[1] != (*int64)(nil) {
		t.Errorf("since = %v, want nil so the server's cursor decides", args[1])
	}
	if args[2] != true {
		t.Errorf("advance = %v, want true", args[2])
	}
}

func TestGetFeedbackAcceptsAnExplicitSinceForReplay(t *testing.T) {
	server, mock := newTestServer(t)
	if _, err := server.Call("get_feedback", map[string]any{
		"slug": "redesign", "since": json.Number("12")}); err != nil {
		t.Fatal(err)
	}
	since := mock.last().Args[1].(*int64)
	if since == nil || *since != 12 {
		t.Errorf("since = %v, want 12", mock.last().Args[1])
	}
}

// --- await_feedback --------------------------------------------------------------------

func emptyFeedback() client.Feedback {
	return client.Feedback{Cursor: 19, Annotations: []client.Annotation{},
		CardsEdited: []map[string]any{}, CardsDeleted: []map[string]any{},
		CardsMoved: []map[string]any{}, LinksAdded: []map[string]any{},
		LinksRemoved: []map[string]any{}, Summary: ""}
}

func TestAwaitFeedbackReturnsAsSoonAsSomethingArrives(t *testing.T) {
	server, mock := newTestServer(t)
	mock.feedbackQueue = []client.Feedback{emptyFeedback(), fixtureFeedback(t), fixtureFeedback(t)}
	result, err := server.Call("await_feedback", map[string]any{
		"slug": "redesign", "timeout_s": 5.0, "poll_s": 0.01})
	if err != nil {
		t.Fatal(err)
	}
	if result.(client.Feedback).Summary != fixtureFeedback(t).Summary {
		t.Errorf("summary = %q", result.(client.Feedback).Summary)
	}
}

func TestAwaitFeedbackPeeksBeforeItConsumes(t *testing.T) {
	// A poll that finds nothing must never advance the cursor.
	server, mock := newTestServer(t)
	mock.feedbackQueue = []client.Feedback{emptyFeedback()}
	if _, err := server.Call("await_feedback", map[string]any{
		"slug": "redesign", "timeout_s": 0.05, "poll_s": 0.01}); err != nil {
		t.Fatal(err)
	}
	polls := 0
	for _, c := range mock.calls {
		if c.Name != "GetFeedback" {
			continue
		}
		polls++
		if c.Args[2] != false {
			t.Error("a poll advanced the cursor")
		}
	}
	if polls == 0 {
		t.Error("it must have polled at least once")
	}
}

func TestAwaitFeedbackTimesOutQuietly(t *testing.T) {
	server, mock := newTestServer(t)
	mock.feedbackQueue = []client.Feedback{emptyFeedback()}
	result, err := server.Call("await_feedback", map[string]any{
		"slug": "redesign", "timeout_s": 0.05, "poll_s": 0.01})
	if err != nil {
		t.Fatal(err)
	}
	f := result.(client.Feedback)
	if f.Summary != "" {
		t.Errorf("summary = %q, want empty on a timeout", f.Summary)
	}
	if f.Annotations == nil || f.CardsEdited == nil {
		t.Error("a timeout still returns the full Feedback shape")
	}
}

// --- errors ----------------------------------------------------------------------------

func TestAClientErrorReachesTheAgentAsAReadableMessage(t *testing.T) {
	server, mock := newTestServer(t)
	mock.canvasErr = &client.Error{Status: 404, Code: client.CodeNotFound,
		Message: "no space 'nope'"}
	_, err := server.Call("read_space", map[string]any{"slug": "nope"})
	if err == nil {
		t.Fatal("the failure was swallowed")
	}
	if !strings.Contains(err.Error(), "no space 'nope'") {
		t.Errorf("err = %v, want it to carry the server's message", err)
	}
}

func TestAnUnknownToolIsRejected(t *testing.T) {
	server, _ := newTestServer(t)
	if _, err := server.Call("teleport", nil); err == nil {
		t.Error("an unknown tool must not succeed")
	}
}

// TestAwaitFeedbackIgnoresAnAlreadyOpenAnnotation is the regression for #19.
//
// Unresolved annotations come back regardless of the cursor, so waking on a
// non-empty `summary` meant one open comment made this return instantly forever —
// a busy loop for exactly the resident agents the tool exists for.
func TestAwaitFeedbackIgnoresAnAlreadyOpenAnnotation(t *testing.T) {
	server, mock := newTestServer(t)
	standing := emptyFeedback()
	standing.Annotations = []client.Annotation{{ID: "a_1", Body: "open since before"}}
	standing.Summary = "1 open comment." // non-empty, and must not be enough
	mock.feedbackQueue = []client.Feedback{standing}

	result, err := server.Call("await_feedback", map[string]any{
		"slug": "redesign", "timeout_s": 0.05, "poll_s": 0.01})
	if err != nil {
		t.Fatal(err)
	}
	if result.(client.Feedback).Summary != standing.Summary {
		t.Fatalf("expected the timeout to return the peek, got %+v", result)
	}
	for _, c := range mock.calls {
		if c.Name == "GetFeedback" && c.Args[2] == true {
			t.Fatal("it consumed the cursor for an annotation that was already open")
		}
	}
	if len(mock.calls) < 2 {
		t.Errorf("it returned after %d call(s); it should have polled and waited",
			len(mock.calls))
	}
}

// TestAwaitFeedbackWakesOnANewAnnotation is the other half: one that arrives while
// waiting is exactly what it should wake on.
func TestAwaitFeedbackWakesOnANewAnnotation(t *testing.T) {
	server, mock := newTestServer(t)
	standing := emptyFeedback()
	standing.Annotations = []client.Annotation{{ID: "a_1"}}
	standing.Summary = "1 open comment."
	arrived := emptyFeedback()
	arrived.Annotations = []client.Annotation{{ID: "a_1"}, {ID: "a_2"}}
	arrived.Summary = "2 open comments."
	mock.feedbackQueue = []client.Feedback{standing, arrived, arrived}

	if _, err := server.Call("await_feedback", map[string]any{
		"slug": "redesign", "timeout_s": 5, "poll_s": 0.01}); err != nil {
		t.Fatal(err)
	}
	consumed := false
	for _, c := range mock.calls {
		if c.Name == "GetFeedback" && c.Args[2] == true {
			consumed = true
		}
	}
	if !consumed {
		t.Error("a new annotation must wake it, and consuming is how it says so")
	}
}
