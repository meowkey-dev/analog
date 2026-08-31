package main

import (
	"strings"
	"testing"
)

// --version and the version line in the root help. The value itself is injected
// at build time (internal/version), so a plain `go build` answers "dev" — what
// matters is that the flag exists, exits 0, and prints something stable for
// agents to parse.
func TestVersionFlagPrintsAVersion(t *testing.T) {
	h := newHarness(t)
	r := h.invoke(options{}, "--version")
	if r.code != 0 {
		t.Fatalf("exit %d: %s%s", r.code, r.stdout, r.stderr)
	}
	if !strings.HasPrefix(r.stdout, "analog version ") {
		t.Errorf("--version = %q, want the `analog version X` line", r.stdout)
	}
	if v := strings.TrimSpace(strings.TrimPrefix(r.stdout, "analog version ")); v == "" {
		t.Errorf("--version printed no value: %q", r.stdout)
	}
	if r2 := h.invoke(options{}, "-v"); r2.code != 0 || r2.stdout != r.stdout {
		t.Errorf("-v = %q, --version = %q", r2.stdout, r.stdout)
	}
}

func TestRootHelpShowsTheVersion(t *testing.T) {
	h := newHarness(t)
	help := h.run("--help")
	if !strings.Contains(help, "analog version ") {
		t.Errorf("root --help does not show the version:\n%s", help)
	}
	// The version line is a root-help thing; subcommand help inherits the
	// template but must not inherit the line.
	sub := h.run("onboard", "--help")
	if strings.Contains(sub, "analog version ") {
		t.Errorf("subcommand --help picked up the version line:\n%s", sub)
	}
}
