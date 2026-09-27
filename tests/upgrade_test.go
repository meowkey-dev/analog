package conformance

import (
	"testing"
)

func TestUpgrade_OperatorCanDisableIt(t *testing.T) {
	s := startServer(t, withEnv("ANALOG_UPGRADE_DISABLED=1"))
	r := s.get(t, "/api/upgrade", nil)
	if r.status != 200 {
		t.Fatalf("GET /api/upgrade: %d %s", r.status, r.str())
	}
	want := fixture(t, "upgrade.unavailable.json")
	got := r.obj()
	for _, key := range []string{"available", "supported", "reason"} {
		if got[key] != asMap(want)[key] {
			t.Errorf("%s = %v, want %v", key, got[key], asMap(want)[key])
		}
	}
	if asStr(got["current"]) == "" {
		t.Error("current release is missing")
	}
	if r := s.post(t, "/api/upgrade", nil, map[string]any{},
		map[string]string{"X-Analog-Upgrade": "1"}); r.status != 409 {
		t.Fatalf("POST /api/upgrade: %d %s", r.status, r.str())
	}
	if r := s.post(t, "/api/upgrade", nil, map[string]any{},
		map[string]string{"X-Analog-Upgrade": "1", "Origin": "https://attacker.example"}); r.status != 403 {
		t.Fatalf("cross-origin POST /api/upgrade: %d %s", r.status, r.str())
	}
}

func TestUpgrade_RequiresHumanTokenAndPreflightHeader(t *testing.T) {
	s := securedServer(t)
	if r := s.get(t, "/api/upgrade", nil); r.status != 401 {
		t.Fatalf("GET without token: %d", r.status)
	}
	if r := s.post(t, "/api/upgrade", nil, map[string]any{},
		map[string]string{"X-Analog-Upgrade": "1"}); r.status != 401 {
		t.Fatalf("POST without token: %d", r.status)
	}
	if r := s.post(t, "/api/upgrade", nil, map[string]any{}, bearer(s, "claude-code")); r.status != 403 {
		t.Fatalf("POST with agent token: %d", r.status)
	}
	if r := s.post(t, "/api/upgrade", nil, map[string]any{}, bearer(s, "kai")); r.status != 400 {
		t.Fatalf("POST without preflight header: %d", r.status)
	}
}
