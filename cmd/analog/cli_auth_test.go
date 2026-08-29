package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/meowkey-dev/analog/client"
	"github.com/meowkey-dev/analog/internal/auth"
)

// The Go rewrite of tests/unit/test_cli_auth.py: the token commands, and what the
// CLI does when a server wants a token.

var tokenRE = regexp.MustCompile(`analog_[A-Za-z0-9_-]+`)

// issue mints a token through the CLI, the way an operator would, and returns the
// secret. The server is already running: the store is re-read per request, so this
// secures it in place.
func (h *harness) issue(actor, kind string) string {
	h.t.Helper()
	output := h.run("token", "add", actor, "--kind", kind)
	secret := tokenRE.FindString(output)
	if secret == "" {
		h.t.Fatalf("no token on stdout:\n%s", output)
	}
	return secret
}

func (h *harness) tokenStore() *auth.Store {
	return auth.NewStore(filepath.Join(h.dataDir, "auth.json"))
}

// --- the token commands ------------------------------------------------------------

func TestTokenAddPrintsTheSecretOnce(t *testing.T) {
	h := newHarness(t)
	output := h.run("token", "add", "claude-code", "--kind", "agent")
	secret := tokenRE.FindString(output)
	if secret == "" {
		t.Fatalf("no secret printed:\n%s", output)
	}
	identity := h.tokenStore().Resolve(secret)
	if identity == nil || identity.Actor != "claude-code" {
		t.Errorf("the printed token does not resolve: %+v", identity)
	}
	if !strings.Contains(output, "not recoverable") {
		t.Error("the operator must be told the secret is shown once")
	}
	if !strings.Contains(output, "ANALOG_TOKEN") {
		t.Error("tell the operator what to do with it")
	}
	if strings.Contains(h.run("token", "list"), secret) {
		t.Error("`token list` must not be able to reprint a secret")
	}
}

func TestTokenListAndRevoke(t *testing.T) {
	h := newHarness(t)
	h.issue("kai", "human")
	h.issue("codex", "agent")

	listing := h.run("token", "list")
	if !strings.Contains(listing, "kai") || !strings.Contains(listing, "codex") {
		t.Errorf("token list = %q", listing)
	}
	rows := decodeJSON[[]map[string]any](t, h.run("token", "list", "--json"))
	names := map[string]bool{}
	for _, row := range rows {
		names[row["name"].(string)] = true
		if _, leaked := row["token_sha256"]; leaked {
			t.Error("token list --json leaked a digest")
		}
	}
	if !names["kai"] || !names["codex"] || len(names) != 2 {
		t.Errorf("names = %v", names)
	}

	h.run("token", "revoke", "codex")
	if strings.Contains(h.run("token", "list"), "codex") {
		t.Error("codex survived the revoke")
	}
	if r := h.invoke(options{}, "token", "revoke", "codex"); r.code != 1 {
		t.Errorf("revoking twice exited %d, want 1", r.code)
	}
}

func TestRevokingTheLastTokenWarnsThatAuthIsOff(t *testing.T) {
	h := newHarness(t)
	h.issue("kai", "human")
	r := h.invoke(options{}, "token", "revoke", "kai")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stdout+r.stderr, "auth is now OFF") {
		t.Errorf("no warning that the server is now open:\n%s%s", r.stdout, r.stderr)
	}
}

func TestTokenAddRejectsABadKind(t *testing.T) {
	h := newHarness(t)
	if r := h.invoke(options{}, "token", "add", "x", "--kind", "robot"); r.code != 1 {
		t.Errorf("exit = %d, want 1", r.code)
	}
}

// --- whoami --------------------------------------------------------------------------

func TestWhoamiReportsTheOpenServer(t *testing.T) {
	h := newHarness(t)
	output := h.run("whoami")
	if !strings.Contains(output, h.url) {
		t.Errorf("whoami does not name the server:\n%s", output)
	}
	if !strings.Contains(output, "auth    off") {
		t.Errorf("whoami = %q", output)
	}
}

func TestWhoamiReportsTheTokenIdentity(t *testing.T) {
	h := newHarness(t)
	secret := h.issue("kai", "human")
	r := h.invoke(options{actor: "kai", kind: "human", token: secret}, "whoami")
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	for _, want := range []string{"per-actor tokens", "token   valid", "kai (human)"} {
		if !strings.Contains(r.stdout, want) {
			t.Errorf("whoami is missing %q:\n%s", want, r.stdout)
		}
	}
}

func TestWhoamiWarnsWhenTheActorDisagreesWithTheToken(t *testing.T) {
	h := newHarness(t)
	secret := h.issue("kai", "human")
	r := h.invoke(options{actor: "someone-else", kind: "human", token: secret}, "whoami")
	if !strings.Contains(r.stdout, "warning") ||
		!strings.Contains(r.stdout, "writes will be refused") {
		t.Errorf("no warning about the mismatch:\n%s", r.stdout)
	}
}

// --- auth failures -----------------------------------------------------------------------

func TestACommandWithoutATokenExits3(t *testing.T) {
	h := newHarness(t)
	h.issue("kai", "human")
	r := h.invoke(options{actor: "kai", kind: "human"}, "spaces")
	if r.code != 3 {
		t.Errorf("exit = %d, want 3 — agents branch on it", r.code)
	}
	output := r.stdout + r.stderr
	if !strings.Contains(output, "unauthorized") {
		t.Errorf("output = %q", output)
	}
	if !strings.Contains(output, "ANALOG_TOKEN") {
		t.Error("say how to fix it")
	}
}

func TestWritingAsTheWrongActorIsRefused(t *testing.T) {
	h := newHarness(t)
	secret := h.issue("kai", "human")
	r := h.invoke(options{actor: "impostor", kind: "human", token: secret},
		"new", "demo")
	if r.code != 1 {
		t.Errorf("exit = %d, want 1", r.code)
	}
	if !strings.Contains(strings.ToLower(r.stdout+r.stderr), "forbidden") {
		t.Errorf("output = %q", r.stdout+r.stderr)
	}
}

func TestAKindOnlyMismatchSaysWhichVariableToSet(t *testing.T) {
	h := newHarness(t)
	secret := h.issue("kai", "human")
	// The commonest case: the actor is right, ANALOG_ACTOR_KIND still says agent.
	r := h.invoke(options{actor: "kai", kind: "agent", token: secret}, "new", "demo")
	if r.code != 1 {
		t.Errorf("exit = %d, want 1", r.code)
	}
	if !strings.Contains(r.stdout+r.stderr, "ANALOG_ACTOR_KIND=human") {
		t.Errorf("the message must name the fix:\n%s%s", r.stdout, r.stderr)
	}
}

// --- login ---------------------------------------------------------------------------------

func TestLoginWritesAConfigAndLearnsTheActor(t *testing.T) {
	h := newHarness(t)
	secret := h.issue("kai", "human")
	configPath := filepath.Join(t.TempDir(), "c.toml")

	r := h.invoke(options{}, "login", h.url, "--token", secret, "--config", configPath)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(raw)
	for _, want := range []string{secret, `actor = "kai"`, `actor_kind = "human"`} {
		if !strings.Contains(body, want) {
			t.Errorf("config is missing %q:\n%s", want, body)
		}
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Errorf("mode = %o; it holds a credential", info.Mode().Perm())
	}

	// And the config is enough on its own: no ANALOG_* needed.
	api := client.New(client.Options{ConfigPath: configPath})
	if api.Actor != "kai" || api.Token != secret {
		t.Errorf("client read actor=%q token set=%v", api.Actor, api.Token != "")
	}
	identity, err := api.Whoami()
	if err != nil {
		t.Fatal(err)
	}
	if identity.Actor != "kai" {
		t.Errorf("whoami = %+v", identity)
	}
}

func TestLoginRefusesABadToken(t *testing.T) {
	h := newHarness(t)
	h.issue("kai", "human")
	configPath := filepath.Join(t.TempDir(), "c.toml")

	r := h.invoke(options{}, "login", h.url, "--token", "analog_wrong",
		"--config", configPath)
	if r.code != 3 {
		t.Errorf("exit = %d, want 3, the auth-failure code", r.code)
	}
	if !strings.Contains(r.stdout+r.stderr, "did not accept that token") {
		t.Errorf("output = %q", r.stdout+r.stderr)
	}
	if _, err := os.Stat(configPath); err == nil {
		t.Error("a refused login must not leave a config behind")
	}
}

func TestLoginRequiresATokenWhenTheServerWantsOne(t *testing.T) {
	h := newHarness(t)
	h.issue("kai", "human")
	configPath := filepath.Join(t.TempDir(), "c.toml")

	r := h.invoke(options{}, "login", h.url, "--config", configPath)
	if r.code != 1 {
		t.Errorf("exit = %d, want 1", r.code)
	}
	if !strings.Contains(r.stdout+r.stderr, "requires a token") {
		t.Errorf("output = %q", r.stdout+r.stderr)
	}
}

func TestLoginWorksAgainstAnOpenServer(t *testing.T) {
	h := newHarness(t)
	configPath := filepath.Join(t.TempDir(), "c.toml")
	r := h.invoke(options{}, "login", h.url, "--actor", "codex", "--config", configPath)
	if r.code != 0 {
		t.Fatalf("exit %d: %s", r.code, r.stderr)
	}
	if actor := client.New(client.Options{ConfigPath: configPath}).Actor; actor != "codex" {
		t.Errorf("actor = %q, want codex", actor)
	}
}
