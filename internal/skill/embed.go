// Package skill carries the agent skill inside the binary, so the workflow an
// agent is taught can never drift from the binary it talks to — the same reasoning
// that put the web UI inside analog-server. `analog onboard` copies it out.
//
// The canonical copy lives at skill/analog/SKILL.md (SPEC §4.3); the embedded one
// must match it byte for byte, which embed_test.go enforces so a checkout cannot
// update one and forget the other.
package skill

import (
	"embed"
	"io/fs"
)

//go:embed all:analog
var bundled embed.FS

// FS is the skill folder's contents: SKILL.md at the root.
func FS() fs.FS {
	sub, err := fs.Sub(bundled, "analog")
	if err != nil {
		return bundled
	}
	return sub
}
