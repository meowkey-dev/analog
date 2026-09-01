// Package conformance is the executable definition of Analog.
//
// It began in python — written against contracts/ and SPEC.md before any Go
// implementation existed — and was ported to Go beside the original in issue #58,
// with the python suite as the standing judge until parity was proven; the python
// original retired in the patch after that. It is a separate go module on purpose:
// nothing here may import github.com/meowkey-dev/analog, so the suite cannot
// quietly start testing the implementation's own objects. It speaks HTTP to a
// spawned server process, the way any other client would. The rule is asserted by
// TestBlackBox_NoAnalogModule.
//
// Dependencies: the standard library, plus modernc.org/sqlite for the frozen
// schema tests (the schema tests' contract is "this DDL behaves as sqlite says",
// which no amount of HTTP can reach). It is the same pure-go driver the server
// uses, so there is no toolchain implication.
//
// Contract the server binary must honour (see tests/README.md):
//
//	<bin> --host H --port P            serve; /api/health answers when ready
//	<bin> seed --db D --media-dir M --reset
//	                                   load contracts/fixtures/ into a fresh database
//	<bin> token add ACTOR --kind K     mint a token into $ANALOG_AUTH_FILE and print
//	                                   it on a line of its own
package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

// The source path is the only fixed point a test binary has.
var repoRoot = func() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("cannot locate the repo root")
	}
	return filepath.Dir(filepath.Dir(file))
}()

var fixturesDir = filepath.Join(repoRoot, "contracts", "fixtures")

// How long to wait for a freshly spawned server to answer /api/health.
const startupTimeout = 20 * time.Second

var tokenRE = regexp.MustCompile(`^analog_[A-Za-z0-9_-]+$`)

// --- json plumbing ---------------------------------------------------------------
//
// Everything the wire carries is decoded with literal numbers (json.Number), so a
// fixture's "x": 0 compares equal to a response's "x": 0 and never to 0.0.

func parseJSON(t *testing.T, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("could not decode json: %v\n%s", err, raw)
	}
	return v
}

// jlit is a json literal from test source, for wants that read better inline than
// as go maps.
func jlit(t *testing.T, s string) any { return parseJSON(t, []byte(s)) }

func fixture(t *testing.T, name string) any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(fixturesDir, name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return parseJSON(t, raw)
}

var openapiRaw []byte

// openapiDoc loads contracts/openapi.json — the contract document itself.
func openapiDoc(t *testing.T) map[string]any {
	t.Helper()
	if openapiRaw == nil {
		raw, err := os.ReadFile(filepath.Join(repoRoot, "contracts", "openapi.json"))
		if err != nil {
			t.Fatalf("read openapi.json: %v", err)
		}
		openapiRaw = raw
	}
	return asMap(parseJSON(t, openapiRaw))
}

// canonical renders a decoded json value deterministically (sorted keys), for
// assertion messages that are diffable.
func canonical(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return fmt.Sprintf("%v", v)
	}
	return strings.TrimRight(buf.String(), "\n")
}

// assertJSONEq compares two decoded json values, naming the failure.
func assertJSONEq(t *testing.T, label string, want, got any) bool {
	t.Helper()
	if jsonEqual(want, got) {
		return true
	}
	t.Errorf("%s: bodies differ\nwant: %s\ngot:  %s", label, canonical(want), canonical(got))
	return false
}

func jsonEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			bvv, ok := bv[k]
			if !ok || !jsonEqual(v, bvv) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}

// --- typed access into decoded json ----------------------------------------------

func asMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func asArr(v any) []any          { a, _ := v.([]any); return a }
func asStr(v any) string         { s, _ := v.(string); return s }
func asBool(v any) bool          { b, _ := v.(bool); return b }

// num converts a json.Number; tests use it for geometry and cursors.
func num(t *testing.T, v any) float64 {
	t.Helper()
	n, ok := v.(json.Number)
	if !ok {
		t.Fatalf("expected a number, got %T (%v)", v, v)
	}
	f, err := n.Float64()
	if err != nil {
		t.Fatalf("bad number %v: %v", v, err)
	}
	return f
}

// --- the binary under test ---------------------------------------------------------

// serverBin is the server command under test. ANALOG_SERVER_BIN names it; unset,
// the fixtures fall back to bin/analog-server in the checkout.
func serverBin(t *testing.T) []string {
	t.Helper()
	if raw := strings.TrimSpace(os.Getenv("ANALOG_SERVER_BIN")); raw != "" {
		return strings.Fields(raw)
	}
	def := filepath.Join(repoRoot, "bin", "analog-server")
	if _, err := os.Stat(def); err != nil {
		t.Fatalf("no server binary: %s does not exist and ANALOG_SERVER_BIN is unset.\n"+
			"  build one with:  scripts/build.sh", def)
	}
	return []string{def}
}

// server is a running server process, and an http client pointed at it. Tests use
// it exactly like the python harness's Server: s.get(t, "/api/health").
type server struct {
	t       *testing.T
	base    string
	env     []string
	secrets map[string]string
	proc    *exec.Cmd
	log     *bytes.Buffer
	http    *http.Client
}

type serverOpt func(*serverConfig)

type serverConfig struct {
	seeded bool
	tokens [][2]string
}

// withSeed loads contracts/fixtures/ by the binary's own seed command before start.
func withSeed() serverOpt { return func(c *serverConfig) { c.seeded = true } }

// withTokens mints tokens through `token add` before the process starts, the way
// an operator would. The store is re-read per request, so issuing later works too —
// TestAuth relies on it — but most tests should not depend on that.
func withTokens(pairs ...[2]string) serverOpt {
	return func(c *serverConfig) { c.tokens = append(c.tokens, pairs...) }
}

func freePort() int {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()
	return port
}

func startServer(t *testing.T, opts ...serverOpt) *server {
	t.Helper()
	var cfg serverConfig
	for _, opt := range opts {
		opt(&cfg)
	}
	root := t.TempDir()
	s := &server{
		t: t,
		env: []string{
			"ANALOG_DATA_DIR=" + root,
			"ANALOG_DB=" + filepath.Join(root, "analog.db"),
			"ANALOG_AUTH_FILE=" + filepath.Join(root, "auth.json"),
		},
		secrets: map[string]string{},
		http:    &http.Client{Timeout: 30 * time.Second},
	}
	bin := serverBin(t)

	if cfg.seeded {
		// The seed path a human runs is the seed path the tests exercise.
		run(t, append(bin, "seed",
			"--db", filepath.Join(root, "analog.db"),
			"--media-dir", filepath.Join(root, "media"),
			"--reset"), s.env)
	}
	for _, actor := range cfg.tokens {
		s.issueToken(actor[0], actor[1])
	}

	s.base = fmt.Sprintf("http://127.0.0.1:%d", freePort())
	port := s.base[strings.LastIndex(s.base, ":")+1:]
	cmd := exec.Command(bin[0], append(bin[1:],
		"--host", "127.0.0.1", "--port", port)...)
	// The checkout's working directory, like any human-run server: fixture-relative
	// paths in the seed command resolve from here.
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), s.env...)
	s.log = &bytes.Buffer{}
	cmd.Stdout, cmd.Stderr = s.log, s.log
	s.proc = cmd
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(s.close)
	s.awaitHealth()
	return s
}

func (s *server) environ() []string { return append(os.Environ(), s.env...) }

func startSeededServer(t *testing.T) *server {
	t.Helper()
	return startServer(t, withSeed())
}

func (s *server) awaitHealth() {
	t := s.t
	t.Helper()
	deadline := time.Now().Add(startupTimeout)
	for time.Now().Before(deadline) {
		if s.proc.ProcessState != nil {
			t.Fatalf("server exited before serving:\n%s", s.log.String())
		}
		resp, err := s.http.Get(s.base + "/api/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	s.close()
	t.Fatalf("server did not answer /api/health in %s", startupTimeout)
}

func (s *server) issueToken(actor, kind string) string {
	t := s.t
	t.Helper()
	out := run(t, append(serverBin(t), "token", "add", actor, "--kind", kind), s.environ())
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if tokenRE.MatchString(line) {
			s.secrets[actor] = line
			return line
		}
	}
	t.Fatalf("no token on stdout:\n%s", out)
	return ""
}

func (s *server) close() {
	if s.proc == nil || s.proc.Process == nil {
		return
	}
	_ = s.proc.Process.Kill()
	_, _ = s.proc.Process.Wait()
	s.proc = nil
}

// run executes a helper command of the binary under test and requires success.
// It runs from the repo root, where a human runs these commands.
func run(t *testing.T, cmd []string, env []string) string {
	t.Helper()
	proc := exec.Command(cmd[0], cmd[1:]...)
	proc.Dir = repoRoot
	proc.Env = env
	var out, errOut bytes.Buffer
	proc.Stdout, proc.Stderr = &out, &errOut
	if err := proc.Run(); err != nil {
		t.Fatalf("%s failed: %v\n%s\n%s", strings.Join(cmd, " "), err, out.String(), errOut.String())
	}
	return out.String()
}

// --- requests ----------------------------------------------------------------------

type response struct {
	status int
	header http.Header
	raw    []byte
	body   any // decoded with literal numbers
}

func (r *response) str() string             { return string(r.raw) }
func (r *response) obj() map[string]any     { return asMap(r.body) }
func (r *response) arr() []any              { return asArr(r.body) }
func (r *response) hasHeader(k string) bool { _, ok := r.header[http.CanonicalHeaderKey(k)]; return ok }

func (s *server) do(t *testing.T, method, path string, q url.Values,
	headers map[string]string, contentType string, body any) *response {
	t.Helper()
	if q == nil {
		q = url.Values{}
	}
	full := s.base + path
	if len(q) > 0 {
		full += "?" + q.Encode()
	}
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(b)
	default:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(body); err != nil {
			t.Fatalf("encode request body: %v", err)
		}
		reader = &buf
		if contentType == "" {
			contentType = "application/json"
		}
	}
	req, err := http.NewRequest(method, full, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("%s %s: read body: %v", method, path, err)
	}
	r := &response{status: resp.StatusCode, header: resp.Header, raw: raw}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) > 0 {
		if json.Valid(trimmed) {
			r.body = parseJSON(t, trimmed)
		}
	}
	return r
}

func (s *server) get(t *testing.T, path string, q url.Values, headers ...map[string]string) *response {
	t.Helper()
	return s.do(t, "GET", path, q, firstHeader(headers), "", nil)
}

func (s *server) post(t *testing.T, path string, q url.Values, body any,
	headers ...map[string]string) *response {
	t.Helper()
	return s.do(t, "POST", path, q, firstHeader(headers), "", body)
}

func (s *server) patch(t *testing.T, path string, q url.Values, body any,
	headers ...map[string]string) *response {
	t.Helper()
	return s.do(t, "PATCH", path, q, firstHeader(headers), "", body)
}

func (s *server) delete(t *testing.T, path string, q url.Values,
	headers ...map[string]string) *response {
	t.Helper()
	return s.do(t, "DELETE", path, q, firstHeader(headers), "", nil)
}

func (s *server) options(t *testing.T, path string, headers map[string]string) *response {
	t.Helper()
	return s.do(t, "OPTIONS", path, nil, headers, "", nil)
}

func firstHeader(hs []map[string]string) map[string]string {
	if len(hs) > 0 {
		return hs[0]
	}
	return nil
}

// --- request helpers ---------------------------------------------------------------
// openapi.json puts actor/actor_kind in the query string. SPEC §3 also permits
// headers; the contract form is what these tests assert.

func params(kv ...string) url.Values {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return v
}

func actorParams(actor, kind string) url.Values {
	return params("actor", actor, "actor_kind", kind)
}

func humanP() url.Values { return actorParams("human", "human") }
func agentP() url.Values { return actorParams("claude-code", "agent") }

// mergeParams is the {**AGENT, "mode": "branch"} form.
func mergeParams(base url.Values, kv ...string) url.Values {
	out := url.Values{}
	for k, vs := range base {
		out[k] = append([]string(nil), vs...)
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out.Set(kv[i], kv[i+1])
	}
	return out
}

func bearer(s *server, actor string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + s.secrets[actor]}
}

// --- shared request shapes ----------------------------------------------------------

func makeSpace(t *testing.T, s *server, slug, title string, revisionMode string) map[string]any {
	t.Helper()
	body := map[string]any{"slug": slug, "title": title}
	if revisionMode != "" {
		body["revision_mode"] = revisionMode
	}
	r := s.post(t, "/api/spaces", humanP(), body)
	if r.status != 201 {
		t.Fatalf("make_space %s: %d %s", slug, r.status, r.str())
	}
	return r.obj()
}

func addCards(t *testing.T, s *server, slug string, cards []any, actor url.Values) []any {
	t.Helper()
	if actor == nil {
		actor = agentP()
	}
	r := s.post(t, "/api/spaces/"+slug+"/cards", actor, map[string]any{"cards": cards})
	if r.status != 201 {
		t.Fatalf("add_cards %s: %d %s", slug, r.status, r.str())
	}
	return r.arr()
}

// oneCard is addCards with a single card; extra card fields ride as kv pairs.
func oneCard(t *testing.T, s *server, slug string, kv ...string) map[string]any {
	t.Helper()
	card := map[string]any{"title": "Card", "content": "body", "kind": "md"}
	for i := 0; i+1 < len(kv); i += 2 {
		card[kv[i]] = parseJSON(t, []byte(kv[i+1]))
	}
	return asMap(addCards(t, s, slug, []any{card}, nil)[0])
}

func eventsOf(t *testing.T, s *server, slug string, since string) []any {
	t.Helper()
	r := s.get(t, "/api/spaces/"+slug+"/events", params("since", since))
	if r.status != 200 {
		t.Fatalf("events %s: %d %s", slug, r.status, r.str())
	}
	return asArr(r.obj()["events"])
}
