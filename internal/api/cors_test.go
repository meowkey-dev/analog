package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The issue (#42) that produced the loopback rule verified each of these against a
// real server; the table is what keeps it true. Echoed means the request gets
// Access-Control-Allow-Origin: <origin>, which is what lets the desktop app's
// sidecar-served UI talk to a remote server cross-origin.
func TestCORSOriginEcho(t *testing.T) {
	server := newTestServer(t)

	cases := []struct {
		origin string
		want   string // the header value, or "" for denied
	}{
		{"http://127.0.0.1:51468", "http://127.0.0.1:51468"},
		{"http://localhost:51468", "http://localhost:51468"},
		{"http://localhost:5173", "http://localhost:5173"},
		{"http://LOCALHOST:51468", "http://LOCALHOST:51468"},
		{"tauri://localhost", "tauri://localhost"},
		{"http://tauri.localhost", "http://tauri.localhost"},

		{"https://evil.example", ""},
		{"http://localhost.evil.example", ""},
		{"http://127.0.0.1.evil.example", ""},
		{"https://localhost:51468", ""},
		{"http://user@localhost", ""},
		{"null", ""},
	}

	for _, tc := range cases {
		request := httptest.NewRequest(http.MethodGet, API+"/health", nil)
		request.Header.Set("Origin", tc.origin)
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)

		if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != tc.want {
			t.Errorf("Origin %q: Access-Control-Allow-Origin = %q, want %q", tc.origin, got, tc.want)
		}
	}
}

// A preflight for an allowed origin answers 200 with the same allowances tauri
// origins get; a disallowed origin is refused before it can ask anything else.
func TestCORSPreflight(t *testing.T) {
	server := newTestServer(t)

	for _, origin := range []string{"http://127.0.0.1:51468", "http://localhost:51468"} {
		request := httptest.NewRequest(http.MethodOptions, API+"/spaces", nil)
		request.Header.Set("Origin", origin)
		request.Header.Set("Access-Control-Request-Method", http.MethodPost)
		request.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
		recorder := httptest.NewRecorder()
		server.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("preflight for %q: status = %d, want 200", origin, recorder.Code)
		}
		h := recorder.Header()
		if got := h.Get("Access-Control-Allow-Origin"); got != origin {
			t.Errorf("preflight for %q: echoed %q", origin, got)
		}
		if !strings.Contains(h.Get("Access-Control-Allow-Methods"), http.MethodPost) {
			t.Errorf("preflight for %q: POST not allowed", origin)
		}
		if got := h.Get("Access-Control-Allow-Headers"); got != "authorization, content-type" {
			t.Errorf("preflight for %q: allow-headers = %q, want the request echoed", origin, got)
		}
	}

	request := httptest.NewRequest(http.MethodOptions, API+"/spaces", nil)
	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Errorf("preflight for evil.example: status = %d, want 400", recorder.Code)
	}
}

// An explicit ANALOG_CORS_ORIGINS replaces the defaults wholesale — loopback
// matching included, the way it already replaces the tauri origins. A custom list
// is a deliberate policy.
func TestCustomCORSListDropsLoopback(t *testing.T) {
	t.Setenv("ANALOG_CORS_ORIGINS", "https://canvas.example.org")
	server := newTestServer(t)

	request := httptest.NewRequest(http.MethodGet, API+"/health", nil)
	request.Header.Set("Origin", "http://127.0.0.1:51468")
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("loopback origin under a custom list: echoed %q, want denied", got)
	}

	request = httptest.NewRequest(http.MethodGet, API+"/health", nil)
	request.Header.Set("Origin", "https://canvas.example.org")
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	if got := recorder.Header().Get("Access-Control-Allow-Origin"); got != "https://canvas.example.org" {
		t.Errorf("listed origin: echoed %q, want the origin", got)
	}
}
