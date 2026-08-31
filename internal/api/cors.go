package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/meowkey-dev/analog/internal/config"
)

// allowedMethods mirrors allow_methods=["*"] as an explicit list, because a
// preflight response has to name them.
const allowedMethods = "DELETE, GET, HEAD, OPTIONS, PATCH, POST, PUT"

// preflightMaxAge is how long a browser may cache the preflight, in seconds.
const preflightMaxAge = "600"

// cors is the outermost middleware.
//
// It has to wrap authentication rather than sit inside it: a 401 that carries no
// CORS headers reaches the browser as an opaque network error, and the user is told
// nothing instead of "unauthorized". A preflight carries no Authorization header at
// all, so it must never be gated either.
func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		allowed, value := s.originAllowed(origin)
		if value != "*" {
			// The response varies by origin, so a shared cache must not reuse it.
			w.Header().Add("Vary", "Origin")
		}

		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			if !allowed {
				http.Error(w, "Disallowed CORS origin", http.StatusBadRequest)
				return
			}
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", value)
			h.Set("Access-Control-Allow-Methods", allowedMethods)
			h.Set("Access-Control-Max-Age", preflightMaxAge)
			// allow_headers=["*"]: echo whatever the browser asked for.
			if want := r.Header.Get("Access-Control-Request-Headers"); want != "" {
				h.Set("Access-Control-Allow-Headers", want)
			}
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("OK"))
			return
		}

		if allowed {
			w.Header().Set("Access-Control-Allow-Origin", value)
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed reports whether the origin is on the allowlist, and what to echo
// back. `*` is honoured but never the default: an open canvas should be a choice.
func (s *Server) originAllowed(origin string) (bool, string) {
	for _, candidate := range config.CORSOrigins() {
		if candidate == "*" {
			return true, "*"
		}
		if strings.EqualFold(candidate, origin) {
			return true, origin
		}
	}
	if config.LoopbackOriginsAllowed() && isLoopbackOrigin(origin) {
		return true, origin
	}
	return false, origin
}

// isLoopbackOrigin matches http://localhost:<any port> and http://127.0.0.1:<any
// port> — the origins a page served on this machine carries. Host compares
// case-insensitively, as browsers serialize it; a suffix like
// localhost.evil.example, another scheme, userinfo or a bare "null" is not
// loopback and stays denied.
func isLoopbackOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil {
		return false
	}
	if !strings.EqualFold(parsed.Scheme, "http") {
		return false
	}
	host := parsed.Hostname()
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1"
}
