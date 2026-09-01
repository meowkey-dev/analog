// Per-actor bearer tokens, over HTTP.
//
// SPEC §3 said "a single shared bearer token ... when you first expose it beyond
// localhost". A shared token gatekeeps the server but not identity, and §2.2/§10
// make `actor` load-bearing: the event log is only worth having if attribution is
// true. So a token names exactly one actor, and the server checks the claim rather
// than taking it.
//
// The store itself is exercised in internal/auth's Go tests; everything here goes
// through the socket, so it judges any implementation.
package conformance

import (
	"testing"
)

// securedServer starts a server with two actors configured. Tokens are minted
// through the binary's own `token add`, so the auth file is written by the
// implementation under test rather than by this suite.
func securedServer(t *testing.T) *server {
	t.Helper()
	return startServer(t, withTokens([2]string{"kai", "human"}, [2]string{"claude-code", "agent"}))
}

// health

func TestAuth_HealthOnAnOpenServer(t *testing.T) {
	s := startServer(t)
	assertJSONEq(t, "health",
		jlit(t, `{"ok": true, "service": "analog", "version": "0.6.0", "auth_required": false}`),
		s.get(t, "/api/health", nil).body)
}

func TestAuth_HealthNeedsNoTokenAndSaysOneIsNeeded(t *testing.T) {
	s := securedServer(t)
	r := s.get(t, "/api/health", nil)
	if r.status != 200 {
		t.Fatalf("%d: a client must be able to discover the server first", r.status)
	}
	if !asBool(r.obj()["auth_required"]) {
		t.Errorf("auth_required = %v", r.obj()["auth_required"])
	}
}

// 401

func TestAuth_ReadsRequireATokenOnceOneExists(t *testing.T) {
	s := securedServer(t)
	for _, path := range []string{
		"/api/spaces", "/api/spaces/demo", "/api/spaces/demo/canvas",
		"/api/spaces/demo/annotations", "/api/spaces/demo/events",
	} {
		t.Run(path, func(t *testing.T) {
			r := s.get(t, path, nil)
			if r.status != 401 {
				t.Fatalf("%s was readable without a token: %d", path, r.status)
			}
			if asStr(r.obj()["error"]) != "unauthorized" {
				t.Errorf("error = %v", r.obj()["error"])
			}
			if got := r.header.Get("WWW-Authenticate"); got != "Bearer" {
				t.Errorf("www-authenticate = %q", got)
			}
		})
	}
}

func TestAuth_WritesRequireAToken(t *testing.T) {
	s := securedServer(t)
	r := s.post(t, "/api/spaces", humanP(), map[string]any{"slug": "x", "title": "X"})
	if r.status != 401 {
		t.Fatalf("%d %s", r.status, r.str())
	}
}

func TestAuth_ABadTokenIs401(t *testing.T) {
	s := securedServer(t)
	for _, header := range []map[string]string{
		{"Authorization": "Bearer analog_wrong"},
		{"Authorization": "Basic analog_wrong"},
		{"Authorization": "Bearer"},
	} {
		t.Run(header["Authorization"], func(t *testing.T) {
			if r := s.get(t, "/api/spaces", nil, header); r.status != 401 {
				t.Fatalf("%d", r.status)
			}
		})
	}
}

func TestAuth_AValidTokenGetsThrough(t *testing.T) {
	s := securedServer(t)
	r := s.get(t, "/api/spaces", nil, bearer(s, "kai"))
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if got := r.arr(); len(got) != 0 {
		t.Errorf("spaces = %v", canonical(got))
	}
}

// whoami

func TestAuth_WhoamiReportsTheTokenIdentity(t *testing.T) {
	s := securedServer(t)
	assertJSONEq(t, "agent whoami",
		jlit(t, `{"authenticated": true, "actor": "claude-code", "actor_kind": "agent"}`),
		s.get(t, "/api/whoami", nil, bearer(s, "claude-code")).body)
	assertJSONEq(t, "human whoami",
		jlit(t, `{"authenticated": true, "actor": "kai", "actor_kind": "human"}`),
		s.get(t, "/api/whoami", nil, bearer(s, "kai")).body)
}

func TestAuth_WhoamiOnAnOpenServer(t *testing.T) {
	s := startServer(t)
	assertJSONEq(t, "whoami",
		jlit(t, `{"authenticated": false, "actor": null, "actor_kind": null}`),
		s.get(t, "/api/whoami", nil).body)
}

// attribution

func TestAuth_TheTokenDecidesWhoYouAre(t *testing.T) {
	// The point of per-actor tokens: a claim that disagrees with the token loses.
	s := securedServer(t)
	r := s.post(t, "/api/spaces", params("actor", "kai", "actor_kind", "human"),
		map[string]any{"slug": "demo", "title": "Demo"}, bearer(s, "kai"))
	if r.status != 201 {
		t.Fatalf("%d %s", r.status, r.str())
	}

	impersonation := s.post(t, "/api/spaces/demo/cards",
		agentP(), // claims to be claude-code
		map[string]any{"cards": []any{map[string]any{"title": "T", "content": "c"}}},
		bearer(s, "kai")) // but holds kai's token
	if impersonation.status != 403 {
		t.Fatalf("%d %s", impersonation.status, impersonation.str())
	}
	if asStr(impersonation.obj()["error"]) != "forbidden" {
		t.Errorf("error = %v", impersonation.obj()["error"])
	}
	if !contains(impersonation.obj()["message"], "claude-code") {
		t.Errorf("message = %v", impersonation.obj()["message"])
	}
}

func TestAuth_AMatchingClaimIsAcceptedAndAttributed(t *testing.T) {
	s := securedServer(t)
	s.post(t, "/api/spaces", params("actor", "kai", "actor_kind", "human"),
		map[string]any{"slug": "demo", "title": "Demo"}, bearer(s, "kai"))
	node := asMap(s.post(t, "/api/spaces/demo/cards", agentP(),
		map[string]any{"cards": []any{map[string]any{"title": "T", "content": "c"}}},
		bearer(s, "claude-code")).arr()[0])
	if asStr(node["sp_created_by"]) != "claude-code" {
		t.Errorf("sp_created_by = %v", node["sp_created_by"])
	}

	log := s.get(t, "/api/spaces/demo/events", nil, bearer(s, "kai")).obj()["events"]
	pairs := map[[2]string]bool{}
	for _, e := range asArr(log) {
		ev := asMap(e)
		pairs[[2]string{asStr(ev["type"]), asStr(ev["actor"])}] = true
	}
	for _, want := range [][2]string{{"space.created", "kai"}, {"card.created", "claude-code"}} {
		if !pairs[want] {
			t.Errorf("log is missing (%s, %s): %v", want[0], want[1], canonical(log))
		}
	}
}

func TestAuth_TheActorParamsAreStillRequired(t *testing.T) {
	// Not inferred from the token: SPEC §10 wants a misconfigured agent to fail
	// loudly, and a silently corrected actor is not loud.
	s := securedServer(t)
	r := s.post(t, "/api/spaces", nil, map[string]any{"slug": "demo", "title": "D"},
		bearer(s, "kai"))
	if r.status != 400 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if asStr(r.obj()["error"]) != "actor_required" {
		t.Errorf("error = %v", r.obj()["error"])
	}
}

func TestAuth_KindMustMatchToo(t *testing.T) {
	s := securedServer(t)
	s.post(t, "/api/spaces", params("actor", "kai", "actor_kind", "human"),
		map[string]any{"slug": "demo", "title": "D"}, bearer(s, "kai"))
	r := s.post(t, "/api/spaces/demo/cards",
		params("actor", "kai", "actor_kind", "agent"),
		map[string]any{"cards": []any{map[string]any{"title": "T", "content": "c"}}},
		bearer(s, "kai"))
	if r.status != 403 {
		t.Fatalf("%d %s", r.status, r.str())
	}
}

// the surfaces that cannot send a header

func TestAuth_MediaIsNotReadableWithoutAToken(t *testing.T) {
	// An <img src> cannot carry a header, so the web client fetches media itself
	// and makes a blob URL. What must not happen is media being world-readable.
	s := securedServer(t)
	human := params("actor", "kai", "actor_kind", "human")
	s.post(t, "/api/spaces", human, map[string]any{"slug": "demo", "title": "D"}, bearer(s, "kai"))
	uploaded := asMap(uploadTo(t, s, "/api/spaces/demo/media", "a.png", "image/png",
		[]byte("\x89PNG"), human, bearer(s, "kai")).body)

	if r := s.get(t, asStr(uploaded["url"]), nil); r.status != 401 {
		t.Errorf("unauthenticated media GET: %d, want 401", r.status)
	}
	served := s.get(t, asStr(uploaded["url"]), nil, bearer(s, "kai"))
	if served.status != 200 || string(served.raw) != "\x89PNG" {
		t.Errorf("authenticated media GET: %d %q", served.status, served.raw)
	}
}

func TestAuth_TheEventStreamNeedsAToken(t *testing.T) {
	s := securedServer(t)
	human := params("actor", "kai", "actor_kind", "human")
	s.post(t, "/api/spaces", human, map[string]any{"slug": "demo", "title": "D"}, bearer(s, "kai"))
	if r := s.get(t, "/api/spaces/demo/events/stream", nil); r.status != 401 {
		t.Fatalf("%d", r.status)
	}
}

// CORS

func TestAuth_ACorsPreflightIsNeverGated(t *testing.T) {
	// A browser sends OPTIONS with no Authorization header. Rejecting it turns
	// every real 401 into an opaque network error.
	s := securedServer(t)
	r := s.options(t, "/api/spaces", map[string]string{
		"Origin":                         "http://localhost:5173",
		"Access-Control-Request-Method":  "GET",
		"Access-Control-Request-Headers": "authorization",
	})
	if r.status != 200 {
		t.Fatalf("%d %s", r.status, r.str())
	}
	if got := r.header.Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("access-control-allow-origin = %q", got)
	}
}

func TestAuth_A401StillCarriesCorsHeaders(t *testing.T) {
	// Otherwise the browser reports a network error and the user sees nothing.
	s := securedServer(t)
	r := s.get(t, "/api/spaces", nil, map[string]string{"Origin": "http://localhost:5173"})
	if r.status != 401 {
		t.Fatalf("%d", r.status)
	}
	if got := r.header.Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("access-control-allow-origin = %q", got)
	}
}

func TestAuth_TheTauriOriginIsAllowedByDefault(t *testing.T) {
	// The Tauri shell loads the UI from its own scheme, so a server it talks to has
	// to allow that origin out of the box or the desktop app cannot reach it.
	s := securedServer(t)
	r := s.get(t, "/api/health", nil, map[string]string{"Origin": "tauri://localhost"})
	if got := r.header.Get("Access-Control-Allow-Origin"); got != "tauri://localhost" {
		t.Errorf("access-control-allow-origin = %q", got)
	}
}

func TestAuth_LoopbackOriginsAreAllowedByDefault(t *testing.T) {
	// The desktop app serves its UI from a local sidecar, so a remote server sees
	// cross-origin requests from http://127.0.0.1:<port> / localhost:<port> — ports
	// it cannot know in advance (#42). Browsers set Origin truthfully, so a loopback
	// origin can only come from a page served on the user's machine.
	s := securedServer(t)
	for _, origin := range []string{"http://127.0.0.1:51468", "http://localhost:51468"} {
		r := s.get(t, "/api/health", nil, map[string]string{"Origin": origin})
		if got := r.header.Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("origin %q: got allow-origin %q", origin, got)
		}
	}

	// a suffix or another scheme is not loopback, and preflight for a disallowed
	// origin is refused outright
	for _, denied := range []string{"http://localhost.evil.example", "https://localhost:51468"} {
		r := s.get(t, "/api/health", nil, map[string]string{"Origin": denied})
		if r.hasHeader("Access-Control-Allow-Origin") {
			t.Errorf("origin %q was allowed", denied)
		}
	}

	r := s.options(t, "/api/health", map[string]string{
		"Origin":                        "http://127.0.0.1:51468",
		"Access-Control-Request-Method": "GET",
	})
	if r.status != 200 {
		t.Fatalf("preflight: %d", r.status)
	}
	if got := r.header.Get("Access-Control-Allow-Origin"); got != "http://127.0.0.1:51468" {
		t.Errorf("preflight allow-origin = %q", got)
	}
}

// an open server keeps behaving exactly as it did

func TestAuth_AnOpenServerIsUnchanged(t *testing.T) {
	s := startServer(t)
	makeSpace(t, s, "demo", "Demo", "")
	if r := s.get(t, "/api/spaces/demo", nil); r.status != 200 {
		t.Fatalf("%d", r.status)
	}
	if r := s.post(t, "/api/spaces/demo/cards", agentP(),
		map[string]any{"cards": []any{map[string]any{"title": "T", "content": "c"}}}); r.status != 201 {
		t.Fatalf("%d %s", r.status, r.str())
	}
}

// --- the contract ------------------------------------------------------------

func TestAuth_TheContractDocumentsBearerAuth(t *testing.T) {
	scheme := asMap(asMap(asMap(openapiDoc(t)["components"])["securitySchemes"])["bearerAuth"])
	if asStr(scheme["type"]) != "http" || asStr(scheme["scheme"]) != "bearer" {
		t.Errorf("bearerAuth = %s", canonical(scheme))
	}
	// [{bearerAuth}, {}] — a token is accepted, and an open server is still valid.
	security := asArr(openapiDoc(t)["security"])
	var hasEmpty, hasBearer bool
	for _, s := range security {
		if len(asMap(s)) == 0 {
			hasEmpty = true
		}
		if _, ok := asMap(s)["bearerAuth"]; ok {
			hasBearer = true
		}
	}
	if !hasEmpty || !hasBearer {
		t.Errorf("security = %v", canonical(security))
	}
}

func TestAuth_TheContractDocumentsHealthAsPublic(t *testing.T) {
	healthGet := asMap(asMap(asMap(openapiDoc(t)["paths"])["/health"])["get"])
	security := asArr(healthGet["security"])
	if len(security) != 1 || len(asMap(security[0])) != 0 {
		t.Errorf("/health security = %v, want [{}]", canonical(security))
	}
}

func TestAuth_TheContractDocumentsWhoami(t *testing.T) {
	paths := asMap(openapiDoc(t)["paths"])
	if _, ok := paths["/whoami"]; !ok {
		t.Fatal("/whoami is not documented")
	}
	if _, ok := asMap(asMap(asMap(paths["/whoami"])["get"])["responses"])["401"]; !ok {
		t.Error("/whoami does not document 401")
	}
}

func TestAuth_TheErrorEnumCoversTheNewCodes(t *testing.T) {
	errorSchema := asMap(asMap(asMap(openapiDoc(t)["components"])["schemas"])["Error"])
	enum := asArr(asMap(errorSchema["properties"])["error"].(map[string]any)["enum"])
	codes := map[string]bool{}
	for _, c := range enum {
		codes[asStr(c)] = true
	}
	if !codes["unauthorized"] || !codes["forbidden"] {
		t.Errorf("error enum = %v", enum)
	}
}
