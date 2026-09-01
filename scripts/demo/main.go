// Command demo is SPEC §7's acceptance demo, start to finish.
//
//	go run ./scripts/demo reset         # wipe the demo space, start over
//	go run ./scripts/demo agent-a       # 1-3: create the space, post cards, link them
//	<human does step 4 in the browser>
//	go run ./scripts/demo agent-b       # 5-6: read feedback over the CLI, post a fix
//	go run ./scripts/demo agent-a-again # 7:  Agent A's independent cursor
//
// And when you want more than the narrative — a smoke pass over everything else the
// surface can do, no human needed. Creates and deletes its own scratch spaces:
//
//	go run ./scripts/demo extras
//
// Agent A speaks MCP over stdio to the `analog-mcp` binary — a real subprocess and a
// real protocol round trip, not a function call. Agent B shells out to `analog`. They
// are different actors with independent cursors, which is the whole point of step 7.
//
// Needs nothing but the Go toolchain: the MCP transport is newline-delimited
// JSON-RPC, which is about forty lines below rather than a dependency.
//
// Binaries come from ./bin, or $ANALOG_BIN_DIR.
package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The source path is the only fixed point a `go run` binary has: os.Executable is
// a throwaway in the build cache, so the repo root comes from where this file sits.
var repoRoot = func() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "cannot locate the repo root")
		os.Exit(1)
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}()

var binDir = func() string {
	if dir := os.Getenv("ANALOG_BIN_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(repoRoot, "bin")
}()

const slug = "demo"

var serverURL = func() string {
	if url := os.Getenv("ANALOG_URL"); url != "" {
		return url
	}
	return "http://127.0.0.1:8787"
}()

const (
	actorA = "claude-code"
	actorB = "codex"
	lab    = "demo-lab"
	// Scratch space for the export/import round trip.
	roundtrip = "demo-lab-roundtrip"
)

// --- output ---------------------------------------------------------------------

func heading(text string) {
	fmt.Printf("\n\x1b[1m%s\x1b[0m\n", text)
}

func show(label string, value any) {
	rendered, ok := value.(string)
	if !ok {
		rendered = jsonOf(value)
	}
	fmt.Printf("  %s: %s\n", label, rendered)
}

func printIndented(text string) {
	trimmed := strings.TrimRight(text, " \t\n")
	for _, line := range strings.Split(trimmed, "\n") {
		fmt.Println("  " + line)
	}
}

func fatal(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}

// jsonOf renders a value the way the wire does. Card text is routinely HTML and
// the display should say what was sent, same as writeJSON does.
func jsonOf(value any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		fatal("could not encode %v: %v", value, err)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// --- subprocess plumbing ----------------------------------------------------------

func binary(name string) string {
	path := filepath.Join(binDir, name)
	if st, err := os.Stat(path); err != nil || st.IsDir() {
		fatal("no %s — build the binaries first:  scripts/build.sh", path)
	}
	return path
}

func agentEnv(actor string) []string {
	return append(os.Environ(),
		"ANALOG_URL="+serverURL,
		"ANALOG_ACTOR="+actor,
		"ANALOG_ACTOR_KIND=agent",
		// Never let a ~/.analog.toml belonging to the operator decide who the
		// demo's agents are.
		"ANALOG_CONFIG=/nonexistent")
}

// runAnalog shells out to the CLI and returns stdout. A non-zero exit is a demo
// bug or a broken server, not something to recover from.
func runAnalog(actor, stdin string, args ...string) string {
	cmd := exec.Command(binary("analog"), args...)
	cmd.Env = agentEnv(actor)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		code := -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			code = exitErr.ExitCode()
		}
		fatal("analog %s failed (%d):\n%s", strings.Join(args, " "),
			code, strings.TrimSpace(errOut.String()))
	}
	return out.String()
}

func analog(args ...string) string { return runAnalog(actorB, "", args...) }

func analogStdin(stdin string, args ...string) string { return runAnalog(actorB, stdin, args...) }

func cliJSON(raw string) map[string]any {
	var out map[string]any
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		fatal("expected a JSON object from the CLI, got: %s", strings.TrimSpace(raw))
	}
	return out
}

func tryRmSpace(space, actor string) {
	// A 404 is fine — it just means there was nothing to clean up.
	cmd := exec.Command(binary("analog"), "rm-space", space, "--yes")
	cmd.Env = agentEnv(actor)
	_ = cmd.Run()
}

// humanPost is one write as the human, over raw HTTP: what the browser does in
// step 4. No token: the dev server this demo targets runs with auth off.
func humanPost(path string, body any) map[string]any {
	payload, err := json.Marshal(body)
	if err != nil {
		fatal("human_post: %v", err)
	}
	req, err := http.NewRequest("POST", serverURL+"/api"+path+"?actor=human&actor_kind=human",
		bytes.NewReader(payload))
	if err != nil {
		fatal("human_post: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fatal("human_post %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		fatal("human_post %s: %s\n%s", path, resp.Status, raw)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		fatal("human_post %s: %v", path, err)
	}
	return out
}

// --- Agent A: MCP over stdio --------------------------------------------------------

// mcpClient is the smallest MCP stdio client that can drive ten tools.
type mcpClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Scanner
	nextID int
	// await_feedback holds the connection for up to a timeout_s in its own
	// goroutine; the mutex keeps that round trip exclusive.
	mu sync.Mutex
}

func newMCP(actor string) *mcpClient {
	cmd := exec.Command(binary("analog-mcp"))
	cmd.Env = agentEnv(actor)
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		fatal("mcp: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		fatal("mcp: %v", err)
	}
	if err := cmd.Start(); err != nil {
		fatal("mcp: %v", err)
	}
	m := &mcpClient{cmd: cmd, stdin: stdin}
	m.stdout = bufio.NewScanner(stdout)
	// Card text rides in tool arguments, and it can be large — the same buffer
	// analog-mcp allows on the way in.
	m.stdout.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	m.call("initialize", map[string]any{
		"protocolVersion": "2024-11-05", "capabilities": map[string]any{},
		"clientInfo": map[string]any{"name": "analog-demo", "version": "0"}})
	m.notify("notifications/initialized", nil)
	return m
}

func (m *mcpClient) close() {
	m.mu.Lock()
	_ = m.stdin.Close()
	m.mu.Unlock()
	done := make(chan struct{})
	go func() { _ = m.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = m.cmd.Process.Kill()
		<-done
	}
}

func (m *mcpClient) send(message map[string]any) {
	line, err := json.Marshal(message)
	if err != nil {
		fatal("mcp: %v", err)
	}
	if _, err := m.stdin.Write(append(line, '\n')); err != nil {
		fatal("analog-mcp closed the connection: %v", err)
	}
}

func (m *mcpClient) notify(method string, params map[string]any) {
	if params == nil {
		params = map[string]any{}
	}
	m.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// call performs one round trip. A protocol error ends the demo; tool-level
// failures travel inside the result for tool() to unwrap.
func (m *mcpClient) call(method string, params map[string]any) map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	if params == nil {
		params = map[string]any{}
	}
	m.send(map[string]any{"jsonrpc": "2.0", "id": m.nextID, "method": method, "params": params})
	if !m.stdout.Scan() {
		fatal("analog-mcp closed the connection")
	}
	var msg struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(m.stdout.Bytes(), &msg); err != nil {
		fatal("mcp: could not decode the reply to %s: %v", method, err)
	}
	if msg.Error != nil {
		fatal("%s failed: %s", method, msg.Error.Message)
	}
	var result map[string]any
	if err := json.Unmarshal(msg.Result, &result); err != nil {
		fatal("mcp: %s returned a non-object result: %v", method, err)
	}
	return result
}

func (m *mcpClient) tools() []string {
	names := make([]string, 0)
	for _, t := range asMaps(m.call("tools/list", nil)["tools"]) {
		names = append(names, fmt.Sprint(t["name"]))
	}
	slices.Sort(names)
	return names
}

// tool is one tool call, unwrapped from the MCP envelope. The server folds
// non-object results into {"result": ...}; this reverses that.
func (m *mcpClient) tool(name string, arguments map[string]any) (any, error) {
	params := map[string]any{"name": name}
	if len(arguments) > 0 {
		params["arguments"] = arguments
	}
	result := m.call("tools/call", params)
	if isErr, _ := result["isError"].(bool); isErr {
		message := name
		if texts := asMaps(result["content"]); len(texts) > 0 {
			message = fmt.Sprintf("%s: %v", name, texts[0]["text"])
		}
		return nil, errors.New(message)
	}
	structured, _ := result["structuredContent"].(map[string]any)
	if inner, ok := structured["result"]; ok {
		return inner, nil
	}
	return structured, nil
}

// --- JSON decoding helpers ----------------------------------------------------------
//
// Everything the demo reads back is free-form wire JSON, decoded into plain maps.
// Numbers come back as float64; every place one is formatted goes through %v or
// formatNumber, which print 40, not 40.0.

func asMaps(v any) []map[string]any {
	rows, _ := v.([]any)
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		if m, ok := row.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func formatNumber(v any) string {
	if f, ok := v.(float64); ok {
		return strconv.FormatFloat(f, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// --- the narrative -----------------------------------------------------------------

const chartHTML = `<!doctype html><meta charset="utf-8">
<style>body{font:13px system-ui;margin:12px}
.bar{height:20px;background:#4a7;margin:6px 0;border-radius:3px}
.axis{border-top:1px solid #999;margin-top:10px;padding-top:4px;color:#666;font-size:11px}</style>
<h3>Render time by option (ms)</h3>
<div>A <div class="bar" style="width:62%"></div></div>
<div>B <div class="bar" style="width:31%"></div></div>
<div>C <div class="bar" style="width:78%"></div></div>
<div class="axis">y-axis starts at 40</div>
<script>document.title = "chart";</script>`

var options = []struct{ title, content string }{
	{"Option A", "## Option A\n\nShip the existing renderer, add lazy loading.\n\n" +
		"- lowest risk\n- no new deps"},
	{"Option B", "## Option B\n\nSwap to a virtualised list.\n\n" +
		"- best at scale\n- rewrite of the scroll logic"},
	{"Option C", "## Option C\n\nPaginate server-side.\n\n" +
		"- simplest client\n- extra round trips"},
	{"Option D", "## Option D\n\nCache everything in IndexedDB.\n\n" +
		"- fast on repeat visits\n- cache invalidation is a project"},
}

func agentA() {
	mcp := newMCP(actorA)
	defer mcp.close()

	tools := mcp.tools()
	heading("Agent A — MCP over stdio")
	show("tools", tools)
	if len(tools) != 10 {
		fatal("expected ten tools, got %d: %s", len(tools), jsonOf(tools))
	}

	heading("1. create_space('demo')")
	space, err := mcp.tool("create_space", map[string]any{"slug": slug, "title": "List perf — demo"})
	if err != nil {
		fatal("space '%s' already exists —  go run ./scripts/demo reset", slug)
	}
	show("space", asMap(space)["slug"])

	heading("2. add_cards — 4 options + 1 html chart")
	drafts := make([]any, 0, len(options)+1)
	for _, option := range options {
		drafts = append(drafts, map[string]any{
			"title": option.title, "content": option.content, "kind": "md"})
	}
	drafts = append(drafts, map[string]any{"title": "Render time by option",
		"content": chartHTML, "kind": "html", "width": 460, "height": 320})
	cards, err := mcp.tool("add_cards", map[string]any{"slug": slug, "cards": drafts})
	if err != nil {
		fatal("add_cards: %v", err)
	}
	byTitle := map[string]string{}
	for _, card := range asMaps(cards) {
		title := fmt.Sprint(card["sp_title"])
		byTitle[title] = fmt.Sprint(card["id"])
		show(title, fmt.Sprintf("%v  (%v) at (%v, %v)",
			card["id"], card["sp_kind"], card["x"], card["y"]))
	}

	heading("3. link_cards — Option B contradicts Option D")
	edge, err := mcp.tool("link_cards", map[string]any{"slug": slug,
		"from_card": byTitle["Option B"], "to_card": byTitle["Option D"], "label": "contradicts"})
	if err != nil {
		fatal("link_cards: %v", err)
	}
	show("edge", fmt.Sprintf("%v  %v", asMap(edge)["id"], asMap(edge)["label"]))

	idsPath := filepath.Join(repoRoot, "demo", "ids.json")
	if err := os.MkdirAll(filepath.Dir(idsPath), 0o755); err != nil {
		fatal("write ids.json: %v", err)
	}
	body, err := json.MarshalIndent(byTitle, "", "  ")
	if err != nil {
		fatal("write ids.json: %v", err)
	}
	if err := os.WriteFile(idsPath, body, 0o644); err != nil {
		fatal("write ids.json: %v", err)
	}

	fmt.Printf("\nNow do step 4 in the browser: %s/s/%s\n", serverURL, slug)
	fmt.Println("  drag cards · delete Option D · pin a comment on the chart reading")
	fmt.Println("  \"y-axis starts at 40, fix\" · link Option A -> Option C \"depends on\"")
}

func agentB() {
	heading("5. Agent B (a different agent, over the CLI): analog feedback demo")
	report := analog("feedback", slug)
	if strings.TrimSpace(report) == "" {
		fmt.Println("  (nothing)")
		fatal("Agent B saw nothing — has step 4 been done in the browser?")
	}
	printIndented(report)

	heading("6. Agent B: post the fixed chart, then resolve the comment")
	fixed, err := os.ReadFile(filepath.Join(repoRoot, "demo", "fixed.svg"))
	if err != nil {
		fatal("read demo/fixed.svg: %v", err)
	}
	added := analogStdin(string(fixed), "add", slug, "-", "--title",
		"Render time by option (fixed)", "--kind", "svg")
	show("cat fixed.svg | analog add demo --kind svg -", strings.TrimSpace(added))

	open := analog("comments", slug, "--json")
	var annotations []map[string]any
	if err := json.Unmarshal([]byte(open), &annotations); err != nil {
		fatal("comments --json: %v", err)
	}
	if len(annotations) == 0 {
		fatal("no open comment to resolve — was one pinned in step 4?")
	}
	target := annotations[0]
	show("resolving", fmt.Sprintf("%v  %q", target["id"], target["body"]))
	show("analog resolve", strings.TrimSpace(
		analog("resolve", fmt.Sprint(target["id"]), "--reply", "rebased axis at 0")))
}

func agentAAgain() {
	mcp := newMCP(actorA)
	defer mcp.close()

	heading("7. Agent A calls get_feedback again — its own cursor, not Agent B's")
	feedback, err := mcp.tool("get_feedback", map[string]any{"slug": slug})
	if err != nil {
		fatal("get_feedback: %v", err)
	}
	fb := asMap(feedback)
	show("summary", fb["summary"])
	for _, bucket := range []string{"annotations", "cards_edited", "cards_deleted",
		"cards_moved", "links_added", "links_removed"} {
		if rows := asMaps(fb[bucket]); len(rows) > 0 {
			show(bucket, rows)
		}
	}

	actors := map[string]bool{}
	for _, bucket := range []string{"cards_edited", "cards_deleted", "cards_moved",
		"links_added", "links_removed"} {
		for _, row := range asMaps(fb[bucket]) {
			if actor, ok := row["actor"].(string); ok {
				actors[actor] = true
			}
		}
	}
	replayed := false
	for _, row := range asMaps(fb["cards_edited"]) {
		if title, _ := row["title"].(string); strings.Contains(title, "fixed") {
			replayed = true
		}
	}

	heading("checks")
	fmt.Printf("  every delta came from the human, not from an agent : %v  %s\n",
		len(actors) == 1 && actors["human"], actorsRepr(actors))
	fmt.Printf("  Agent B's card was not replayed as an edit          : %v\n", !replayed)
	fmt.Printf("  the comment Agent B resolved is gone                : %v\n",
		len(asMaps(fb["annotations"])) == 0)
}

func actorsRepr(actors map[string]bool) string {
	names := make([]string, 0, len(actors))
	for actor := range actors {
		names = append(names, actor)
	}
	slices.Sort(names)
	return "{" + strings.Join(names, ", ") + "}"
}

// --- Extras: the rest of the surface, no human needed --------------------------------
//
// The §7 narrative is a story; this is an inventory. Everything MCP or the CLI can
// do that the story does not touch gets one honest round trip here, on scratch
// spaces that are deleted again at the end. The one thing it cannot do is be the
// human in a browser, so the human's part here — pinning an annotation — goes
// over raw HTTP as the human actor, clearly labelled.

// A 1×1 PNG, so `analog upload` has a real image without a fixture on disk.
var pixelPNG = func() []byte {
	raw, err := base64.StdEncoding.DecodeString(
		"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg==")
	if err != nil {
		panic(err) // a malformed literal here is a bug in this file, not the demo
	}
	return raw
}()

// expectConflict documents the 409 as a feature: surfaced to the caller, never
// auto-resolved (SPEC §3).
func expectConflict(run func() error, label string) {
	if err := run(); err != nil {
		show(label, strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", " "))
		return
	}
	fatal("%s: expected a conflict, got success", label)
}

func extras() {
	tryRmSpace(lab, actorA)
	tryRmSpace(roundtrip, actorA)

	heading("E0. analog whoami — the first diagnostic when something 401s or 403s")
	printIndented(analog("whoami"))

	a := newMCP(actorA)
	defer a.close()
	b := newMCP(actorB)
	defer b.close()

	heading("E1. list_spaces, create_space, read_space (MCP)")
	spaces, err := a.tool("list_spaces", nil)
	if err != nil {
		fatal("list_spaces: %v", err)
	}
	slugs := make([]string, 0, len(asMaps(spaces)))
	for _, space := range asMaps(spaces) {
		slugs = append(slugs, fmt.Sprint(space["slug"]))
	}
	show("spaces", slugs)
	if _, err := a.tool("create_space", map[string]any{"slug": lab, "title": "Extras lab"}); err != nil {
		fatal("create_space: %v", err)
	}
	read, err := a.tool("read_space", map[string]any{"slug": lab})
	if err != nil {
		fatal("read_space: %v", err)
	}
	canvas := asMap(read)
	show("lab", fmt.Sprintf("%d nodes, %d edges, %d open annotations",
		len(asMaps(canvas["nodes"])), len(asMaps(canvas["edges"])),
		len(asMaps(canvas["annotations"]))))

	heading("E2. add_cards: explicit x/y, auto-layout, and the plain kind")
	cards, err := a.tool("add_cards", map[string]any{"slug": lab, "cards": []any{
		map[string]any{"title": "Pinned at (40, 40)", "kind": "plain",
			"content": "coordinates chosen by the agent", "x": 40, "y": 40},
		map[string]any{"title": "Placed by the server",
			"content": "no x/y — auto-layout decides"},
	}})
	if err != nil {
		fatal("add_cards: %v", err)
	}
	created := asMaps(cards)
	pinned, placed := created[0], created[1]
	for _, card := range created {
		show(fmt.Sprint(card["sp_title"]), fmt.Sprintf("(%v, %v)  rev %v",
			card["x"], card["y"], card["sp_rev"]))
	}

	heading("E3. update_card: if_match honoured, a stale one must 409")
	upd, err := a.tool("update_card", map[string]any{"slug": lab, "card_id": pinned["id"],
		"patch": map[string]any{"text": "rewritten"}, "if_match": pinned["sp_rev"]})
	if err != nil {
		fatal("update_card: %v", err)
	}
	show("fresh if_match", fmt.Sprintf("rev %v -> %v", pinned["sp_rev"], asMap(upd)["sp_rev"]))
	expectConflict(func() error {
		_, err := a.tool("update_card", map[string]any{"slug": lab, "card_id": pinned["id"],
			"patch": map[string]any{"text": "lost write"}, "if_match": pinned["sp_rev"]})
		return err
	}, "stale if_match")

	heading("E4. delete_card — the agent's own card, so this is allowed")
	if _, err := a.tool("delete_card", map[string]any{"slug": lab, "card_id": pinned["id"]}); err != nil {
		fatal("delete_card: %v", err)
	}
	show("deleted", pinned["id"])

	heading("E5. await_feedback — a resident agent blocks, and wakes on a comment")
	backlog, err := b.tool("get_feedback", map[string]any{"slug": lab})
	if err != nil {
		fatal("get_feedback: %v", err)
	}
	summary, _ := asMap(backlog)["summary"].(string)
	if summary == "" {
		summary = "(nothing)"
	}
	show("B consumes backlog", summary)

	woke := make(chan any, 1)
	wokeErr := make(chan error, 1)
	go func() {
		fb, err := b.tool("await_feedback", map[string]any{"slug": lab, "timeout_s": 30, "poll_s": 1})
		if err != nil {
			wokeErr <- err
			return
		}
		woke <- fb
	}()
	time.Sleep(1 * time.Second)
	// The one browser gesture this script cannot make: a rect pin with an
	// instruction in it, exactly as step 4's human would leave.
	note := humanPost("/spaces/"+lab+"/annotations", map[string]any{
		"card_id":  placed["id"],
		"selector": map[string]any{"type": "rect", "x": 0.1, "y": 0.2, "w": 0.3, "h": 0.25},
		"body":     "> tighten this\n\nthe prose rambles", "motivation": "editing"})
	show("human pinned", fmt.Sprintf("%v on %v  (rect selector)",
		note["id"], placed["sp_title"]))
	var waited map[string]any
	select {
	case v := <-woke:
		waited = asMap(v)
	case err := <-wokeErr:
		fatal("await_feedback: %v", err)
	case <-time.After(35 * time.Second):
		fatal("await_feedback never woke on the annotation")
	}
	if !strings.Contains(jsonOf(waited), "rambles") {
		fatal("await_feedback never woke on the annotation")
	}
	stale0 := asMaps(waited["annotations"])[0]
	show("B woke to", fmt.Sprintf("%q  stale=%v", stale0["body"], stale0["stale"]))

	heading("E6. the rewrite that makes the pin stale, seen by the resident")
	if _, err := a.tool("update_card", map[string]any{"slug": lab, "card_id": placed["id"],
		"patch": map[string]any{"text": "tightened prose"}}); err != nil {
		fatal("update_card: %v", err)
	}
	fb, err := b.tool("get_feedback", map[string]any{"slug": lab})
	if err != nil {
		fatal("get_feedback: %v", err)
	}
	show("cards_edited", asMap(fb)["cards_edited"])
	stale1 := asMaps(asMap(fb)["annotations"])[0]
	if isStale, _ := stale1["stale"].(bool); !isStale {
		fatal("annotation should be stale after a content rewrite")
	}
	show("stale flip", fmt.Sprintf("card_rev %v < sp_rev now — stale=%v",
		stale1["card_rev"], stale1["stale"]))

	heading("E7. resolve_annotation (MCP), with the reply the human reads")
	resolved, err := b.tool("resolve_annotation", map[string]any{
		"annotation_id": stale1["id"], "slug": lab, "reply": "tightened — see the new rev"})
	if err != nil {
		fatal("resolve_annotation: %v", err)
	}
	show("resolved", asMap(resolved)["id"])

	heading("E8. CLI media: upload makes a JSON Canvas file node")
	png, err := os.CreateTemp("", "demo-*.png")
	if err != nil {
		fatal("temp file: %v", err)
	}
	if _, err := png.Write(pixelPNG); err != nil {
		fatal("temp file: %v", err)
	}
	if err := png.Close(); err != nil {
		fatal("temp file: %v", err)
	}
	defer os.Remove(png.Name())
	fileCard := cliJSON(analog("upload", lab, png.Name(), "--json"))
	show("file node", fmt.Sprintf("%v  type=%v  %s",
		fileCard["id"], fileCard["type"], jsonOf(fileCard["file"])))

	heading("E9. link, unlink — and one pair that cancels inside one window")
	fileCardID, placedID := fmt.Sprint(fileCard["id"]), fmt.Sprint(placed["id"])
	linked := cliJSON(analog("link", lab, fileCardID, placedID,
		"--label", "illustrates", "--json"))
	show("linked", linked["id"])
	fb, err = a.tool("get_feedback", map[string]any{"slug": lab})
	if err != nil {
		fatal("get_feedback: %v", err)
	}
	feedback := asMap(fb)
	if len(asMaps(feedback["links_added"])) == 0 || len(asMaps(feedback["annotations"])) > 0 {
		fatal("expected A to see B's link and no open comments: %s", jsonOf(feedback))
	}
	labels := make([]string, 0, len(asMaps(feedback["links_added"])))
	for _, link := range asMaps(feedback["links_added"]) {
		labels = append(labels, fmt.Sprint(link["label"]))
	}
	show("A saw links_added", labels)
	edgeID := asMaps(feedback["links_added"])[0]["id"]
	analog("unlink", lab, fmt.Sprint(edgeID))
	fb, err = a.tool("get_feedback", map[string]any{"slug": lab})
	if err != nil {
		fatal("get_feedback: %v", err)
	}
	if len(asMaps(asMap(fb)["links_removed"])) == 0 {
		fatal("A should have seen the link removal")
	}
	show("A saw links_removed", edgeID)
	// Created and removed before A looked again: neither bucket (DECISIONS.md).
	added := cliJSON(analog("link", lab, placedID, fileCardID,
		"--label", "noise", "--json"))
	analog("unlink", lab, fmt.Sprint(added["id"]))
	fb, err = a.tool("get_feedback", map[string]any{"slug": lab})
	if err != nil {
		fatal("get_feedback: %v", err)
	}
	if len(asMaps(asMap(fb)["links_added"])) > 0 || len(asMaps(asMap(fb)["links_removed"])) > 0 {
		fatal("a link created and removed in one window shows in neither")
	}
	fmt.Println("  created+removed between A's reads         : in neither bucket  (correct)")

	heading("E10. export → import into a branch-mode space (Obsidian round trip)")
	canvasFile, err := os.CreateTemp("", "demo-*.canvas")
	if err != nil {
		fatal("temp file: %v", err)
	}
	if _, err := canvasFile.WriteString(analog("export", lab)); err != nil {
		fatal("temp file: %v", err)
	}
	if err := canvasFile.Close(); err != nil {
		fatal("temp file: %v", err)
	}
	defer os.Remove(canvasFile.Name())
	if _, err := a.tool("create_space", map[string]any{"slug": roundtrip,
		"title": "Extras roundtrip", "revision_mode": "branch"}); err != nil {
		fatal("create_space: %v", err)
	}
	idMap := cliJSON(analog("import", roundtrip, "--file", canvasFile.Name(), "--json"))["id_map"]
	remapped := asMap(idMap)
	show("id_map", fmt.Sprintf("%d cards remapped (clients never choose ids)", len(remapped)))
	var old any
	for _, id := range remapped {
		old = id
		break
	}
	branched, err := a.tool("update_card", map[string]any{"slug": roundtrip,
		"card_id": old, "patch": map[string]any{"text": "revised in branch mode"}})
	if err != nil {
		fatal("update_card: %v", err)
	}
	if fmt.Sprint(asMap(branched)["id"]) == fmt.Sprint(old) {
		fatal("branch mode should return a NEW card")
	}
	round, err := a.tool("read_space", map[string]any{"slug": roundtrip})
	if err != nil {
		fatal("read_space: %v", err)
	}
	show("branch", fmt.Sprintf("%v superseded by %v  auto-link %q", old,
		asMap(branched)["id"], asMaps(asMap(round)["edges"])[0]["label"]))

	heading("E11. feedback --since 0 --peek — replay without advancing the cursor")
	replay := analog("feedback", lab, "--since", "0", "--peek")
	again := analog("feedback", lab, "--since", "0", "--peek")
	if replay != again {
		fatal("--peek must not advance the cursor")
	}
	fmt.Println("  identical twice, cursor untouched — silence still means nothing changed")

	heading("E12. events --watch rides the SSE stream")
	// Start the watcher at the current cursor, so the backlog is empty and any
	// event it prints can only have arrived live.
	cursor := cliJSON(analog("events", lab, "--json"))["cursor"]
	watcher := exec.Command(binary("analog"), "events", lab, "--since",
		formatNumber(cursor), "--watch")
	watcher.Env = agentEnv(actorB)
	watcher.Stderr = io.Discard
	pipe, err := watcher.StdoutPipe()
	if err != nil {
		fatal("events --watch: %v", err)
	}
	if err := watcher.Start(); err != nil {
		fatal("events --watch: %v", err)
	}
	outCh := make(chan string, 1)
	go func() {
		raw, _ := io.ReadAll(pipe)
		outCh <- string(raw)
	}()
	time.Sleep(1 * time.Second)
	blip := cliJSON(analog("link", lab, fileCardID, placedID,
		"--label", "sse blip", "--json"))
	analog("unlink", lab, fmt.Sprint(blip["id"]))
	var out string
	select {
	case out = <-outCh:
	case <-time.After(8 * time.Second):
		_ = watcher.Process.Kill()
		out = <-outCh
	}
	_ = watcher.Wait()
	var seen []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "link.created") {
			seen = append(seen, line)
		}
	}
	if len(seen) == 0 {
		fatal("events --watch never saw the link event — SSE is broken?")
	}
	last := strings.TrimSpace(seen[len(seen)-1])
	if runes := []rune(last); len(runes) > 80 {
		last = string(runes[:80])
	}
	show("arrived live", last)

	heading("cleanup")
	tryRmSpace(lab, actorA)
	tryRmSpace(roundtrip, actorA)
	fmt.Printf("  %s and %s deleted — the narrative's '%s' space is untouched\n",
		lab, roundtrip, slug)
}

func main() {
	usage := `The SPEC §7 acceptance demo, start to finish.

    go run ./scripts/demo reset         # wipe the demo space, start over
    go run ./scripts/demo agent-a       # 1-3: create the space, post cards, link them
    <human does step 4 in the browser>
    go run ./scripts/demo agent-b       # 5-6: read feedback over the CLI, post a fix
    go run ./scripts/demo agent-a-again # 7:  Agent A's independent cursor
    go run ./scripts/demo extras        # a smoke pass over everything else

Binaries come from ./bin, or $ANALOG_BIN_DIR.
`
	step := "all"
	if len(os.Args) > 1 {
		step = os.Args[1]
	}
	switch step {
	case "agent-a":
		agentA()
	case "agent-b":
		agentB()
	case "agent-a-again":
		agentAAgain()
	case "extras":
		extras()
	case "reset":
		tryRmSpace(slug, actorA)
		fmt.Printf("'%s' deleted (a 404 above means it was already gone)\n", slug)
	default:
		fmt.Print(usage)
		os.Exit(1)
	}
}
