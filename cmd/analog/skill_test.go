package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// `skill install` is the update path: it needs no actor, always overwrites, and
// says whether anything changed. `skill status` is the check a CI step or a human
// runs after a binary upgrade.
func TestSkillInstallAndStatus(t *testing.T) {
	h := newHarness(t)
	dir := filepath.Join(h.dataDir, "skills")
	target := filepath.Join(dir, "analog", "SKILL.md")

	r := h.invoke(options{}, "skill", "status", "--dir", dir)
	if r.code != exitError || !strings.Contains(r.stdout, "skill missing") {
		t.Fatalf("status before install: exit %d\n%s%s", r.code, r.stdout, r.stderr)
	}

	out := h.run("skill", "install", "--dir", dir)
	if !strings.Contains(out, "skill installed") {
		t.Errorf("first install did not say so:\n%s", out)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("skill not installed: %v", err)
	}
	if !strings.Contains(string(body), "analog feedback") {
		t.Errorf("installed SKILL.md does not teach the workflow:\n%.80s", body)
	}
	if _, err := os.Stat(filepath.Join(dir, "analog", "STYLE.md")); err != nil {
		t.Errorf("STYLE.md was not installed beside SKILL.md: %v", err)
	}

	out = h.run("skill", "status", "--dir", dir)
	if !strings.Contains(out, "skill current") {
		t.Errorf("status after install:\n%s", out)
	}
	if out = h.run("skill", "install", "--dir", dir); !strings.Contains(out, "skill unchanged") {
		t.Errorf("reinstall of a current skill should say unchanged:\n%s", out)
	}

	// A copy from an older binary differs; that is exactly what status is for.
	if err := os.WriteFile(target, []byte("# stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r = h.invoke(options{}, "skill", "status", "--dir", dir)
	if r.code != exitError || !strings.Contains(r.stdout, "skill stale") {
		t.Errorf("stale status: exit %d\n%s", r.code, r.stdout)
	}
	if out = h.run("skill", "install", "--dir", dir); !strings.Contains(out, "skill updated") {
		t.Errorf("install over a stale copy should say updated:\n%s", out)
	}
	if body, _ = os.ReadFile(target); !strings.Contains(string(body), "analog feedback") {
		t.Errorf("stale copy was not replaced")
	}

	// A file the skill does not know about is stale too: install replaces the
	// folder, so anything else in it is about to disappear.
	if err := os.WriteFile(filepath.Join(dir, "analog", "notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if r = h.invoke(options{}, "skill", "status", "--dir", dir); r.code != exitError {
		t.Errorf("an extra file should read as stale, got exit %d\n%s", r.code, r.stdout)
	}
}

func TestSkillCatPrintsTheEmbeddedFiles(t *testing.T) {
	h := newHarness(t)
	if out := h.run("skill", "cat"); !strings.HasPrefix(out, "---\nname: analog") {
		t.Errorf("cat should print SKILL.md:\n%.80s", out)
	}
	if out := h.run("skill", "cat", "STYLE.md"); !strings.Contains(out, "# Analog style") {
		t.Errorf("cat STYLE.md:\n%.80s", out)
	}
	if r := h.invoke(options{}, "skill", "cat", "nope.md"); r.code != exitError {
		t.Errorf("cat of a missing file should fail, got exit %d", r.code)
	}
}

// onboard keeps its polite skip, but now points at the command that refreshes.
func TestOnboardSkipPointsAtSkillInstall(t *testing.T) {
	h := newHarness(t)
	h.run("onboard", "claude-code", "--url", h.url)
	out := h.run("onboard", "claude-code", "--url", h.url)
	if !strings.Contains(out, "analog skill install") {
		t.Errorf("skip message should name the refresh command:\n%s", out)
	}
}
