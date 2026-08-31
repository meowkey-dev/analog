// Command analog is the CLI. SPEC §4.2.
//
// A thin shell over client/. No rules live here: what a command prints is a
// presentation choice, what it means is the server's.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/meowkey-dev/analog/client"
	"github.com/meowkey-dev/analog/internal/version"
)

// stdinMarker is `-`: SPEC §4.2, so agents can pipe generated content in.
const stdinMarker = "-"

// Exit codes. 3 means auth, and agents branch on it.
const (
	exitError        = 1
	exitConflict     = 2
	exitUnauthorized = 3
)

// newClient is a variable so tests can point the CLI at a server.
var newClient = func() *client.Client { return client.New(client.Options{}) }

func errln(message string) { fmt.Fprintln(os.Stderr, message) }

// usage is a caller mistake rather than a server answer: cobra prints it and the
// process exits non-zero.
func usage(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

// fail turns a client error into the right exit code, with a message on stderr so
// agents notice failures.
func fail(err error) error {
	e, ok := client.As(err)
	if !ok {
		return err
	}
	switch {
	case e.Code == client.CodeUnauthorized:
		errln("unauthorized: " + e.Message)
		errln("  set ANALOG_TOKEN, or run `analog login <url> --token ...`")
		os.Exit(exitUnauthorized)
	case e.Code == client.CodeConflict:
		errln("conflict: " + e.Message)
		if current := e.Current(); current != nil {
			errln(fmt.Sprintf("  current sp_rev is %v", current["sp_rev"]))
		}
		os.Exit(exitConflict)
	}
	errln(e.Error())
	os.Exit(exitError)
	return nil
}

// out prints a value as indented JSON, which is the --json shape everywhere.
func out(value any) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return err
	}
	fmt.Print(buf.String())
	return nil
}

// readSource resolves a positional path or --file, with `-` meaning stdin.
func readSource(source, file string) (string, error) {
	target := file
	if target == "" {
		target = source
	}
	if target == "" {
		return "", usage("provide a file path, or - to read stdin")
	}
	if target == stdinMarker {
		raw, err := io.ReadAll(os.Stdin)
		return string(raw), err
	}
	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		return "", usage("no such file: %s", target)
	}
	raw, err := os.ReadFile(target)
	return string(raw), err
}

func main() {
	// Server failures have already exited with their own code in fail(); anything
	// reaching here is a usage error, which cobra has printed.
	if err := root().Execute(); err != nil {
		os.Exit(exitError)
	}
}

func root() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "analog",
		Short:         "A shared canvas for you and your agents.",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	cmd.CompletionOptions.DisableDefaultCmd = true
	version.Attach(cmd)
	cmd.AddCommand(
		whoamiCmd(), loginCmd(), tokenCmd(), onboardCmd(),
		spacesCmd(), newSpaceCmd(), openCmd(), rmSpaceCmd(),
		feedbackCmd(),
		addCmd(), cardsCmd(), updateCmd(), rmCmd(), linkCmd(), unlinkCmd(),
		commentsCmd(), resolveCmd(),
		exportCmd(), importCmd(), uploadCmd(), eventsCmd(),
	)
	return cmd
}

// --- connection ----------------------------------------------------------------

func whoamiCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Which server this shell talks to, and who it writes as",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			a := newClient()
			health, err := a.Health()
			if err != nil {
				return fail(err)
			}
			identity := client.Identity{Actor: a.Actor, ActorKind: a.ActorKind}
			if health.AuthRequired {
				if identity, err = a.Whoami(); err != nil {
					return fail(err)
				}
			}
			if asJSON {
				return out(map[string]any{
					"url": a.Base, "configured_actor": a.Actor,
					"ok": health.OK, "service": health.Service, "version": health.Version,
					"auth_required": health.AuthRequired,
					"authenticated": identity.Authenticated,
					"actor":         identity.Actor,
					"actor_kind":    identity.ActorKind,
				})
			}
			version := health.Version
			if version == "" {
				version = "?"
			}
			fmt.Printf("server  %s  (analog %s)\n", a.Base, version)
			state := "off"
			if health.AuthRequired {
				state = "per-actor tokens"
			}
			fmt.Printf("auth    %s\n", state)
			if health.AuthRequired {
				valid := "MISSING OR INVALID"
				if identity.Authenticated {
					valid = "valid"
				}
				fmt.Printf("token   %s\n", valid)
				if identity.Authenticated && identity.Actor != a.Actor {
					fmt.Printf("        warning: ANALOG_ACTOR is '%s' but this token "+
						"writes as '%s'; writes will be refused\n", a.Actor, identity.Actor)
				}
			}
			actor, kind := identity.Actor, identity.ActorKind
			if actor == "" {
				actor = a.Actor
			}
			if kind == "" {
				kind = a.ActorKind
			}
			fmt.Printf("actor   %s (%s)\n", actor, kind)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

func loginCmd() *cobra.Command {
	var token, actor, kind, path string
	cmd := &cobra.Command{
		Use:   "login <url>",
		Short: "Remember a server in ~/.analog.toml so ANALOG_* need not be set every time",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			target := configPath(path)
			probeActor := actor
			if probeActor == "" {
				probeActor = "probe"
			}
			probe := client.New(client.Options{URL: args[0], Actor: probeActor,
				Token: token, Config: map[string]string{}})
			health, err := probe.Health()
			if err != nil {
				return fail(err)
			}
			if health.AuthRequired && token == "" {
				errln("this server requires a token; pass --token")
				os.Exit(exitError)
			}
			if token != "" {
				identity, err := probe.Whoami()
				if err != nil || !identity.Authenticated {
					errln(probe.Base + " did not accept that token")
					os.Exit(exitUnauthorized)
				}
				actor, kind = identity.Actor, identity.ActorKind
			}

			lines := []string{fmt.Sprintf("url = %q", strings.TrimSuffix(probe.Base, "/api"))}
			if actor != "" {
				lines = append(lines,
					fmt.Sprintf("actor = %q", actor), fmt.Sprintf("actor_kind = %q", kind))
			}
			if token != "" {
				lines = append(lines, fmt.Sprintf("token = %q", token))
			}
			body := "# written by `analog login`\n" + strings.Join(lines, "\n") + "\n"
			// 0600 from the start: it holds a credential, so it must never exist
			// world-readable even briefly.
			if err := os.WriteFile(target, []byte(body), 0o600); err != nil {
				return err
			}
			_ = os.Chmod(target, 0o600)
			fmt.Printf("wrote %s\n", target)
			fmt.Printf("  server %s\n", probe.Base)
			if actor != "" {
				fmt.Printf("  actor  %s (%s)\n", actor, kind)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&token, "token", "", "bearer token issued by `analog token add`")
	cmd.Flags().StringVar(&actor, "actor", "", "actor to write as")
	cmd.Flags().StringVar(&kind, "kind", "agent", "human | agent")
	cmd.Flags().StringVar(&path, "config", "", "config file to write")
	return cmd
}

func configPath(override string) string {
	if override != "" {
		return override
	}
	if env := os.Getenv("ANALOG_CONFIG"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".analog.toml"
	}
	return home + "/.analog.toml"
}
