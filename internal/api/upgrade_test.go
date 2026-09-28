package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/meowkey-dev/analog/internal/updater"
)

type fakeUpgrade struct {
	status   updater.Status
	err      error
	installs int
}

func (f *fakeUpgrade) Status(context.Context) (updater.Status, error) { return f.status, nil }
func (f *fakeUpgrade) Install(context.Context) (updater.Status, error) {
	f.installs++
	return f.status, f.err
}

func upgradeRequest(s *Server) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787"+API+"/upgrade", strings.NewReader("{}"))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Analog-Upgrade", "1")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

func TestUpgradeRejectsRepeatedPostBeforeRestart(t *testing.T) {
	s := newTestServer(t)
	fake := &fakeUpgrade{status: updater.Status{Current: "0.1.0", Latest: "0.2.0",
		Available: true, Supported: true}}
	s.Updater = fake
	s.Restart = func() {}
	if w := upgradeRequest(s); w.Code != http.StatusAccepted {
		t.Fatalf("first POST: %d %s", w.Code, w.Body.String())
	}
	if w := upgradeRequest(s); w.Code != http.StatusConflict {
		t.Fatalf("repeat POST: %d %s", w.Code, w.Body.String())
	}
	if fake.installs != 1 {
		t.Fatalf("Install called %d times", fake.installs)
	}
}

func TestUpgradeErrorsMatchTheContract(t *testing.T) {
	for _, tc := range []struct {
		err    error
		status int
	}{
		{fmt.Errorf("%w: offline", updater.ErrRelease), http.StatusBadGateway},
		{fmt.Errorf("%w: read-only install", updater.ErrInstall), http.StatusInternalServerError},
	} {
		s := newTestServer(t)
		s.Updater = &fakeUpgrade{status: updater.Status{Supported: true}, err: tc.err}
		s.Restart = func() {}
		w := upgradeRequest(s)
		if w.Code != tc.status || !strings.Contains(w.Body.String(), `"error":"upgrade_failed"`) {
			t.Fatalf("POST error %v: %d %s", tc.err, w.Code, w.Body.String())
		}
	}
}

func TestUpgradeRejectsReboundAndOtherLoopbackOrigins(t *testing.T) {
	s := newTestServer(t)
	s.Updater = updater.New()
	s.Restart = func() {}
	for _, tc := range []struct{ host, origin string }{
		{"evil.example:8787", "http://evil.example:8787"},
		{"localhost:8787", "http://localhost:9999"},
	} {
		t.Run(tc.host+"/"+tc.origin, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "http://"+tc.host+API+"/upgrade", strings.NewReader("{}"))
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Analog-Upgrade", "1")
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			if w.Code != http.StatusForbidden {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
}
