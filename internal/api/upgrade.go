package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/meowkey-dev/analog/internal/apierr"
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
		fail(w, apierr.Conflict("Upgrade is unavailable for this server."))
		return
	}
	id := identityOf(r)
	if s.Tokens.Enabled() && (id == nil || id.ActorKind != "human") {
		fail(w, apierr.Forbidden("A human token is required to upgrade the server."))
		return
	}
	if !s.Tokens.Enabled() {
		if !loopbackHost(r.Host) ||
			(r.Header.Get("Origin") != "" && r.Header.Get("Origin") != "http://"+r.Host) {
			fail(w, apierr.Forbidden("Upgrade requires a loopback, same-origin request on a tokenless server."))
			return
		}
	}
	// The loopback Host and same-origin checks above are the browser boundary.
	// The custom header also excludes plain cross-site form submissions, but CORS
	// permits other loopback ports to request it, so the header alone is not enough.
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if r.Header.Get("X-Analog-Upgrade") != "1" ||
		mediaType != "application/json" {
		fail(w, apierr.ValidationFailed("Upgrade requires JSON and X-Analog-Upgrade: 1."))
		return
	}
	var body map[string]any
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	if decoder.Decode(&body) != nil || body == nil || len(body) != 0 ||
		decoder.Decode(new(any)) != io.EOF {
		fail(w, apierr.ValidationFailed("Upgrade body must be an empty JSON object."))
		return
	}
	if !s.upgradeMu.TryLock() {
		fail(w, apierr.Conflict("An upgrade is already in progress."))
		return
	}
	defer s.upgradeMu.Unlock()
	if s.upgradePending {
		fail(w, apierr.Conflict("The server is restarting after an upgrade."))
		return
	}
	status, err := s.Updater.Install(r.Context())
	if err != nil {
		if errors.Is(err, updater.ErrRelease) {
			fail(w, apierr.UpgradeFailed(http.StatusBadGateway, err.Error()))
		} else {
			fail(w, apierr.UpgradeFailed(http.StatusInternalServerError, err.Error()))
		}
		return
	}
	if !status.Supported {
		fail(w, apierr.Conflict(status.Reason))
		return
	}
	if !status.Available {
		writeJSON(w, http.StatusOK, status)
		return
	}
	s.upgradePending = true
	writeJSON(w, http.StatusAccepted, status)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	time.AfterFunc(250*time.Millisecond, s.Restart)
}

func loopbackHost(hostport string) bool {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		host = hostport
	}
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}
