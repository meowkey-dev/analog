// Command onboard_agent is deprecated: use `analog onboard <actor>` — same flags,
// no checkout needed.
//
// The script is now a subcommand of the CLI (issue #31). This shim forwards to it
// and will be removed in the next minor release. `--bin-dir` still works: it is
// consumed here to find the `analog` binary and not passed on.
//
//	go run ./scripts/onboard_agent [flags] <actor>
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The source path is the only fixed point a `go run` binary has.
var repoRoot = func() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(file)))
}()

func main() {
	binDir, forwarded, err := translate(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "scripts/onboard_agent is deprecated; use `analog onboard` "+
		"(same flags). The shim will be removed in the next minor release.\n\n")

	analog, err := findBinary("analog", binDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cmd := exec.Command(analog, append([]string{"onboard"}, forwarded...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// translate copies the shim's argv onto `analog onboard`: --bin-dir is consumed
// here, and the bare --wrapper / --claude-env forms — allowed by the old script,
// where the subcommand's flags take a value — gain the defaults it used to apply.
// A valued flag or a bare one followed by another flag passes through untouched;
// the subcommand's own parser decides what a token means from there.
func translate(argv []string) (binDir string, forwarded []string, err error) {
	defaults := map[string]string{"--wrapper": "~/.local/bin", "--claude-env": "."}
	rest := argv
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		switch {
		case arg == "--bin-dir":
			if len(rest) == 0 {
				return "", nil, errors.New("--bin-dir needs a value")
			}
			binDir, rest = rest[0], rest[1:]
		case strings.HasPrefix(arg, "--bin-dir="):
			binDir = strings.TrimPrefix(arg, "--bin-dir=")
		case defaults[arg] != "" && (len(rest) == 0 || strings.HasPrefix(rest[0], "-")):
			forwarded = append(forwarded, arg, defaults[arg])
		default:
			forwarded = append(forwarded, arg)
		}
	}
	return binDir, forwarded, nil
}

func findBinary(name, binDir string) (string, error) {
	for _, dir := range []string{binDir, os.Getenv("ANALOG_BIN_DIR"), filepath.Join(repoRoot, "bin")} {
		if dir == "" {
			continue
		}
		candidate := filepath.Join(expand(dir), name)
		if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
			resolved, err := filepath.Abs(candidate)
			if err != nil {
				resolved = candidate
			}
			return resolved, nil
		}
	}
	if found, err := exec.LookPath(name); err == nil {
		return found, nil
	}
	return "", fmt.Errorf("cannot find `%s`. Build it with scripts/build.sh, "+
		"or pass --bin-dir.", name)
}

func expand(path string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
