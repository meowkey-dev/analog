package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/meowkey-dev/analog/internal/auth"
	"github.com/meowkey-dev/analog/internal/config"
	"github.com/meowkey-dev/analog/internal/store"
)

// The contract is the definition of done, so it is checked mechanically rather than
// by discipline: every operation contracts/openapi.json documents must be routed,
// and every route under /api must be documented. This is what makes hand-written
// types as load-bearing as generated ones would have been.

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

func loadOpenAPI(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "contracts", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	return spec
}

// documented lists "METHOD /path" for every operation in the contract, with the
// contract's own {placeholder} names.
func documented(t *testing.T) []string {
	spec := loadOpenAPI(t)
	paths, _ := spec["paths"].(map[string]any)
	var out []string
	for path, raw := range paths {
		operations, _ := raw.(map[string]any)
		for method := range operations {
			switch method {
			case "get", "post", "patch", "put", "delete":
				out = append(out, strings.ToUpper(method)+" "+API+path)
			}
		}
	}
	sort.Strings(out)
	return out
}

// routed lists the patterns the server registers, normalised the same way.
func routed(t *testing.T) []string {
	t.Helper()
	server := newTestServer(t)

	var out []string
	for _, pattern := range server.Patterns() {
		if !strings.Contains(pattern, " "+API+"/") {
			continue // the SPA catch-all is not an API operation
		}
		out = append(out, pattern)
	}
	sort.Strings(out)
	return out
}

func TestEveryDocumentedOperationIsRouted(t *testing.T) {
	have := map[string]bool{}
	for _, pattern := range routed(t) {
		have[pattern] = true
	}
	for _, operation := range documented(t) {
		if !have[operation] {
			t.Errorf("contracts/openapi.json documents %s, and nothing routes it", operation)
		}
	}
}

func TestEveryRouteIsDocumented(t *testing.T) {
	want := map[string]bool{}
	for _, operation := range documented(t) {
		want[operation] = true
	}
	for _, pattern := range routed(t) {
		if !want[pattern] {
			t.Errorf("%s is served but contracts/openapi.json does not document it; "+
				"an addition goes through the amendment process in contracts/README.md",
				pattern)
		}
	}
}

func TestHealthIsTheOnlyPublicOperation(t *testing.T) {
	if len(publicPaths) != 1 || !publicPaths[API+"/health"] {
		t.Errorf("publicPaths = %v; a client cannot be asked to authenticate before "+
			"it can discover that authentication exists, and nothing else earns that",
			publicPaths)
	}
}

func TestVersionMatchesTheContract(t *testing.T) {
	spec := loadOpenAPI(t)
	info, _ := spec["info"].(map[string]any)
	if info["version"] != Version {
		t.Errorf("/health reports %q, contracts/openapi.json says %q", Version, info["version"])
	}
}

// --- helpers -------------------------------------------------------------------

func newTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "analog.db"), filepath.Join(dir, "media"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(st, auth.NewStore(filepath.Join(dir, "auth.json")), nil)
}

// TestAMalformedBodyIsAContractShapedError pins the remapping: whatever net/http
// produces natively for a broken body is not the Error schema.
func TestAMalformedBodyIsAContractShapedError(t *testing.T) {
	server := newTestServer(t)
	request := httptest.NewRequest("POST",
		API+"/spaces?actor=kai&actor_kind=human", strings.NewReader("{not json"))
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "validation_failed" {
		t.Errorf("error = %v, want validation_failed", body["error"])
	}
	if _, ok := body["message"].(string); !ok {
		t.Error("the Error schema requires a message")
	}
}

// TestAnOversizedUploadIsRejectedWithTheCap covers what the contract suite does not:
// there is no fixture for a 25MB file, and one has no business being in the repo.
func TestAnOversizedUploadIsRejectedWithTheCap(t *testing.T) {
	server := newTestServer(t)
	if _, err := server.Store.CreateSpace("demo", "D", "", "kai", "human"); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="big.png"`)
	header.Set("Content-Type", "image/png")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(make([]byte, config.MaxUploadBytes+1)); err != nil {
		t.Fatal(err)
	}
	writer.Close()

	request := httptest.NewRequest("POST",
		API+"/spaces/demo/media?actor=kai&actor_kind=human", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
	var parsed map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["error"] != "validation_failed" {
		t.Errorf("error = %v", parsed["error"])
	}
	// The message has to say what the limit is, or the caller cannot act on it.
	if message, _ := parsed["message"].(string); !strings.Contains(message, "exceeds") {
		t.Errorf("message = %q", message)
	}
}
