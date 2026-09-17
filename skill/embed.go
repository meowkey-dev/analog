// Package skill carries the canonical agent skill inside the binary, so the
// workflow an agent is taught can never drift from the binary it talks to — the
// same reasoning that put the web UI inside analog-server. `analog onboard`
// copies it out.
package skill

import (
	"embed"
	"io/fs"
)

//go:embed all:analog
var bundled embed.FS

// FS is the skill folder's contents, rooted at analog/.
func FS() fs.FS {
	sub, err := fs.Sub(bundled, "analog")
	if err != nil {
		return bundled
	}
	return sub
}
