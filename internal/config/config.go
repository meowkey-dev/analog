// Package config holds the runtime specifics the contract leaves open.
//
// contracts/openapi.json pins the base URL to http://127.0.0.1:8787/api, so the port
// and the /api prefix are contract, not choice. Everything else here is a decision
// recorded in DECISIONS.md.
package config

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// --- network -----------------------------------------------------------------
// 8787 comes from openapi.json servers[0].url. Bind loopback by default; a
// non-loopback bind with no tokens is refused in internal/auth.

const (
	DefaultHost = "127.0.0.1"
	DefaultPort = 8787
	APIPrefix   = "/api"
)

func Host() string {
	if v := os.Getenv("ANALOG_HOST"); v != "" {
		return v
	}
	return DefaultHost
}

func Port() int {
	if v := os.Getenv("ANALOG_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return DefaultPort
}

// Vite dev server. The web app is same-origin in production (the server serves the
// embedded bundle), so CORS only matters during development.
var defaultCORSOrigins = []string{
	"http://localhost:5173",
	"http://127.0.0.1:5173",
}

// The Tauri shell loads the bundled UI from its own scheme, so a remote server has
// to allow it explicitly. macOS/iOS use tauri://localhost; the others use
// http://tauri.localhost.
var tauriOrigins = []string{"tauri://localhost", "http://tauri.localhost"}

// CORSOrigins returns the allowlist. `*` is honoured but never the default: an open
// canvas should be a choice.
func CORSOrigins() []string {
	raw, ok := os.LookupEnv("ANALOG_CORS_ORIGINS")
	if !ok {
		return append(append([]string{}, defaultCORSOrigins...), tauriOrigins...)
	}
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// LoopbackOriginsAllowed reports whether http://localhost:<any port> and
// http://127.0.0.1:<any port> may be echoed. The desktop app moved its UI to a
// local sidecar, so the page's origin is a loopback port the server cannot know in
// advance — the tauri origins whitelisted the shell, but it is the sidecar-served
// page that makes the cross-origin calls. Browsers set Origin truthfully, so a
// loopback origin can only come from a page actually served on this machine: the
// same trust class the tauri schemes were, generalized over the port. An explicit
// ANALOG_CORS_ORIGINS replaces the defaults, loopback matching included, the way it
// already replaces the tauri origins.
func LoopbackOriginsAllowed() bool {
	_, ok := os.LookupEnv("ANALOG_CORS_ORIGINS")
	return !ok
}

// --- storage -----------------------------------------------------------------

// DataDir is `./data`, or ANALOG_DATA_DIR.
//
// A binary has no repo to sit beside, so it makes a `data/` where you ran it.
// ANALOG_DATA_DIR is the answer whenever that guess is wrong.
func DataDir() string {
	if v := os.Getenv("ANALOG_DATA_DIR"); v != "" {
		return abs(v)
	}
	return abs("data")
}

func DBPath() string {
	if v := os.Getenv("ANALOG_DB"); v != "" {
		return abs(v)
	}
	return filepath.Join(DataDir(), "analog.db")
}

// MediaDir holds uploads at <data>/media/<space_id>/<m_ulid>.<ext>, keyed by space
// id rather than slug so a space rename cannot orphan its media.
func MediaDir() string { return filepath.Join(DataDir(), "media") }

// AuthPath holds the per-actor bearer tokens. Absent or empty means auth is off
// (loopback dev).
func AuthPath() string {
	if v := os.Getenv("ANALOG_AUTH_FILE"); v != "" {
		return abs(v)
	}
	return filepath.Join(DataDir(), "auth.json")
}

func abs(p string) string {
	if out, err := filepath.Abs(p); err == nil {
		return out
	}
	return p
}

// --- media -------------------------------------------------------------------

// MaxUploadBytes is the largest accepted upload. Arbitrary; a screenshot is ~1MB.
const MaxUploadBytes = 25 * 1024 * 1024

// MediaExtensions maps an accepted content type to the extension it is stored with.
var MediaExtensions = map[string]string{
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"image/svg+xml":   ".svg",
	"application/pdf": ".pdf",
}

// SupportedMediaTypes lists the accepted content types, sorted, for error detail.
func SupportedMediaTypes() []string {
	out := make([]string, 0, len(MediaExtensions))
	for ct := range MediaExtensions {
		out = append(out, ct)
	}
	slices.Sort(out)
	return out
}
