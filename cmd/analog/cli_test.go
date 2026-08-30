package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/meowkey-dev/analog/client"
)

// The Go rewrite of tests/unit/test_cli.py and test_cli_auth.py. The CLI runs as a
// real process against a real server, so these check the whole path from argv to
// the event log — which is what the Python tests did, and the only way an exit code
// is worth asserting.

var binaries struct {
	cli    string
	server string
}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "analog-cli-test")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	binaries.cli = filepath.Join(dir, "analog")
	binaries.server = filepath.Join(dir, "analog-server")
	for target, pkg := range map[string]string{
		binaries.cli:    "github.com/meowkey-dev/analog/cmd/analog",
		binaries.server: "github.com/meowkey-dev/analog/cmd/analog-server",
	} {
		build := exec.Command("go", "build", "-o", target, pkg)
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "building", pkg, err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

// --- the harness -------------------------------------------------------------------

type harness struct {
	t       *testing.T
	url     string
	dataDir string
	env     []string
}

type result struct {
	stdout string
	stderr string
	code   int
}

func freePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	port := freePort(t)
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + dir,
		"ANALOG_DATA_DIR=" + dir,
		"ANALOG_DB=" + filepath.Join(dir, "analog.db"),
		"ANALOG_AUTH_FILE=" + filepath.Join(dir, "auth.json"),
		// Never read the developer's own ~/.analog.toml.
		"ANALOG_CONFIG=" + filepath.Join(dir, "no-such-config.toml"),
	}

	server := exec.Command(binaries.server, "--port", fmt.Sprint(port))
	server.Env = env
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = server.Process.Kill()
		_, _ = server.Process.Wait()
	})

	h := &harness{t: t, url: fmt.Sprintf("http://127.0.0.1:%d", port), dataDir: dir, env: env}
	h.awaitHealth()
	return h
}

func (h *harness) awaitHealth() {
	h.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := http.Get(h.url + "/api/health")
		if err == nil {
			response.Body.Close()
			if response.StatusCode == 200 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatal("the server never answered /api/health")
}

// options tweaks one invocation.
type options struct {
	actor string
	kind  string
	space string
	token string
	stdin string
}

func (h *harness) invoke(opts options, args ...string) result {
	h.t.Helper()
	if opts.actor == "" {
		opts.actor = "claude-code"
	}
	if opts.kind == "" {
		opts.kind = "agent"
	}
	cmd := exec.Command(binaries.cli, args...)
	cmd.Env = append(append([]string{}, h.env...),
		"ANALOG_URL="+h.url, "ANALOG_ACTOR="+opts.actor, "ANALOG_ACTOR_KIND="+opts.kind)
	if opts.space != "" {
		cmd.Env = append(cmd.Env, "ANALOG_SPACE="+opts.space)
	}
	if opts.token != "" {
		cmd.Env = append(cmd.Env, "ANALOG_TOKEN="+opts.token)
	}
	if opts.stdin != "" {
		cmd.Stdin = strings.NewReader(opts.stdin)
	}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		h.t.Fatal(err)
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

// run invokes as the default agent and fails the test on a non-zero exit.
func (h *harness) run(args ...string) string {
	h.t.Helper()
	r := h.invoke(options{}, args...)
	if r.code != 0 {
		h.t.Fatalf("`analog %s` exited %d\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), r.code, r.stdout, r.stderr)
	}
	return r.stdout
}

// invokeServer runs the server binary with the harness env, for the operator
// side (`seed`, `token`) that lives on that binary.
func (h *harness) invokeServer(args ...string) result {
	h.t.Helper()
	cmd := exec.Command(binaries.server, args...)
	cmd.Env = h.env
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		h.t.Fatal(err)
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

// asHuman invokes as the human actor, which is how these tests play the other side.
func (h *harness) asHuman(args ...string) string {
	h.t.Helper()
	r := h.invoke(options{actor: "human", kind: "human"}, args...)
	if r.code != 0 {
		h.t.Fatalf("`analog %s` (human) exited %d\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), r.code, r.stdout, r.stderr)
	}
	return r.stdout
}

func decodeJSON[T any](t *testing.T, raw string) T {
	t.Helper()
	var out T
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, raw)
	}
	return out
}

func (h *harness) addCard(slug, title, text string) map[string]any {
	h.t.Helper()
	return decodeJSON[map[string]any](h.t,
		h.run("add", slug, "--text", text, "--title", title, "--json"))
}

// --- spaces -----------------------------------------------------------------------

func TestNewAndSpaces(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign", "--title", "Nav redesign")
	listing := h.run("spaces")
	if !strings.Contains(listing, "redesign") || !strings.Contains(listing, "Nav redesign") {
		t.Errorf("spaces = %q", listing)
	}
	rows := decodeJSON[[]map[string]any](t, h.run("spaces", "--json"))
	if len(rows) != 1 || rows[0]["slug"] != "redesign" {
		t.Errorf("spaces --json = %v", rows)
	}
}

func TestOpenPrintsTheURL(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	if got := strings.TrimSpace(h.run("open", "redesign")); !strings.HasSuffix(got, "/s/redesign") {
		t.Errorf("open = %q", got)
	}
}

func TestOpenOnAMissingSpaceExitsNonZero(t *testing.T) {
	h := newHarness(t)
	if r := h.invoke(options{}, "open", "nope"); r.code != 1 {
		t.Errorf("exit = %d, want 1", r.code)
	}
}

func TestRmSpaceRefusesWithoutYes(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	r := h.invoke(options{}, "rm-space", "redesign")
	if r.code == 0 {
		t.Error("rm-space deleted a space without --yes")
	}
	if !strings.Contains(r.stderr, "--yes") {
		t.Errorf("stderr = %q, want it to name the flag", r.stderr)
	}
	h.run("rm-space", "redesign", "--yes")
	if rows := decodeJSON[[]map[string]any](t, h.run("spaces", "--json")); len(rows) != 0 {
		t.Errorf("spaces = %v, want none", rows)
	}
}

// --- add, with stdin -----------------------------------------------------------------

func TestAddFromAFile(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	draft := filepath.Join(t.TempDir(), "draft.md")
	if err := os.WriteFile(draft, []byte("## Option E\n\nlazy load"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.run("add", "redesign", "--title", "Option E", "--kind", "md", "--file", draft)
	nodes := decodeJSON[[]map[string]any](t, h.run("cards", "redesign", "--json"))
	if nodes[0]["sp_title"] != "Option E" || nodes[0]["text"] != "## Option E\n\nlazy load" {
		t.Errorf("node = %v", nodes[0])
	}
}

func TestAddFromStdin(t *testing.T) {
	// `cat chart.svg | analog add redesign --title Revenue --kind svg -`
	h := newHarness(t)
	h.run("new", "redesign")
	svg := `<svg xmlns="http://www.w3.org/2000/svg"><rect width="10" height="10"/></svg>`
	r := h.invoke(options{stdin: svg},
		"add", "redesign", "-", "--title", "Revenue", "--kind", "svg")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	node := decodeJSON[[]map[string]any](t, h.run("cards", "redesign", "--json"))[0]
	if node["sp_kind"] != "svg" || node["text"] != svg {
		t.Errorf("node = %v", node)
	}
}

func TestAddReportsTheNewID(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	output := h.run("add", "redesign", "--text", "hi", "--title", "T")
	if !strings.HasPrefix(strings.Fields(output)[0], "c_") {
		t.Errorf("add = %q", output)
	}
}

func TestAddWithoutContentIsAUsageError(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	if r := h.invoke(options{}, "add", "redesign", "--title", "T"); r.code == 0 {
		t.Error("add accepted a card with no content")
	}
}

func TestAnalogActorDrivesSpCreatedBy(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	for _, actor := range []string{"codex", "researcher-1"} {
		r := h.invoke(options{actor: actor},
			"add", "redesign", "--text", "x", "--title", actor)
		if r.code != 0 {
			t.Fatalf("exit %d: %s", r.code, r.stderr)
		}
	}
	nodes := decodeJSON[[]map[string]any](t, h.run("cards", "redesign", "--json"))
	if nodes[0]["sp_created_by"] != "codex" || nodes[1]["sp_created_by"] != "researcher-1" {
		t.Errorf("authors = %v, %v", nodes[0]["sp_created_by"], nodes[1]["sp_created_by"])
	}
}

// --- cards, update, rm, link -----------------------------------------------------------

func TestCardsListsIDTitleKindAndAuthor(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	h.run("add", "redesign", "--text", "a", "--title", "Option A")
	line := strings.TrimSpace(h.run("cards", "redesign"))
	for _, want := range []string{"c_", "Option A", "md", "claude-code"} {
		if !strings.Contains(line, want) {
			t.Errorf("cards line %q is missing %q", line, want)
		}
	}
}

func TestUpdateFromAFileBumpsRev(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	card := h.addCard("redesign", "T", "v1")
	fixed := filepath.Join(t.TempDir(), "fixed.svg")
	if err := os.WriteFile(fixed, []byte("<svg/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.run("update", "redesign", card["id"].(string), "--file", fixed)
	node := decodeJSON[[]map[string]any](t, h.run("cards", "redesign", "--json"))[0]
	if node["text"] != "<svg/>" || node["sp_rev"].(float64) != 2 {
		t.Errorf("node = %v", node)
	}
}

func TestUpdateModeBranchKeepsTheOldCard(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	card := h.addCard("redesign", "T", "v1")
	h.run("update", "redesign", card["id"].(string), "--text", "v2", "--mode", "branch")
	nodes := decodeJSON[[]map[string]any](t, h.run("cards", "redesign", "--json"))
	if len(nodes) != 2 {
		t.Fatalf("cards = %d, want 2", len(nodes))
	}
	superseded := false
	for _, n := range nodes {
		if n["sp_superseded_by"] != nil {
			superseded = true
		}
	}
	if !superseded {
		t.Error("branch mode must leave a superseded card behind")
	}
}

func TestUpdateSurfacesA409(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	card := h.addCard("redesign", "T", "v1")
	r := h.invoke(options{},
		"update", "redesign", card["id"].(string), "--text", "v2", "--if-match", "9")
	if r.code != 2 {
		t.Errorf("exit = %d, want 2 for a conflict\n%s", r.code, r.stderr)
	}
	if !strings.Contains(strings.ToLower(r.stderr), "conflict") {
		t.Errorf("stderr = %q", r.stderr)
	}
}

func TestRmSoftDeletes(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	card := h.addCard("redesign", "A", "a")
	h.run("rm", "redesign", card["id"].(string))
	if live := decodeJSON[[]map[string]any](t, h.run("cards", "redesign", "--json")); len(live) != 0 {
		t.Errorf("live cards = %d, want 0", len(live))
	}
	deleted := decodeJSON[[]map[string]any](t, h.run("cards", "redesign", "--deleted", "--json"))
	if len(deleted) != 1 {
		t.Errorf("deleted cards = %d, want 1", len(deleted))
	}
}

func TestLinkAndUnlink(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	a := h.addCard("redesign", "A", "a")
	b := h.addCard("redesign", "B", "b")
	edge := decodeJSON[map[string]any](t, h.run("link", "redesign",
		a["id"].(string), b["id"].(string), "--label", "depends on", "--json"))
	if edge["label"] != "depends on" {
		t.Errorf("edge = %v", edge)
	}
	h.run("unlink", "redesign", edge["id"].(string))
	canvas := decodeJSON[map[string]any](t, h.run("export", "redesign"))
	if edges := canvas["edges"].([]any); len(edges) != 0 {
		t.Errorf("edges = %v, want none", edges)
	}
}

// --- feedback ---------------------------------------------------------------------------

func TestFeedbackPrintsNothingWhenNothingChanged(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	r := h.invoke(options{}, "feedback", "redesign")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if strings.TrimSpace(r.stdout) != "" {
		t.Errorf("SPEC §4.2: silence means nothing changed, got %q", r.stdout)
	}
}

// humanClient is the other side of the canvas. The CLI has no command for leaving
// an annotation — that is the web UI's job — so these tests play the human through
// the client package, exactly as the Python tests did.
func (h *harness) humanClient() *client.Client {
	return client.New(client.Options{URL: h.url, Actor: "human", ActorKind: "human",
		Config: map[string]string{}})
}

func TestFeedbackShowsHumanCommentsDeletionsAndLinks(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	a := h.addCard("redesign", "Option A", "a")
	b := h.addCard("redesign", "Option B", "b")
	d := h.addCard("redesign", "Option D", "d")
	h.run("feedback", "redesign") // consume own writes

	human := h.humanClient()
	if _, err := human.CreateAnnotation("redesign", b["id"].(string),
		"y-axis starts at 40", nil, "editing"); err != nil {
		t.Fatal(err)
	}
	if err := human.DeleteCard("redesign", d["id"].(string)); err != nil {
		t.Fatal(err)
	}
	if _, err := human.LinkCards("redesign", a["id"].(string), b["id"].(string),
		"depends on"); err != nil {
		t.Fatal(err)
	}

	output := h.run("feedback", "redesign")
	for _, want := range []string{
		"1 open comment, 1 deleted, 1 new link.",
		"y-axis starts at 40", "[editing]", "Option D", "depends on",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("feedback is missing %q:\n%s", want, output)
		}
	}
}

// --- annotations ---------------------------------------------------------------------------

func TestCommentsAndResolveByIDAlone(t *testing.T) {
	// SPEC §4.2 spells `analog resolve a_7f --reply "..."` with no slug.
	h := newHarness(t)
	h.run("new", "redesign")
	card := h.addCard("redesign", "A", "a")
	annotation, err := h.humanClient().CreateAnnotation("redesign", card["id"].(string),
		"fix the axis", nil, "")
	if err != nil {
		t.Fatal(err)
	}

	listing := h.run("comments", "redesign")
	if !strings.Contains(listing, annotation.ID) || !strings.Contains(listing, "fix the axis") {
		t.Errorf("comments = %q", listing)
	}

	h.run("resolve", annotation.ID, "--reply", "rebased axis at 0")
	if open := strings.TrimSpace(h.run("comments", "redesign")); open != "" {
		t.Errorf("still open: %q", open)
	}
	done := decodeJSON[[]map[string]any](t, h.run("comments", "redesign", "--all", "--json"))
	if done[0]["resolved"] != true || done[0]["resolved_reply"] != "rebased axis at 0" {
		t.Errorf("annotation = %v", done[0])
	}
}

func TestResolveUsesAnalogSpaceWhenGiven(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	card := h.addCard("redesign", "A", "a")
	annotation, err := h.humanClient().CreateAnnotation("redesign", card["id"].(string),
		"b", nil, "")
	if err != nil {
		t.Fatal(err)
	}
	r := h.invoke(options{space: "redesign"}, "resolve", annotation.ID)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if open := decodeJSON[[]map[string]any](t, h.run("comments", "redesign", "--json")); len(open) != 0 {
		t.Errorf("open = %v, want none", open)
	}
}

func TestResolvingAnUnknownAnnotationExitsNonZero(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	if r := h.invoke(options{}, "resolve", "a_nope"); r.code != 1 {
		t.Errorf("exit = %d, want 1", r.code)
	}
}

func TestFeedbackPeekDoesNotConsume(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	card := h.addCard("redesign", "A", "a")
	h.run("feedback", "redesign")
	h.asHuman("update", "redesign", card["id"].(string), "--text", "v2")

	for i := 0; i < 2; i++ {
		if !strings.Contains(h.run("feedback", "redesign", "--peek"), "1 card edited") {
			t.Fatalf("peek %d did not report the edit", i)
		}
	}
	if !strings.Contains(h.run("feedback", "redesign"), "1 card edited") {
		t.Fatal("the consuming call did not report the edit")
	}
	if strings.Contains(h.run("feedback", "redesign"), "card edited") {
		t.Error("the cursor did not advance")
	}
}

func TestCursorsArePerActor(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	card := h.addCard("redesign", "A", "a")
	h.asHuman("update", "redesign", card["id"].(string), "--text", "v2")

	for _, actor := range []string{"claude-code", "codex"} {
		r := h.invoke(options{actor: actor}, "feedback", "redesign")
		if !strings.Contains(r.stdout, "1 card edited") {
			t.Errorf("%s did not see the edit: %q", actor, r.stdout)
		}
	}
	again := h.invoke(options{actor: "claude-code"}, "feedback", "redesign")
	if strings.TrimSpace(again.stdout) != "" {
		t.Errorf("claude-code's cursor did not advance: %q", again.stdout)
	}
}

func TestFeedbackJSONMatchesTheAPIShape(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	card := h.addCard("redesign", "A", "a")
	h.run("feedback", "redesign")
	h.asHuman("update", "redesign", card["id"].(string), "--text", "v2")

	body := decodeJSON[map[string]any](t, h.run("feedback", "redesign", "--json"))
	want := []string{"cursor", "annotations", "replies", "cards_edited", "cards_deleted",
		"cards_moved", "links_added", "links_removed", "summary"}
	if len(body) != len(want) {
		t.Errorf("keys = %v, want exactly %v", keysOf(body), want)
	}
	for _, key := range want {
		if _, ok := body[key]; !ok {
			t.Errorf("feedback --json is missing %q", key)
		}
	}
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// --- export / import -----------------------------------------------------------------------

func TestExportIsJSONCanvasAndImportRoundTrips(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	h.run("new", "copy")
	a := h.addCard("redesign", "A", "a")
	b := h.addCard("redesign", "B", "b")
	h.run("link", "redesign", a["id"].(string), b["id"].(string), "--label", "leads to")

	exported := h.run("export", "redesign")
	canvas := decodeJSON[map[string]any](t, exported)
	if len(canvas) != 2 || canvas["nodes"] == nil || canvas["edges"] == nil {
		t.Fatalf("export = %v, want just nodes and edges", keysOf(canvas))
	}

	if r := h.invoke(options{stdin: exported}, "import", "copy", "-"); r.code != 0 {
		t.Fatalf("import exited %d: %s", r.code, r.stderr)
	}
	copied := decodeJSON[map[string]any](t, h.run("export", "copy"))
	var titles []string
	for _, n := range copied["nodes"].([]any) {
		titles = append(titles, n.(map[string]any)["sp_title"].(string))
	}
	if len(titles) != 2 || titles[0] != "A" || titles[1] != "B" {
		t.Errorf("titles = %v, want [A B]", titles)
	}
	edges := copied["edges"].([]any)
	if len(edges) != 1 || edges[0].(map[string]any)["label"] != "leads to" {
		t.Errorf("edges = %v", edges)
	}
}

func TestImportIsAdditive(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	h.run("add", "redesign", "--text", "mine", "--title", "Mine")
	incoming, _ := json.Marshal(map[string]any{
		"nodes": []any{map[string]any{"id": "x", "type": "text", "x": 0, "y": 0,
			"width": 100, "height": 100, "text": "new", "sp_title": "New"}},
		"edges": []any{},
	})
	if r := h.invoke(options{stdin: string(incoming)}, "import", "redesign", "-"); r.code != 0 {
		t.Fatalf("import exited %d: %s", r.code, r.stderr)
	}
	if nodes := decodeJSON[[]map[string]any](t, h.run("cards", "redesign", "--json")); len(nodes) != 2 {
		t.Errorf("cards = %d, want 2", len(nodes))
	}
}

// --- events ---------------------------------------------------------------------------------

func TestEventsPrintsTheLog(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	h.run("add", "redesign", "--text", "a", "--title", "A")
	output := h.run("events", "redesign")
	if !strings.Contains(output, "card.created") || !strings.Contains(output, "claude-code") {
		t.Errorf("events = %q", output)
	}
}

// --- the command surface -----------------------------------------------------------------------

func TestEveryDocumentedCommandExists(t *testing.T) {
	h := newHarness(t)
	help := h.run("--help")
	for _, command := range []string{"spaces", "feedback", "add", "cards", "update", "rm",
		"link", "resolve", "export", "import", "open", "whoami", "login", "token",
		"unlink", "upload", "events", "comments", "new", "rm-space"} {
		if !strings.Contains(help, command) {
			t.Errorf("`analog --help` does not mention %q", command)
		}
	}
}

// TestUpdateKind covers #10: a card's kind was set at creation and then fixed
// forever, so a markdown card typed in as svg could not be corrected.
func TestUpdateKind(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	card := h.addCard("redesign", "T", "<svg/>")
	if card["sp_kind"] != "md" {
		t.Fatalf("precondition: sp_kind = %v, want md", card["sp_kind"])
	}
	h.run("update", "redesign", card["id"].(string), "--kind", "svg")
	node := decodeJSON[[]map[string]any](t, h.run("cards", "redesign", "--json"))[0]
	if node["sp_kind"] != "svg" {
		t.Errorf("sp_kind = %v, want svg", node["sp_kind"])
	}
	// --kind alone is enough; it does not need content alongside it.
	if node["text"] != "<svg/>" {
		t.Errorf("text = %v; --kind must not disturb the content", node["text"])
	}
}

func TestUpdateRejectsAnUnknownKind(t *testing.T) {
	h := newHarness(t)
	h.run("new", "redesign")
	card := h.addCard("redesign", "T", "x")
	r := h.invoke(options{}, "update", "redesign", card["id"].(string), "--kind", "pdf")
	if r.code == 0 {
		t.Fatal("an sp_kind outside the contract's enum was accepted")
	}
	node := decodeJSON[[]map[string]any](t, h.run("cards", "redesign", "--json"))[0]
	if node["sp_kind"] != "md" {
		t.Errorf("sp_kind = %v; a rejected patch must change nothing", node["sp_kind"])
	}
}
