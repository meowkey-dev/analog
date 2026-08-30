package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meowkey-dev/analog/client"
	"github.com/meowkey-dev/analog/internal/skill"
)

// onboardCmd gives an agent everything it needs to use an Analog server: a token
// (the server decides who it is), the MCP server or the CLI wired to that token,
// and the skill, which teaches the workflow the API cannot. SPEC §4.2.
//
// `--issue` composes the existing `token add` group by shelling out to
// analog-server, which owns the auth file — the format stays the server's business.
// Everything else works anywhere; `--issue` only on the server host, where the
// server binary is. Both are found beside this binary first (they ship together in
// the release archives and in brew), then on PATH.
func onboardCmd() *cobra.Command {
	var kind, url, token, skillInto, claudeEnv, wrapper string
	var issue, printMCP, printEnv bool

	cmd := &cobra.Command{
		Use:   "onboard <actor>",
		Short: "Set up an agent: token, wiring, and the skill",
		Long: "Set up an agent with one command: a token (`--issue`, server host " +
			"only), the skill (`--skill-into`), and the wiring (`--claude-env`, " +
			"`--wrapper`, or `--print-mcp` / `--print-env`).",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			actor := args[0]
			if kind != "agent" && kind != "human" {
				return usage("--kind must be agent or human, not %q", kind)
			}

			if issue {
				secret, err := issueToken(actor, kind, url)
				if err != nil {
					return err
				}
				token = secret
			}

			if skillInto != "" {
				target, err := installSkill(expandTilde(skillInto))
				if err != nil {
					return err
				}
				fmt.Printf("skill installed: %s\n", target)
				fmt.Println("  it loads on demand, so it costs nothing in unrelated sessions.")
				fmt.Println()
			}

			if claudeEnv != "" {
				target, err := writeClaudeEnv(expandTilde(claudeEnv), actor, kind, url, token)
				if err != nil {
					return err
				}
				fmt.Printf("claude env: %s\n", target)
				if token == "" {
					fmt.Println("  no token written — add ANALOG_TOKEN there once the server issues one.")
				}
				fmt.Println("  Read at session start, so restart the agent for it to take effect.")
				fmt.Println()
			}

			if wrapper != "" {
				path, err := writeWrapper(expandTilde(wrapper), actor, kind, url, token)
				if err != nil {
					return err
				}
				fmt.Printf("wrapper: %s\n", path)
				base := filepath.Base(path)
				fmt.Printf("  A running agent can use it immediately — no restart, no exports:\n"+
					"    %s whoami\n"+
					"    %s feedback <slug>\n", base, base)
				fmt.Println("  It carries the token, so it is mode 700 and lives outside the repo.")
				if !onPath(filepath.Dir(path)) {
					fmt.Printf("  %s is not on PATH; use the full path or add it.\n\n",
						filepath.Dir(path))
				} else {
					fmt.Println()
				}
			}

			if printMCP {
				command, err := mcpBin()
				if err != nil {
					return err
				}
				secret := token
				if secret == "" {
					secret = "$ANALOG_TOKEN"
				}
				fmt.Println("wire up MCP (stdio) — run this where the agent runs:")
				fmt.Println()
				fmt.Println("  claude mcp add analog \\")
				fmt.Printf("    -e ANALOG_URL=%s \\\n", url)
				fmt.Printf("    -e ANALOG_ACTOR=%s \\\n", actor)
				fmt.Printf("    -e ANALOG_ACTOR_KIND=%s \\\n", kind)
				fmt.Printf("    -e ANALOG_TOKEN=%s \\\n", secret)
				fmt.Printf("    -- %s\n", command)
				fmt.Println()
				fmt.Println("  --scope user puts it in every project; the default is this one only.")
				fmt.Println("  Check it with:  claude mcp get analog")
				fmt.Println()
			}

			if printEnv || (!printMCP && skillInto == "") {
				analogPath, err := os.Executable()
				if err != nil {
					return err
				}
				analogPath, _ = filepath.EvalSymlinks(analogPath)
				fmt.Println("or, for an agent that only has a shell:")
				fmt.Println()
				fmt.Printf("  export ANALOG_URL=%s\n", url)
				fmt.Printf("  export ANALOG_ACTOR=%s\n", actor)
				fmt.Printf("  export ANALOG_ACTOR_KIND=%s\n", kind)
				fmt.Printf("  export ANALOG_TOKEN=%s\n", or(token, "<token>"))
				fmt.Printf("  export PATH=%s:$PATH\n", filepath.Dir(analogPath))
				fmt.Println()
				fmt.Println("  then:  analog whoami   # confirms who the server thinks you are")
			}

			if token != "" && !printMCP && !printEnv {
				fmt.Printf("token: %s\n", token)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "agent", "agent | human")
	cmd.Flags().BoolVar(&issue, "issue", false, "mint a token (server host only)")
	cmd.Flags().StringVar(&url, "url", "http://127.0.0.1:8787", "the server's base URL")
	cmd.Flags().StringVar(&token, "token", "", "an existing token; implied by --issue")
	cmd.Flags().StringVar(&skillInto, "skill-into", "", "copy the skill here, e.g. ~/.claude/skills")
	cmd.Flags().BoolVar(&printMCP, "print-mcp", false, "print the claude mcp add command")
	cmd.Flags().BoolVar(&printEnv, "print-env", false,
		"print the exports an agent with only a shell needs")
	cmd.Flags().StringVar(&claudeEnv, "claude-env", "",
		"merge ANALOG_* into PROJECT/.claude/settings.local.json; use . for this project")
	cmd.Flags().StringVar(&wrapper, "wrapper", "",
		"write a wrapper command carrying this actor's config; DIR defaults to ~/.local/bin")
	return cmd
}

// --- the binaries this command composes -----------------------------------------

// siblingBin finds one of the binaries that ship beside this one: the release
// archives and brew both put them in one directory, and anything else falls back
// to PATH.
func siblingBin(name string) (string, error) {
	if self, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(self); err == nil {
			self = resolved
		}
		candidate := filepath.Join(filepath.Dir(self), name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if found, err := exec.LookPath(name); err == nil {
		return found, nil
	}
	return "", usage("cannot find `%s`. It ships beside `analog` in the release "+
		"archives, or build it with scripts/build.sh", name)
}

func mcpBin() (string, error) { return siblingBin("analog-mcp") }

var mintedTokenRE = regexp.MustCompile(`analog_[A-Za-z0-9_-]+`)

// issueToken mints a token through analog-server, which owns the auth file.
// Shelling out rather than reading the file: the format is the server's business.
func issueToken(actor, kind, url string) (string, error) {
	server, err := siblingBin("analog-server")
	if err != nil {
		return "", err
	}
	proc := exec.Command(server, "token", "add", actor, "--kind", kind)
	var stdout, stderr bytes.Buffer
	proc.Stdout, proc.Stderr = &stdout, &stderr
	if err := proc.Run(); err != nil {
		return "", fmt.Errorf("%s token add failed:\n%s%s",
			server, stdout.String(), stderr.String())
	}
	match := mintedTokenRE.FindString(stdout.String())
	if match == "" {
		return "", fmt.Errorf("no token in the output of `analog-server token add`:\n%s",
			stdout.String())
	}
	fmt.Printf("issued a token for %s (%s)\n", actor, kind)
	fmt.Println("  it is shown once and stored only as a digest.")
	fmt.Println()

	// The token store this just wrote is whichever file the environment points at,
	// which is not necessarily the one the server at --url reads. Say so now, not
	// when the agent 401s later.
	if identity, err := client.New(client.Options{URL: url, Actor: actor,
		Token: match, Config: map[string]string{}}).Whoami(); err != nil || !identity.Authenticated {
		errln("warning: the fresh token did not authenticate against " + url)
		errln("  the server is probably reading a different auth file than this shell;")
		errln("  point ANALOG_DATA_DIR (or ANALOG_AUTH_FILE) at the server's before retrying.")
		fmt.Println()
	}
	return match, nil
}

// --- the artifacts ---------------------------------------------------------------

// installSkill copies the embedded skill into place. Skills are a folder with a
// SKILL.md; copying is the whole install.
func installSkill(into string) (string, error) {
	target := filepath.Join(into, "analog")
	if err := os.MkdirAll(into, 0o755); err != nil {
		return "", err
	}
	if err := os.RemoveAll(target); err != nil {
		return "", err
	}
	if err := os.CopyFS(target, skill.FS()); err != nil {
		return "", err
	}
	return target, nil
}

// writeWrapper is a command that is already configured as one actor. MCP config and
// skills are read when a session starts, so neither reaches an agent that is
// mid-session. A wrapper on disk does, and it also sidesteps the trap in
// `analog login`: that writes ~/.analog.toml for the *user*, so an agent running as
// you would inherit your identity and write under your name.
func writeWrapper(into, actor, kind, url, token string) (string, error) {
	if err := os.MkdirAll(into, 0o755); err != nil {
		return "", err
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(self); err == nil {
		self = resolved
	}
	lines := []string{
		"#!/bin/sh",
		fmt.Sprintf("# Analog, pre-configured as %s (%s).", actor, kind),
		"# Written by `analog onboard`. Contains a token: keep it mode 700",
		"# and out of any repository.",
		fmt.Sprintf("export ANALOG_URL=%s", url),
		fmt.Sprintf("export ANALOG_ACTOR=%s", actor),
		fmt.Sprintf("export ANALOG_ACTOR_KIND=%s", kind),
	}
	if token != "" {
		lines = append(lines, fmt.Sprintf("export ANALOG_TOKEN=%s", token))
	}
	lines = append(lines,
		"# Ignore any ~/.analog.toml, which may belong to a different actor.",
		"export ANALOG_CONFIG=/nonexistent",
		fmt.Sprintf("exec %q \"$@\"", self),
	)
	path := filepath.Join(into, "analog-"+actor)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(path, 0o700)
	return path, nil
}

// writeClaudeEnv merges the ANALOG_* env into a project's
// .claude/settings.local.json — a merge, never a clobber. `settings.local.json`
// rather than `settings.json` because it holds a token and is the gitignored one.
// Claude Code applies `env` to its Bash tool calls, so the skill's plain
// `analog ...` commands work with no wrapper and no exports — but it is read at
// session start, so an already-running agent needs a restart.
func writeClaudeEnv(project, actor, kind, url, token string) (string, error) {
	dir := filepath.Join(project, ".claude")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	target := filepath.Join(dir, "settings.local.json")

	settings := map[string]any{}
	if raw, err := os.ReadFile(target); err == nil && len(strings.TrimSpace(string(raw))) > 0 {
		if err := json.Unmarshal(raw, &settings); err != nil {
			return "", fmt.Errorf("%s is not JSON: %w", target, err)
		}
	}

	env, _ := settings["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	env["ANALOG_URL"] = url
	env["ANALOG_ACTOR"] = actor
	env["ANALOG_ACTOR_KIND"] = kind
	// Otherwise a ~/.analog.toml belonging to a different actor wins.
	env["ANALOG_CONFIG"] = "/nonexistent"
	if token != "" {
		env["ANALOG_TOKEN"] = token
	}
	settings["env"] = env

	encoded, err := marshalStable(settings)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(target, encoded, 0o644); err != nil {
		return "", err
	}
	return target, nil
}

// marshalStable indents like the Python writer did and sorts map keys, so
// re-running onboard produces a stable diff rather than a shuffled file.
func marshalStable(settings map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(settings); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// --- small helpers ---------------------------------------------------------------

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func onPath(dir string) bool {
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if entry == dir {
			return true
		}
	}
	return false
}

// expandTilde resolves a leading ~, which shell users type and the script expanded.
func expandTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}
