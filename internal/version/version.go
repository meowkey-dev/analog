// Package version carries the release version of the binaries, injected at build
// time (scripts/build.sh, from the tag in CI): a plain `go build` — tests, `go
// install` — reports "dev".
//
// This is the *binary's* version, which moves on every release. The API contract's
// version (internal/api.Version, matching contracts/openapi.json) is a different
// number and moves only through the amendment process. /api/health reports both:
// `version` is the contract, `release` is this string. A v0.11.0 binary can serve
// the 0.6.0 contract.
package version

import (
	"github.com/spf13/cobra"
)

// Version is what `--version` and the root help print.
var Version = "dev"

// helpTemplate is cobra's default plus a version line on the root command's help
// only — subcommand help inherits the template through the parent chain, so
// HasParent guards it.
const helpTemplate = `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if not .HasParent}}{{.Name}} version {{.Version}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}`

// Attach gives a root command a `--version` flag (cobra provides it once
// Version is set) and puts the version on the root `--help`.
func Attach(cmd *cobra.Command) {
	cmd.Version = Version
	cmd.SetHelpTemplate(helpTemplate)
}
