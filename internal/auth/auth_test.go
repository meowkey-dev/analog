package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These hold the token store's objects rather than a socket. Everything they
// describe that is observable over HTTP is also asserted black-box in
// tests/auth_test.go.

func newTestStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), "auth.json"))
}

func TestAuthIsOffUntilATokenExists(t *testing.T) {
	store := newTestStore(t)
	if store.Enabled() {
		t.Error("a store with no file must not be enabled")
	}
	entries, err := store.Entries()
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %v, want none", entries)
	}
	if store.Resolve("anything") != nil {
		t.Error("resolved a token that was never issued")
	}
}

func TestIssueReturnsAUsableToken(t *testing.T) {
	store := newTestStore(t)
	token, err := store.Issue("claude-code", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(token, TokenPrefix) {
		t.Errorf("token %q lacks the %q prefix", token, TokenPrefix)
	}
	if len(token) <= 40 {
		t.Errorf("token %q is too short to be a secret", token)
	}
	if !store.Enabled() {
		t.Error("issuing a token must enable auth")
	}
	identity := store.Resolve(token)
	if identity == nil || identity.Actor != "claude-code" || identity.ActorKind != "agent" {
		t.Errorf("Resolve = %+v, want claude-code/agent", identity)
	}
}

func TestTheSecretIsNeverStored(t *testing.T) {
	store := newTestStore(t)
	token, err := store.Issue("kai", "human")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), token) {
		t.Error("a leaked auth file must not hand over working tokens")
	}
	var file struct {
		Actors []map[string]any `json:"actors"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	if file.Actors[0]["token_sha256"] == "" {
		t.Error("the digest is missing")
	}
	entries, _ := store.Entries()
	rendered, _ := json.Marshal(entries)
	if strings.Contains(string(rendered), "token_sha256") {
		t.Error("Entries must carry no secret material")
	}
}

func TestTheStoreIsNotWorldReadable(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Issue("kai", "human"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

func TestTokensArePerActor(t *testing.T) {
	store := newTestStore(t)
	human, err := store.Issue("kai", "human")
	if err != nil {
		t.Fatal(err)
	}
	agent, err := store.Issue("claude-code", "agent")
	if err != nil {
		t.Fatal(err)
	}
	if store.Resolve(human).Actor != "kai" {
		t.Error("the human token resolved to the wrong actor")
	}
	if store.Resolve(agent).Actor != "claude-code" {
		t.Error("the agent token resolved to the wrong actor")
	}
	if human == agent {
		t.Error("two actors were issued the same secret")
	}
}

func TestReissuingReplacesThePreviousToken(t *testing.T) {
	store := newTestStore(t)
	old, _ := store.Issue("claude-code", "agent")
	fresh, _ := store.Issue("claude-code", "agent")
	if store.Resolve(old) != nil {
		t.Error("the old token must stop working")
	}
	if store.Resolve(fresh).Actor != "claude-code" {
		t.Error("the new token does not work")
	}
	entries, _ := store.Entries()
	if len(entries) != 1 {
		t.Errorf("entries = %d, want 1", len(entries))
	}
}

func TestRevoke(t *testing.T) {
	store := newTestStore(t)
	token, _ := store.Issue("codex", "agent")
	removed, err := store.Revoke("codex")
	if err != nil || !removed {
		t.Fatalf("Revoke = %v, %v; want true, nil", removed, err)
	}
	if store.Resolve(token) != nil {
		t.Error("a revoked token still resolves")
	}
	if again, _ := store.Revoke("codex"); again {
		t.Error("revoking twice must report nothing was removed")
	}
}

func TestIssueValidates(t *testing.T) {
	store := newTestStore(t)
	for _, tc := range []struct{ actor, kind string }{
		{"", "agent"},
		{strings.Repeat("a", 65), "agent"},
		{"x", "robot"},
	} {
		if _, err := store.Issue(tc.actor, tc.kind); err == nil {
			t.Errorf("Issue(%q, %q) was accepted", tc.actor, tc.kind)
		}
	}
}

func TestBearerParsing(t *testing.T) {
	for _, tc := range []struct{ header, want string }{
		{"Bearer abc", "abc"},
		{"bearer abc", "abc"},
		{"Bearer  abc ", "abc"},
		{"Basic abc", ""},
		{"abc", ""},
		{"Bearer", ""},
		{"Bearer ", ""},
		{"", ""},
	} {
		if got := Bearer(tc.header); got != tc.want {
			t.Errorf("Bearer(%q) = %q, want %q", tc.header, got, tc.want)
		}
	}
}

// --- the safety rail ---------------------------------------------------------

func TestIsLoopback(t *testing.T) {
	for _, tc := range []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true}, {"localhost", true}, {"::1", true}, {"127.0.0.5", true},
		{"0.0.0.0", false}, {"192.168.1.10", false}, {"analog.example.com", false},
	} {
		if got := IsLoopback(tc.host); got != tc.want {
			t.Errorf("IsLoopback(%q) = %v, want %v", tc.host, got, tc.want)
		}
	}
}

func TestLoopbackMayRunWithoutTokens(t *testing.T) {
	if err := RequireAuthForHost("127.0.0.1", newTestStore(t)); err != nil {
		t.Errorf("loopback with no tokens was refused: %v", err)
	}
}

func TestANetworkBindWithoutTokensIsRefused(t *testing.T) {
	err := RequireAuthForHost("0.0.0.0", newTestStore(t))
	if err == nil {
		t.Fatal("an unauthenticated network bind was allowed")
	}
	if !strings.Contains(err.Error(), "world-writable") {
		t.Errorf("the error must say why: %v", err)
	}
	if !strings.Contains(err.Error(), "analog token add") {
		t.Errorf("the error must say how to fix it: %v", err)
	}
}

func TestANetworkBindWithTokensIsAllowed(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Issue("kai", "human"); err != nil {
		t.Fatal(err)
	}
	if err := RequireAuthForHost("0.0.0.0", store); err != nil {
		t.Errorf("a network bind with tokens was refused: %v", err)
	}
}

// TestTheStoreIsReRead pins what the `secured` fixture and `analog token add`
// depend on: issuing a token secures a server that is already running.
func TestTheStoreIsReRead(t *testing.T) {
	store := newTestStore(t)
	other := NewStore(store.Path)
	if other.Enabled() {
		t.Fatal("precondition: auth should start off")
	}
	if _, err := store.Issue("kai", "human"); err != nil {
		t.Fatal(err)
	}
	if !other.Enabled() {
		t.Error("a second handle on the same file did not see the new token")
	}
}
