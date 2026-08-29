package main

import (
	"github.com/spf13/cobra"

	"github.com/meowkey-dev/analog/internal/tokencli"
)

// tokenCmd reads and writes the server's auth file, so run it on the server host.
func tokenCmd() *cobra.Command { return tokencli.Command() }
