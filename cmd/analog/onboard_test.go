package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The onboarding subcommand (#31): one command that mints a token, installs the
// skill, and wires the CLI or MCP. These run it as a real process against a real
// server and then act as the onboarded agent, because what matters is that the
// artifacts it leaves behind actually work.

// runWrapper executes an onboarded wrapper with a clean environment — the whole
// point of the wrapper is that it carries its own config, so nothing else may leak
// in.
func (h *harness) runWrapper(path string, args ...string) result {
	h.t.Helper()
	cmd := exec.Command(path, args...)
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + h.dataDir,
	}
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		h.t.Fatal(err)
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func TestOnboardSetsUpTokenSkillAndWrapper(t *testing.T) {
	h := newHarness(t)
	home := filepath.Join(h.dataDir, "agent-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	skills := filepath.Join(home, ".claude", "skills")
	wrappers := filepath.Join(home, ".local", "bin")
	project := filepath.Join(home, "proj")

	out := h.run("onboard", "claude-code",
		"--issue", "--url", h.url,
		"--skill-into", skills, "--wrapper", wrappers, "--claude-env", project)

	secret := tokenRE.FindString(out)
	if secret == "" {
		t.Fatalf("no token in the output:\n%s", out)
	}

	// The skill came from inside the binary, not from a checkout path.
	skill, err := os.ReadFile(filepath.Join(skills, "analog", "SKILL.md"))
	if err != nil {
		t.Fatalf("skill not installed: %v", err)
	}
	if !strings.Contains(string(skill), "analog feedback") {
		t.Errorf("installed SKILL.md does not teach the workflow:\n%.80s", skill)
	}

	// The wrapper is mode 700 (it carries the token) and carries it.
	wrapper := filepath.Join(wrappers, "analog-claude-code")
	info, err := os.Stat(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("wrapper mode = %o, want 700", info.Mode().Perm())
	}
	body, err := os.ReadFile(wrapper)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{h.url, "ANALOG_ACTOR=claude-code",
		"ANALOG_ACTOR_KIND=agent", secret, "ANALOG_CONFIG=/nonexistent"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("wrapper is missing %q:\n%s", want, body)
		}
	}

	// claude-env merged, with the token.
	raw, err := os.ReadFile(filepath.Join(project, ".claude", "settings.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	env := settings["env"].(map[string]any)
	if env["ANALOG_TOKEN"] != secret || env["ANALOG_ACTOR"] != "claude-code" {
		t.Errorf("claude env = %v", env)
	}

	// The proof: the wrapper works with nothing but a clean shell, and the token
	// it carries is good for writes on the server it points at.
	whoami := h.runWrapper(wrapper, "whoami")
	if whoami.code != 0 || !strings.Contains(whoami.stdout, "token   valid") ||
		!strings.Contains(whoami.stdout, "claude-code (agent)") {
		t.Fatalf("wrapper whoami exited %d:\n%s%s", whoami.code, whoami.stdout, whoami.stderr)
	}
	if made := h.runWrapper(wrapper, "new", "demo", "--title", "Demo"); made.code != 0 {
		t.Fatalf("the minted token could not write:\n%s%s", made.stdout, made.stderr)
	}
}

// A ~/.analog.toml belongs to the *user*. The wrapper pins its own actor and
// points ANALOG_CONFIG nowhere, so a human's config cannot make the agent post
// under the human's name.
func TestOnboardWrapperIgnoresTheHumanConfig(t *testing.T) {
	h := newHarness(t)
	home := filepath.Join(h.dataDir, "agent-home")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	decoy := "# written by `analog login`\n" +
		"url = \"" + h.url + "\"\nactor = \"human\"\nactor_kind = \"human\"\n"
	if err := os.WriteFile(filepath.Join(home, ".analog.toml"), []byte(decoy), 0o600); err != nil {
		t.Fatal(err)
	}

	wrappers := filepath.Join(h.dataDir, "bin")
	out := h.run("onboard", "claude-code", "--issue", "--url", h.url,
		"--wrapper", wrappers)
	// The old bare `--wrapper` form is a usage error now, loudly:
	if r := h.invoke(options{}, "onboard", "claude-code", "--wrapper"); r.code == 0 {
		t.Error("bare --wrapper must ask for a directory")
	}
	if secret := tokenRE.FindString(out); secret == "" {
		t.Fatalf("no token in the output:\n%s", out)
	}

	whoami := h.runWrapper(filepath.Join(wrappers, "analog-claude-code"), "whoami")
	if whoami.code != 0 {
		t.Fatalf("exit %d: %s%s", whoami.code, whoami.stdout, whoami.stderr)
	}
	if !strings.Contains(whoami.stdout, "claude-code (agent)") {
		t.Errorf("the human's config leaked into the wrapper:\n%s", whoami.stdout)
	}
	if strings.Contains(whoami.stdout, "human") {
		t.Errorf("whoami mentions the human actor:\n%s", whoami.stdout)
	}
}

func TestOnboardClaudeEnvMergesAndNeverClobbers(t *testing.T) {
	h := newHarness(t)
	project := filepath.Join(h.dataDir, "proj")
	dir := filepath.Join(project, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{
  "permissions": {"allow": ["Bash(analog:*)"]},
  "env": {"OTHER": "keep me", "ANALOG_URL": "http://stale:1"}
}
`
	target := filepath.Join(dir, "settings.local.json")
	if err := os.WriteFile(target, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	h.run("onboard", "codex", "--url", h.url, "--token", "analog_existing",
		"--claude-env", project)

	raw, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, raw)
	}
	if _, ok := settings["permissions"]; !ok {
		t.Error("claude-env clobbered keys outside env")
	}
	env := settings["env"].(map[string]any)
	if env["OTHER"] != "keep me" {
		t.Errorf("claude-env clobbered an unrelated variable: %v", env)
	}
	if env["ANALOG_URL"] != h.url || env["ANALOG_TOKEN"] != "analog_existing" ||
		env["ANALOG_ACTOR"] != "codex" || env["ANALOG_CONFIG"] != "/nonexistent" {
		t.Errorf("env = %v", env)
	}
}

func TestOnboardPrintsTheWiring(t *testing.T) {
	h := newHarness(t)

	envOut := h.run("onboard", "claude-code", "--url", h.url, "--print-env")
	for _, want := range []string{
		"export ANALOG_URL=" + h.url, "export ANALOG_ACTOR=claude-code",
		"export ANALOG_TOKEN=<token>", "analog whoami",
	} {
		if !strings.Contains(envOut, want) {
			t.Errorf("print-env output is missing %q:\n%s", want, envOut)
		}
	}

	mcpOut := h.run("onboard", "claude-code", "--url", h.url,
		"--token", "analog_secret", "--print-mcp")
	for _, want := range []string{"claude mcp add analog", "ANALOG_TOKEN=analog_secret",
		binaries.mcp} {
		if !strings.Contains(mcpOut, want) {
			t.Errorf("print-mcp output is missing %q:\n%s", want, mcpOut)
		}
	}

	// A bare onboard prints the shell exports as the fallback, with no token to
	// echo.
	bare := h.run("onboard", "claude-code", "--url", h.url)
	if !strings.Contains(bare, "export ANALOG_URL=") {
		t.Errorf("bare onboard printed nothing useful:\n%s", bare)
	}
	if strings.Contains(bare, "token:") {
		t.Errorf("bare onboard echoed a token it was never given:\n%s", bare)
	}
}

func TestOnboardWithoutAnIssueSaysHowToGetAToken(t *testing.T) {
	// --claude-env without a token is the "agent elsewhere, token later" path.
	h := newHarness(t)
	project := filepath.Join(h.dataDir, "proj")
	out := h.run("onboard", "claude-code", "--url", h.url,
		"--claude-env", project, "--print-env")
	if !strings.Contains(out, "no token written") {
		t.Errorf("no hint about the missing token:\n%s", out)
	}
}

func TestOnboardRejectsABadKind(t *testing.T) {
	h := newHarness(t)
	if r := h.invoke(options{}, "onboard", "claude-code", "--kind", "robot"); r.code == 0 {
		t.Error("onboard accepted --kind robot")
	}
}

// Issue #31: the release binaries must be able to onboard with no checkout. That
// reduces to the token path: it shells out to the analog-server sitting beside the
// binary. Everything else is already self-contained.
func TestOnboardIssueWorksFromBareBinaries(t *testing.T) {
	h := newHarness(t)
	// The binaries live in one directory with no checkout in sight; `analog
	// onboard --issue` must find its sibling there.
	out := h.run("onboard", "solo", "--issue", "--url", h.url, "--print-env")
	secret := tokenRE.FindString(out)
	if secret == "" {
		t.Fatalf("no token minted:\n%s", out)
	}
	identity := h.tokenStore().Resolve(secret)
	if identity == nil || identity.Actor != "solo" || identity.ActorKind != "agent" {
		t.Errorf("the minted token does not resolve: %+v", identity)
	}
}

// The token store `--issue` writes is whichever file the environment points at,
// which is not necessarily the running server's. Found by the tmux e2e: the mint
// succeeded, the wrapper carried a dead token, and nothing said why. It must say
// so immediately, and still exit 0 — the token itself was fine.
func TestOnboardIssueWarnsWhenTheTokenDoesNotAuthenticate(t *testing.T) {
	h := newHarness(t)
	r := h.invoke(options{}, "onboard", "solo", "--issue", "--url", "http://127.0.0.1:1")
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.stdout, r.stderr)
	}
	if !strings.Contains(r.stderr, "did not authenticate") ||
		!strings.Contains(r.stderr, "ANALOG_DATA_DIR") {
		t.Errorf("no warning naming the likely cause:\n%s", r.stderr)
	}
}
