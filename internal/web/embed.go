// Package web carries the built SPA so the server binary is self-contained.
//
// `just build` (or scripts/build.sh) copies web/dist here before `go build`. With
// nothing copied in, Dist returns nil and the server is API-only — which is what
// the conformance harness runs against.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var bundled embed.FS

// Dist is the built bundle, or nil when none was embedded.
func Dist() fs.FS {
	sub, err := fs.Sub(bundled, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
