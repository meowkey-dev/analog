package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/meowkey-dev/analog/internal/updater"
	"github.com/meowkey-dev/analog/internal/version"
)

func (s *Server) upgradeStatus(w http.ResponseWriter, r *http.Request) {
	if s.Updater == nil {
		writeJSON(w, http.StatusOK, updater.Status{Current: version.Version,
			Reason: "Upgrade is unavailable for this server."})
		return
	}
	status, err := s.Updater.Status(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) upgrade(w http.ResponseWriter, r *http.Request) {
	if s.Updater == nil || s.Restart == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "conflict",
			"message": "Upgrade is unavailable for this server."})
		return
	}
	id := identityOf(r)
	if s.Tokens.Enabled() && (id == nil || id.ActorKind != "human") {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden",
			"message": "A human token is required to upgrade the server."})
		return
	}
	if !s.Tokens.Enabled() {
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden",
				"message": "Upgrade requires a same-origin page on a tokenless server."})
			return
		}
	}
	// A cross-site form cannot send this header. Browser fetch performs a preflight
	// for it, so a hostile site cannot make a tokenless loopback server upgrade.
	if r.Header.Get("X-Analog-Upgrade") != "1" ||
		!strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "validation_failed",
			"message": "Upgrade requires JSON and X-Analog-Upgrade: 1."})
		return
	}
	var body map[string]any
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1024))
	if r.ContentLength > 1024 || decoder.Decode(&body) != nil || body == nil || len(body) != 0 ||
		decoder.Decode(new(any)) != io.EOF {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "validation_failed",
			"message": "Upgrade body must be an empty JSON object."})
		return
	}
	if !s.upgradeMu.TryLock() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "conflict",
			"message": "An upgrade is already in progress."})
		return
	}
	defer s.upgradeMu.Unlock()
	status, err := s.Updater.Install(r.Context())
	if err != nil {
		fail(w, err)
		return
	}
	if !status.Supported {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "conflict",
			"message": status.Reason})
		return
	}
	if !status.Available {
		writeJSON(w, http.StatusOK, status)
		return
	}
	writeJSON(w, http.StatusAccepted, status)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	time.AfterFunc(250*time.Millisecond, s.Restart)
}
