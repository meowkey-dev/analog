package client

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The Go rewrite of tests/unit/test_client.py: no server, a stub transport that
// serves contracts/fixtures/ and records requests.

// --- fixtures ------------------------------------------------------------------

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("could not find the repository root")
	return ""
}

func fixture(t *testing.T, name string) json.RawMessage {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "contracts", "fixtures", name))
	if err != nil {
		t.Fatal(err)
	}
	return json.RawMessage(raw)
}

func decodeFixture[T any](t *testing.T, name string) T {
	t.Helper()
	var out T
	dec := json.NewDecoder(strings.NewReader(string(fixture(t, name))))
	dec.UseNumber()
	if err := dec.Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

// --- the stub transport ---------------------------------------------------------

type recorded struct {
	Method  string
	Path    string
	Params  url.Values
	Headers http.Header
	Body    string
}

type stub struct {
	t       *testing.T
	calls   *[]recorded
	routes  map[string]json.RawMessage
	handler func(*http.Request) (*http.Response, error)
}

func (s stub) RoundTrip(r *http.Request) (*http.Response, error) {
	body := ""
	if r.Body != nil {
		raw := make([]byte, 1<<20)
		n, _ := r.Body.Read(raw)
		body = string(raw[:n])
	}
	*s.calls = append(*s.calls, recorded{Method: r.Method, Path: r.URL.Path,
		Params: r.URL.Query(), Headers: r.Header.Clone(), Body: body})
	if s.handler != nil {
		return s.handler(r)
	}
	payload, ok := s.routes[r.Method+" "+r.URL.Path]
	if !ok {
		return respond(404, `{"error":"not_found","message":"`+r.URL.Path+`"}`), nil
	}
	if payload == nil {
		return respond(204, ""), nil
	}
	status := 200
	if r.Method == "POST" {
		status = 201
	}
	return respond(status, string(payload)), nil
}

func respond(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func routesFor(t *testing.T) map[string]json.RawMessage {
	canvas := fixture(t, "canvas.json")
	space := fixture(t, "space.json")
	annotations := fixture(t, "annotations.json")
	events := fixture(t, "events.json")
	feedback := fixture(t, "feedback.claude-code.since-12.json")

	nodes := decodeFixture[Canvas](t, "canvas.json").Nodes
	firstNode, _ := json.Marshal(nodes[0])
	firstNodeList, _ := json.Marshal(nodes[:1])
	edges := decodeFixture[Canvas](t, "canvas.json").Edges
	firstEdgeList, _ := json.Marshal(edges[:1])
	firstAnnotation, _ := json.Marshal(decodeFixture[[]Annotation](t, "annotations.json")[0])
	spaceList, _ := json.Marshal([]json.RawMessage{space})
	importResult, _ := json.Marshal(map[string]any{
		"id_map": map[string]string{}, "canvas": json.RawMessage(canvas)})

	return map[string]json.RawMessage{
		"GET /api/spaces":                            spaceList,
		"POST /api/spaces":                           space,
		"GET /api/spaces/redesign":                   space,
		"PATCH /api/spaces/redesign":                 space,
		"DELETE /api/spaces/redesign":                nil,
		"GET /api/spaces/redesign/canvas":            canvas,
		"POST /api/spaces/redesign/import":           importResult,
		"POST /api/spaces/redesign/cards":            firstNodeList,
		"PATCH /api/spaces/redesign/cards/c_opt_a":   firstNode,
		"DELETE /api/spaces/redesign/cards/c_opt_a":  nil,
		"POST /api/spaces/redesign/links":            firstEdgeList,
		"DELETE /api/spaces/redesign/links/l_1":      nil,
		"GET /api/spaces/redesign/annotations":       annotations,
		"POST /api/spaces/redesign/annotations":      firstAnnotation,
		"PATCH /api/spaces/redesign/annotations/a_1": firstAnnotation,
		"GET /api/spaces/redesign/feedback":          feedback,
		"GET /api/spaces/redesign/events":            events,
		"POST /api/spaces/redesign/media": json.RawMessage(
			`{"url":"/api/spaces/redesign/media/m_9.png","content_type":"image/png","bytes":4}`),
	}
}

func newTestClient(t *testing.T) (*Client, *[]recorded) {
	t.Helper()
	calls := &[]recorded{}
	c := New(Options{URL: "http://testserver", Actor: "claude-code", ActorKind: "agent",
		Config:    map[string]string{},
		Transport: stub{t: t, calls: calls, routes: routesFor(t)}})
	c.sleep = func(time.Duration) {}
	return c, calls
}

// --- coverage of §3 ---------------------------------------------------------------

func TestEveryEndpointIsCallable(t *testing.T) {
	api, calls := newTestClient(t)

	if spaces, err := api.ListSpaces(); err != nil || len(spaces) != 1 {
		t.Fatalf("ListSpaces = %v, %v", spaces, err)
	}
	if space, err := api.CreateSpace("redesign", "T", ""); err != nil || space.Slug != "redesign" {
		t.Fatalf("CreateSpace = %+v, %v", space, err)
	}
	must(t, err2(api.GetSpace("redesign")))
	must(t, err2(api.UpdateSpace("redesign", map[string]any{"title": "T2"})))
	must(t, api.DeleteSpace("redesign"))
	must(t, err2(api.GetCanvas("redesign", false)))
	must(t, err2(api.ImportCanvas("redesign", decodeFixture[Canvas](t, "canvas.json"))))

	nodes, err := api.CreateCards("redesign", []CardDraft{{Title: "T", Content: "c"}})
	if err != nil || nodes[0]["id"] != "c_opt_a" {
		t.Fatalf("CreateCards = %v, %v", nodes, err)
	}
	must(t, err2(api.CreateNodes("redesign", decodeFixture[Canvas](t, "canvas.json").Nodes[:1])))
	must(t, err2(api.UpdateCard("redesign", "c_opt_a", map[string]any{"text": "x"}, "", nil)))
	must(t, api.DeleteCard("redesign", "c_opt_a"))
	must(t, err2(api.CreateLinks("redesign", []Edge{{"fromNode": "a", "toNode": "b"}})))
	must(t, err2(api.LinkCards("redesign", "a", "b", "why")))
	must(t, api.DeleteLink("redesign", "l_1"))
	must(t, err2(api.ListAnnotations("redesign", nil, "")))
	must(t, err2(api.CreateAnnotation("redesign", "c_chart", "b", nil, "")))
	reply := "fixed"
	must(t, err2(api.ResolveAnnotation("redesign", "a_1", &reply, true)))
	must(t, err2(api.GetFeedback("redesign", nil, true)))
	must(t, err2(api.ListEvents("redesign", 0, 0)))

	png := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(png, []byte("\x89PNG"), 0o644); err != nil {
		t.Fatal(err)
	}
	media, err := api.UploadMedia("redesign", png, "")
	if err != nil || !strings.HasSuffix(media.URL, ".png") {
		t.Fatalf("UploadMedia = %+v, %v", media, err)
	}

	// Every §3 operation, and nothing left untested.
	seen := map[string]bool{}
	for _, c := range *calls {
		seen[c.Method+" "+c.Path] = true
	}
	for route := range routesFor(t) {
		if !seen[route] {
			t.Errorf("%s is never exercised", route)
		}
	}
	if len(seen) != len(routesFor(t)) {
		t.Errorf("called %d routes, the table has %d", len(seen), len(routesFor(t)))
	}
}

// --- actor plumbing (SPEC §2.2) ------------------------------------------------------

func TestEveryMutationCarriesActorAndKind(t *testing.T) {
	api, calls := newTestClient(t)
	must(t, err2(api.CreateSpace("redesign", "T", "")))
	must(t, err2(api.CreateCards("redesign", []CardDraft{{Title: "T", Content: "c"}})))
	must(t, err2(api.UpdateCard("redesign", "c_opt_a", map[string]any{"text": "x"}, "", nil)))
	must(t, api.DeleteCard("redesign", "c_opt_a"))
	must(t, err2(api.CreateAnnotation("redesign", "c_chart", "b", nil, "")))
	for _, c := range *calls {
		if c.Params.Get("actor") != "claude-code" || c.Params.Get("actor_kind") != "agent" {
			t.Errorf("%s %s carried %v", c.Method, c.Path, c.Params)
		}
	}
}

func TestFeedbackSendsActorButNotActorKind(t *testing.T) {
	api, calls := newTestClient(t)
	must(t, err2(api.GetFeedback("redesign", nil, true)))
	last := (*calls)[len(*calls)-1]
	if last.Params.Get("actor") != "claude-code" {
		t.Error("feedback must identify the actor: a cursor is keyed by actor name")
	}
	if last.Params.Has("actor_kind") {
		t.Error("feedback is a read; actor_kind is not part of it")
	}
}

func TestReadsSendNoActor(t *testing.T) {
	api, calls := newTestClient(t)
	must(t, err2(api.GetCanvas("redesign", false)))
	must(t, err2(api.ListAnnotations("redesign", nil, "")))
	must(t, err2(api.ListEvents("redesign", 0, 0)))
	for _, c := range *calls {
		if c.Params.Has("actor") {
			t.Errorf("%s %s sent an actor", c.Method, c.Path)
		}
	}
}

func TestAnUnconfiguredActorFailsBeforeTheRequest(t *testing.T) {
	calls := &[]recorded{}
	api := New(Options{URL: "http://testserver", Config: map[string]string{},
		Transport: stub{t: t, calls: calls,
			handler: func(*http.Request) (*http.Response, error) {
				return respond(200, "{}"), nil
			}}})
	_, err := api.CreateCards("redesign", []CardDraft{{Title: "T", Content: "c"}})
	if !Is(err, CodeActorRequired) {
		t.Fatalf("err = %v, want actor_required", err)
	}
	if len(*calls) != 0 {
		t.Error("SPEC §10: fail loudly, do not write anonymously")
	}
}

func TestReadsStillWorkWithoutAnActor(t *testing.T) {
	api, _ := newTestClient(t)
	api.Actor = ""
	if _, err := api.GetCanvas("redesign", false); err != nil {
		t.Fatalf("a read needed an actor: %v", err)
	}
}

// --- parameters ------------------------------------------------------------------------

func TestModeAndIfMatchAreForwarded(t *testing.T) {
	api, calls := newTestClient(t)
	rev := int64(3)
	must(t, err2(api.UpdateCard("redesign", "c_opt_a", map[string]any{"text": "x"},
		"branch", &rev)))
	last := (*calls)[len(*calls)-1]
	if last.Params.Get("mode") != "branch" {
		t.Errorf("mode = %q", last.Params.Get("mode"))
	}
	if last.Headers.Get("If-Match") != "3" {
		t.Errorf("If-Match = %q", last.Headers.Get("If-Match"))
	}
}

func TestFeedbackSinceAndAdvance(t *testing.T) {
	api, calls := newTestClient(t)
	since := int64(12)
	must(t, err2(api.GetFeedback("redesign", &since, false)))
	last := (*calls)[len(*calls)-1]
	if last.Params.Get("since") != "12" || last.Params.Get("advance") != "false" {
		t.Errorf("params = %v", last.Params)
	}

	must(t, err2(api.GetFeedback("redesign", nil, true)))
	last = (*calls)[len(*calls)-1]
	if last.Params.Has("since") {
		t.Error("omitting since must leave the server's cursor to decide")
	}
	if last.Params.Get("advance") != "true" {
		t.Errorf("advance = %q", last.Params.Get("advance"))
	}
}

func TestAnnotationFilters(t *testing.T) {
	api, calls := newTestClient(t)
	open := false
	must(t, err2(api.ListAnnotations("redesign", &open, "c_chart")))
	last := (*calls)[len(*calls)-1]
	if last.Params.Get("resolved") != "false" || last.Params.Get("card_id") != "c_chart" {
		t.Errorf("params = %v", last.Params)
	}
}

func TestIncludeDeleted(t *testing.T) {
	api, calls := newTestClient(t)
	must(t, err2(api.GetCanvas("redesign", true)))
	if (*calls)[len(*calls)-1].Params.Get("include_deleted") != "true" {
		t.Error("include_deleted was not forwarded")
	}
}

// --- errors ------------------------------------------------------------------------------

func TestErrorCodesAreSurfaced(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   string
	}{
		{404, CodeNotFound},
		{409, CodeConflict},
		{400, CodeActorRequired},
		{400, CodeValidationFailed},
		{400, CodeUnsupportedKind},
	} {
		calls := &[]recorded{}
		api := New(Options{URL: "http://testserver", Actor: "a",
			Config: map[string]string{},
			Transport: stub{t: t, calls: calls,
				handler: func(*http.Request) (*http.Response, error) {
					return respond(tc.status,
						`{"error":"`+tc.code+`","message":"nope"}`), nil
				}}})
		_, err := api.GetCanvas("redesign", false)
		e, ok := As(err)
		if !ok {
			t.Fatalf("%s did not produce a client error: %v", tc.code, err)
		}
		if e.Status != tc.status || e.Code != tc.code {
			t.Errorf("got %d/%s, want %d/%s", e.Status, e.Code, tc.status, tc.code)
		}
		// unsupported_kind is a validation failure with a friendlier name.
		if tc.code == CodeUnsupportedKind && !Is(err, CodeValidationFailed) {
			t.Error("unsupported_kind must read as a validation failure")
		}
	}
}

func TestConflictExposesTheCurrentNode(t *testing.T) {
	node := decodeFixture[Canvas](t, "canvas.json").Nodes[0]
	body, _ := json.Marshal(map[string]any{
		"error": "conflict", "message": "stale", "current": node})
	calls := &[]recorded{}
	api := New(Options{URL: "http://testserver", Actor: "a", Config: map[string]string{},
		Transport: stub{t: t, calls: calls,
			handler: func(*http.Request) (*http.Response, error) {
				return respond(409, string(body)), nil
			}}})
	rev := int64(1)
	_, err := api.UpdateCard("redesign", "c_opt_a", map[string]any{"text": "x"}, "", &rev)
	e, ok := As(err)
	if !ok || e.Code != CodeConflict {
		t.Fatalf("err = %v, want a conflict", err)
	}
	if current := e.Current(); current == nil || current["id"] != "c_opt_a" {
		t.Errorf("Current() = %v; SPEC §3 surfaces a conflict, never auto-resolves it", current)
	}
}

// --- retry -------------------------------------------------------------------------------

func TestRetriesOnceOnAConnectionError(t *testing.T) {
	calls := &[]recorded{}
	canvas := fixture(t, "canvas.json")
	api := New(Options{URL: "http://testserver", Actor: "a", Config: map[string]string{},
		Transport: stub{t: t, calls: calls,
			handler: func(*http.Request) (*http.Response, error) {
				if len(*calls) == 1 {
					return nil, errors.New("connection refused")
				}
				return respond(200, string(canvas)), nil
			}}})
	api.sleep = func(time.Duration) {}
	if _, err := api.GetCanvas("redesign", false); err != nil {
		t.Fatalf("the retry did not recover: %v", err)
	}
	if len(*calls) != 2 {
		t.Errorf("attempts = %d, want 2", len(*calls))
	}
}

func TestGivesUpAfterTheSecondConnectionError(t *testing.T) {
	calls := &[]recorded{}
	api := New(Options{URL: "http://testserver", Actor: "a", Config: map[string]string{},
		Transport: stub{t: t, calls: calls,
			handler: func(*http.Request) (*http.Response, error) {
				return nil, errors.New("connection refused")
			}}})
	api.sleep = func(time.Duration) {}
	_, err := api.GetCanvas("redesign", false)
	if err == nil {
		t.Fatal("an unreachable server must be an error")
	}
	if len(*calls) != 2 {
		t.Errorf("attempts = %d, want 2", len(*calls))
	}
	if !strings.Contains(err.Error(), "cannot reach") {
		t.Errorf("err = %v, want it to say the server is unreachable", err)
	}
}

func TestHTTPErrorsAreNotRetried(t *testing.T) {
	calls := &[]recorded{}
	api := New(Options{URL: "http://testserver", Actor: "a", Config: map[string]string{},
		Transport: stub{t: t, calls: calls,
			handler: func(*http.Request) (*http.Response, error) {
				return respond(404, `{"error":"not_found","message":"x"}`), nil
			}}})
	api.sleep = func(time.Duration) {}
	if _, err := api.GetCanvas("redesign", false); !Is(err, CodeNotFound) {
		t.Fatalf("err = %v, want not_found", err)
	}
	if len(*calls) != 1 {
		t.Errorf("attempts = %d; a status is an answer, not a failure to reach", len(*calls))
	}
}

// --- config (SPEC §4.2) --------------------------------------------------------------------

func TestBaseURLNormalizes(t *testing.T) {
	for _, tc := range []struct{ given, want string }{
		{"http://127.0.0.1:8787", "http://127.0.0.1:8787/api"},
		{"http://127.0.0.1:8787/", "http://127.0.0.1:8787/api"},
		{"http://127.0.0.1:8787/api", "http://127.0.0.1:8787/api"},
		{"http://127.0.0.1:8787/api/", "http://127.0.0.1:8787/api"},
	} {
		if got := NormalizeBase(tc.given); got != tc.want {
			t.Errorf("NormalizeBase(%q) = %q, want %q", tc.given, got, tc.want)
		}
	}
}

func TestEnvConfiguresURLActorAndKind(t *testing.T) {
	t.Setenv("ANALOG_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("ANALOG_URL", "http://elsewhere:9000")
	t.Setenv("ANALOG_ACTOR", "researcher-1")
	t.Setenv("ANALOG_ACTOR_KIND", "agent")
	api := New(Options{})
	if api.Base != "http://elsewhere:9000/api" {
		t.Errorf("Base = %q", api.Base)
	}
	if api.Actor != "researcher-1" || api.ActorKind != "agent" {
		t.Errorf("actor = %q/%q", api.Actor, api.ActorKind)
	}
}

func TestTOMLConfigIsReadAndEnvWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".analog.toml")
	if err := os.WriteFile(path,
		[]byte("url = \"http://from-toml:1234\"\nactor = \"from-toml\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANALOG_CONFIG", path)
	t.Setenv("ANALOG_URL", "")
	t.Setenv("ANALOG_ACTOR", "")
	if actor := New(Options{}).Actor; actor != "from-toml" {
		t.Errorf("actor = %q, want from-toml", actor)
	}
	t.Setenv("ANALOG_ACTOR", "from-env")
	if actor := New(Options{}).Actor; actor != "from-env" {
		t.Errorf("actor = %q; the environment must win over the file", actor)
	}
}

func TestDefaultURLIsTheContractBase(t *testing.T) {
	t.Setenv("ANALOG_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("ANALOG_URL", "")
	t.Setenv("ANALOG_WEB_URL", "")
	api := New(Options{Actor: "a"})
	if api.Base != "http://127.0.0.1:8787/api" {
		t.Errorf("Base = %q", api.Base)
	}
	if got := api.SpaceURL("redesign"); got != "http://127.0.0.1:8787/s/redesign" {
		t.Errorf("SpaceURL = %q", got)
	}
}

// --- SSE parsing ---------------------------------------------------------------------------

func TestSSEMessagesAreParsedAndCommentsIgnored(t *testing.T) {
	stream := strings.Join([]string{
		": connected", "",
		"id: 1", "event: card.created", `data: {"seq": 1, "type": "card.created"}`, "",
		": keepalive", "",
		"id: 2", `data: {"seq": 2, "type": "card.moved"}`, "",
		// A message is terminated by a blank line, so the stream ends with one.
		// A truncated final message is not delivered, and must not be.
		"",
	}, "\n")
	var seqs []int64
	if err := forEachSSEMessage(strings.NewReader(stream), func(e Event) error {
		seqs = append(seqs, e.Seq)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seqs) != 2 || seqs[0] != 1 || seqs[1] != 2 {
		t.Errorf("seqs = %v, want [1 2]", seqs)
	}
}

// --- cross-space lookup ----------------------------------------------------------------------

func TestFindAnnotationScansSpaces(t *testing.T) {
	api, _ := newTestClient(t)
	slug, annotation, err := api.FindAnnotation("a_2")
	if err != nil {
		t.Fatal(err)
	}
	if slug != "redesign" {
		t.Errorf("slug = %q", slug)
	}
	if !strings.HasPrefix(annotation.Body, "rewrite cost") {
		t.Errorf("body = %q", annotation.Body)
	}
}

func TestFindAnnotationReportsAnAbsentOne(t *testing.T) {
	api, _ := newTestClient(t)
	if _, _, err := api.FindAnnotation("a_missing"); !Is(err, CodeNotFound) {
		t.Errorf("err = %v, want not_found", err)
	}
}

func TestGetMediaFetchesBytesNotJSON(t *testing.T) {
	calls := &[]recorded{}
	png := "\x89PNG\r\n"
	api := New(Options{URL: "http://testserver", Actor: "claude-code", Config: map[string]string{},
		Token: "tok",
		Transport: stub{t: t, calls: calls, handler: func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: 200,
				Header:     http.Header{"Content-Type": []string{"image/png"}},
				Body:       io.NopCloser(strings.NewReader(png)),
			}, nil
		}}})
	data, ctype, err := api.GetMedia("/api/spaces/redesign/media/m_01.png")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != png || ctype != "image/png" {
		t.Fatalf("got %q %q", data, ctype)
	}
	if (*calls)[0].Path != "/api/spaces/redesign/media/m_01.png" {
		t.Errorf("path = %s", (*calls)[0].Path)
	}
	if got := (*calls)[0].Headers.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("Authorization = %q", got)
	}
}

func TestGetMediaSurfaces404(t *testing.T) {
	calls := &[]recorded{}
	api := New(Options{URL: "http://testserver", Actor: "claude-code", Config: map[string]string{},
		Transport: stub{t: t, calls: calls, handler: func(*http.Request) (*http.Response, error) {
			return respond(404, `{"error":"not_found","message":"nope"}`), nil
		}}})
	_, _, err := api.GetMedia("/api/spaces/redesign/media/missing.png")
	if !Is(err, CodeNotFound) {
		t.Errorf("err = %v, want not_found", err)
	}
}

func TestGetMediaDoesNotSendBearerToExternalURL(t *testing.T) {
	var authorization string
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	defer external.Close()

	api := New(Options{URL: "http://analog.example", Config: map[string]string{}, Token: "secret"})
	data, _, err := api.GetMedia(external.URL + "/image.png")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "png" {
		t.Fatalf("data = %q", data)
	}
	if authorization != "" {
		t.Fatalf("external media received Authorization %q", authorization)
	}
}

func TestGetMediaStripsBearerWhenMediaRedirectsAway(t *testing.T) {
	var authorization string
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png"))
	}))
	defer external.Close()

	analog := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL+"/redirected.png", http.StatusFound)
	}))
	defer analog.Close()

	api := New(Options{URL: analog.URL, Config: map[string]string{}, Token: "secret"})
	data, _, err := api.GetMedia("/api/spaces/redesign/media/m_01.png")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "png" {
		t.Fatalf("data = %q", data)
	}
	if authorization != "" {
		t.Fatalf("redirect target received Authorization %q", authorization)
	}
}

func TestGetMediaCapsRedirects(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		time.Sleep(time.Millisecond)
		http.Redirect(w, r, r.URL.String(), http.StatusFound)
	}))
	defer server.Close()

	api := New(Options{URL: server.URL, Config: map[string]string{}, Token: "secret", Timeout: 500 * time.Millisecond})
	api.sleep = func(time.Duration) {}
	_, _, err := api.GetMedia("/api/spaces/redesign/media/m_01.png")
	if err == nil {
		t.Fatal("redirect loop unexpectedly succeeded")
	}
	if requests > maxMediaRedirects*2+2 {
		t.Fatalf("redirect loop made %d requests; cap is %d per attempt", requests, maxMediaRedirects)
	}
}

// --- helpers ------------------------------------------------------------------------------------

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// err2 drops the value from a (T, error) pair so `must` can take it.
func err2[T any](_ T, err error) error { return err }
