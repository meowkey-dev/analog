// Package tokencli is the `token` command group.
//
// It reads and writes the server's auth file directly, so it runs on the server
// host. Both binaries expose it: `analog token ...` is what SPEC §4.2 spells, and
// `analog-server token ...` is what an operator has to hand when only the server
// binary is installed.
package tokencli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/meowkey-dev/analog/internal/auth"
	"github.com/meowkey-dev/analog/internal/config"
)

func store() *auth.Store { return auth.NewStore(config.AuthPath()) }

// Command builds the `token` group.
func Command() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "token",
		Short: "Issue and revoke per-actor tokens",
		Long: "Issue and revoke per-actor tokens. Reads and writes the server's auth " +
			"file, so run it on the server host.",
	}
	cmd.AddCommand(tokenAddCmd(), tokenListCmd(), tokenRevokeCmd())
	return cmd
}

func tokenAddCmd() *cobra.Command {
	var kind string
	cmd := &cobra.Command{
		Use:   "add <actor>",
		Short: "Mint a token for one actor",
		Long:  "Mint a token for one actor. It is shown once and only stored as a digest.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tokens := store()
			secret, err := tokens.Issue(args[0], kind)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			fmt.Printf("%s (%s)\n", args[0], kind)
			// On a line of its own: this is what the conformance harness reads.
			fmt.Printf("  %s\n", secret)
			fmt.Println()
			fmt.Println("Copy it now — it is not recoverable. On the client:")
			fmt.Printf("  export ANALOG_ACTOR=%s\n", args[0])
			fmt.Printf("  export ANALOG_TOKEN=%s\n", secret)
			fmt.Printf("stored in %s\n", tokens.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&kind, "kind", "agent", "human | agent")
	return cmd
}

func tokenListCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Every actor with a token; secrets are not recoverable",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			tokens := store()
			entries, err := tokens.Entries()
			if err != nil {
				return err
			}
			if asJSON {
				out, err := json.MarshalIndent(entries, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(out))
				return nil
			}
			if len(entries) == 0 {
				fmt.Printf("no tokens; auth is off (%s)\n", tokens.Path)
				return nil
			}
			for _, e := range entries {
				issued := e.CreatedAt
				if issued == "" {
					issued = "?"
				}
				fmt.Printf("%-20s %-6s issued %s\n", e.Name, e.Kind, issued)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "machine-readable output")
	return cmd
}

func tokenRevokeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "revoke <actor>",
		Short: "Invalidate an actor's token",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			tokens := store()
			removed, err := tokens.Revoke(args[0])
			if err != nil {
				return err
			}
			if !removed {
				fmt.Fprintf(os.Stderr, "no token for '%s'\n", args[0])
				os.Exit(1)
			}
			fmt.Printf("revoked %s\n", args[0])
			if !tokens.Enabled() {
				fmt.Fprintln(os.Stderr,
					"warning: that was the last token — auth is now OFF on this server")
			}
			return nil
		},
	}
}
