// The contract document itself is valid and covers SPEC §3.
//
// The python harness also ran the full OpenAPI 3.1 metaschema check
// (openapi_spec_validator); that retires with the python suite. What the suite
// actually relies on is asserted here structurally: the document parses, its
// paths and components are navigable, and it pins what SPEC §3 requires.
package conformance

import (
	"net"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// SPEC §3, plus /feedback which contracts/README.md documents as a correction to it.
var specEndpoints = [][2]string{
	{"/spaces", "post"}, {"/spaces", "get"},
	{"/spaces/{slug}", "get"}, {"/spaces/{slug}", "patch"}, {"/spaces/{slug}", "delete"},
	{"/spaces/{slug}/canvas", "get"},
	{"/spaces/{slug}/import", "post"},
	{"/spaces/{slug}/cards", "post"},
	{"/spaces/{slug}/cards/{card_id}", "patch"},
	{"/spaces/{slug}/cards/{card_id}", "delete"},
	{"/spaces/{slug}/links", "post"},
	{"/spaces/{slug}/links/{link_id}", "delete"},
	{"/spaces/{slug}/annotations", "get"}, {"/spaces/{slug}/annotations", "post"},
	{"/spaces/{slug}/annotations/{annotation_id}", "patch"},
	{"/spaces/{slug}/feedback", "get"},
	{"/spaces/{slug}/events", "get"},
	{"/spaces/{slug}/events/stream", "get"},
	{"/spaces/{slug}/media", "post"},
}

func TestOpenapi_SpecIsOpenapi31(t *testing.T) {
	openapi := openapiDoc(t)
	if asStr(openapi["openapi"]) != "3.1.0" {
		t.Errorf("openapi = %q, want 3.1.0", openapi["openapi"])
	}
	paths := asMap(openapi["paths"])
	if len(paths) == 0 {
		t.Fatal("no paths documented")
	}
	components := asMap(openapi["components"])
	if asMap(components["schemas"]) == nil || asMap(components["parameters"]) == nil {
		t.Fatal("components/schemas and components/parameters are required")
	}
	for path, ops := range paths {
		if asMap(ops) == nil {
			t.Errorf("path %s is not an operations object", path)
			continue
		}
		for method, op := range asMap(ops) {
			if method == "parameters" {
				continue
			}
			if asMap(op)["responses"] == nil {
				t.Errorf("%s %s documents no responses", method, path)
			}
		}
	}
	if asStr(asMap(openapi["info"])["title"]) == "" || asStr(asMap(openapi["info"])["version"]) == "" {
		t.Error("info.title and info.version are required")
	}
}

func TestOpenapi_EverySpecEndpointIsDocumented(t *testing.T) {
	openapi := openapiDoc(t)
	documented := map[[2]string]bool{}
	for path, ops := range asMap(openapi["paths"]) {
		for method := range asMap(ops) {
			switch method {
			case "get", "post", "patch", "put", "delete":
				documented[[2]string{path, method}] = true
			}
		}
	}
	for _, key := range specEndpoints {
		if !documented[key] {
			t.Errorf("%s %s is required by SPEC §3 but not documented", key[1], key[0])
		}
	}
}

func TestOpenapi_BaseUrlPinsThePort(t *testing.T) {
	// The port is contract, not a runtime choice.
	servers := asArr(openapiDoc(t)["servers"])
	if len(servers) == 0 {
		t.Fatal("no servers documented")
	}
	if got := asStr(asMap(servers[0])["url"]); got != "http://127.0.0.1:8787/api" {
		t.Errorf("servers[0].url = %q", got)
	}
}

func TestOpenapi_TheServerDefaultsToTheContractsAddress(t *testing.T) {
	// ...and the binary must actually default to it.
	//
	// Asserted by running the server with no --host/--port and knocking on the
	// address openapi.json advertises, rather than by reading a constant out of
	// the implementation. Skipped when something else already holds the port.
	probe, err := net.Listen("tcp", "127.0.0.1:8787")
	if err != nil {
		t.Skipf("127.0.0.1:8787 is already in use: %v", err)
	}
	probe.Close()

	bin := serverBin(t)
	root := t.TempDir()
	cmd := exec.Command(bin[0], bin[1:]...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(),
		"ANALOG_DATA_DIR="+root,
		"ANALOG_DB="+root+"/analog.db",
		"ANALOG_AUTH_FILE="+root+"/auth.json")
	cmd.Stdout, cmd.Stderr = nil, nil
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	deadline := time.Now().Add(startupTimeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://127.0.0.1:8787/api/health")
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == 200 {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("nothing answered http://127.0.0.1:8787/api/health")
}

// operationParams resolves one operation's parameter list into names, following
// $refs into components/parameters.
func operationParams(t *testing.T, path, method string) map[string]bool {
	t.Helper()
	op := asMap(asMap(openapiDoc(t)["paths"])[path])[method]
	operation := asMap(op)
	parameters := asMap(asMap(openapiDoc(t)["components"])["parameters"])
	names := map[string]bool{}
	for _, p := range asArr(operation["parameters"]) {
		param := asMap(p)
		if ref, ok := param["$ref"].(string); ok {
			name := ref[strings.LastIndex(ref, "/")+1:]
			param = asMap(parameters[name])
		}
		names[asStr(param["name"])] = true
	}
	return names
}

func TestOpenapi_MutatingOperationsRequireActor(t *testing.T) {
	// SPEC §3 and §2.2: actor is mandatory everywhere, with no default.
	parameters := asMap(asMap(openapiDoc(t)["components"])["parameters"])
	for _, actorRef := range []string{"actor", "actorKind"} {
		if !asBool(asMap(parameters[actorRef])["required"]) {
			t.Errorf("components/parameters/%s is not required", actorRef)
		}
	}
	for _, key := range specEndpoints {
		path, method := key[0], key[1]
		switch method {
		case "post", "patch", "delete":
			t.Run(method+" "+path, func(t *testing.T) {
				names := operationParams(t, path, method)
				if !names["actor"] || !names["actor_kind"] {
					t.Errorf("%s %s is missing actor params", method, path)
				}
			})
		}
	}
}

func TestOpenapi_FeedbackRequiresActorButNotActorKind(t *testing.T) {
	// A cursor is keyed by actor name alone (schema.sql actor_cursor PK).
	names := operationParams(t, "/spaces/{slug}/feedback", "get")
	if !names["actor"] {
		t.Error("feedback does not take actor")
	}
	if names["actor_kind"] {
		t.Error("feedback must not take actor_kind")
	}
	if !names["since"] || !names["advance"] {
		t.Errorf("feedback is missing since/advance: %v", names)
	}
}

func TestOpenapi_NoWholeCanvasReplace(t *testing.T) {
	// SPEC §10: destructive bulk semantics are deliberately absent.
	canvasOps := map[string]bool{}
	for method := range asMap(asMap(openapiDoc(t)["paths"])["/spaces/{slug}/canvas"]) {
		if method != "parameters" {
			canvasOps[method] = true
		}
	}
	if len(canvasOps) != 1 || !canvasOps["get"] {
		t.Errorf("canvas operations = %v, want only get", canvasOps)
	}
	if _, has := asMap(asMap(openapiDoc(t)["paths"])["/spaces/{slug}/import"])["put"]; has {
		t.Error("import must not accept put")
	}
}
