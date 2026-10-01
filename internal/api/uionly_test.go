package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

// A --ui-only server is what the desktop app runs once the user connects to a
// remote: the page comes from it, the data from somewhere else.

func healthOf(t *testing.T, s *Server) map[string]any {
	t.Helper()
	resp := httptest.NewRecorder()
	s.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, API+"/health", nil))
	if resp.Code != http.StatusOK {
		t.Fatalf("health: status %d", resp.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestUIOnlyRoutesWhatTheFullServerRoutes(t *testing.T) {
	// Routed rather than left to the SPA fallback: a client pointed here by mistake
	// should get the contract's error, not index.html with a 200.
	full := newTestServer(t).Patterns()
	ui := NewUIOnly(nil).Patterns()
	if !slices.Equal(full, ui) {
		t.Errorf("ui-only routes differ from the full server:\nfull %v\nui   %v", full, ui)
	}
}

func TestUIOnlyHealthSaysSo(t *testing.T) {
	body := healthOf(t, NewUIOnly(nil))
	if body["ui_only"] != true {
		t.Errorf("ui_only = %v, want true", body["ui_only"])
	}
	if body["auth_required"] != false {
		t.Errorf("auth_required = %v; a server with no data has nothing to guard", body["auth_required"])
	}
	if body["version"] != Version || body["min_client"] != MinClient {
		t.Errorf("version/min_client = %v/%v", body["version"], body["min_client"])
	}
}

func TestAFullServerIsNotUIOnly(t *testing.T) {
	body := healthOf(t, newTestServer(t))
	if body["ui_only"] != false {
		t.Errorf("ui_only = %v, want false", body["ui_only"])
	}
	if body["min_client"] != MinClient {
		t.Errorf("min_client = %v, want %q", body["min_client"], MinClient)
	}
}

func TestUIOnlyRefusesEveryOtherOperation(t *testing.T) {
	s := NewUIOnly(nil)
	for _, req := range []struct{ method, path string }{
		{"GET", API + "/spaces"},
		{"POST", API + "/spaces?actor=kai&actor_kind=human"},
		{"GET", API + "/whoami"},
		{"GET", API + "/upgrade"},
		{"GET", API + "/spaces/demo/events/stream"},
	} {
		resp := httptest.NewRecorder()
		s.ServeHTTP(resp, httptest.NewRequest(req.method, req.path, nil))
		if resp.Code != http.StatusServiceUnavailable {
			t.Errorf("%s %s: status %d, want 503", req.method, req.path, resp.Code)
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["error"] != "ui_only" {
			t.Errorf("%s %s: error = %v, want ui_only", req.method, req.path, body["error"])
		}
	}
}

func TestUIOnlyServesTheBundle(t *testing.T) {
	s := NewUIOnly(fstest.MapFS{"index.html": {Data: []byte("<!doctype html>analog")}})
	for _, path := range []string{"/", "/s/redesign"} {
		resp := httptest.NewRecorder()
		s.ServeHTTP(resp, httptest.NewRequest(http.MethodGet, path, nil))
		if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "analog") {
			t.Errorf("%s: status %d body %q", path, resp.Code, resp.Body.String())
		}
	}
}

func TestMinClientIsNotAheadOfTheContract(t *testing.T) {
	// A server that demands a newer client than itself would refuse its own UI.
	if compareContract(MinClient, Version) > 0 {
		t.Errorf("MinClient %s is newer than Version %s", MinClient, Version)
	}
}

func compareContract(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(pa[i])
		y, _ := strconv.Atoi(pb[i])
		if x != y {
			return x - y
		}
	}
	return 0
}
